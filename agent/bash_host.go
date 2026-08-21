package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

// 宿主 Linux bash（Windows 经 WSL 的 bash.exe 执行，带完整 Linux/kali 工具链）：
// 替代 SDK 在 Windows 上默认的 PowerShell 执行，让 curl/nmap/sqlmap/python3 与
// 管道等 Unix 写法在跑分/渗透任务里真正可用。

// hostBashCmd 返回可执行与参数前缀：Windows → bash.exe -lc（WSL），否则 bash -c。
func hostBashCmd() (string, []string) {
	if runtime.GOOS == "windows" {
		return "bash.exe", []string{"-lc"}
	}
	return "bash", []string{"-lc"}
}

// winToWslPath 把 Windows 路径 F:\a\b 转成 WSL /mnt/f/a/b。
func winToWslPath(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return p
	}
	drive := strings.ToLower(p[:1])
	rest := strings.ReplaceAll(p[2:], "\\", "/")
	return "/mnt/" + drive + rest
}

var secretEnvName = regexp.MustCompile(`(?i)(^|_)(API_KEY|TOKEN|SECRET|PASSWORD|PASS|PRIVATE_KEY|CREDENTIALS?|DSN)$`)

// ToolEnvironment returns the environment inherited by agent-launched child
// processes. Host credentials are filtered by default so a shell or custom
// script cannot read the server's LLM keys, database DSN, or deployment tokens.
func ToolEnvironment(session []string) []string {
	all := append(append([]string{}, os.Environ()...), session...)
	if os.Getenv("RESTXTRA_ALLOW_TOOL_SECRET_ENV") == "true" {
		return all
	}
	out := make([]string, 0, len(all))
	for _, kv := range all {
		i := strings.IndexByte(kv, '=')
		if i <= 0 || secretEnvName.MatchString(kv[:i]) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// wslEnv 过滤掉指向 Windows 本机（localhost/127.0.0.1）的代理与 CA 变量——
// WSL2 NAT 用不了它们，反而触发 localhost 代理提示并污染输出。保留其余 env。
func wslEnv(tc *actool.ToolContext) []string {
	var session []string
	if tc != nil {
		session = tc.Env
	}
	env := ToolEnvironment(session)
	skipPrefix := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy",
		"SSL_CERT_FILE", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO"}
	isProxy := func(k string) bool {
		for _, p := range skipPrefix {
			if strings.EqualFold(k, p) {
				return true
			}
		}
		return false
	}
	hasLocalRef := func(v string) bool {
		return strings.Contains(strings.ToLower(v), "127.0.0.1") || strings.Contains(strings.ToLower(v), "localhost")
	}
	out := env[:0]
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if isProxy(k) && hasLocalRef(v) {
			continue // 指向 Windows 本机代理：WSL 用不了，丢弃
		}
		out = append(out, kv)
	}
	return out
}

var hostDestructive = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+-rf\s+(/|/\*|~|/ [a-z])`),
	regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=.*of=/dev/sd`),
	regexp.MustCompile(`(?i)\bshutdown\b|\breboot\b|\bhalt\b|\bpoweroff\b`),
	regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:\s*&\s*\};:`), // fork bomb
}

// HostBashTimeout 是宿主 Bash 工具的默认单条命令墙钟超时（P3.5 子集：防单条挂死命令
// 占满整个 worker 回合）。命令可用 timeout_ms 参数按次覆盖。0 = 不设超时。
var HostBashTimeout = 120 * time.Second

// HostBash 构建一个在宿主 Linux bash（Windows=WSL）执行的 Bash 工具。
func HostBash() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "Bash",
		Description: "在宿主机执行 shell 命令（Windows 上经 WSL bash.exe 的 Linux bash 执行，" +
			"支持 curl/nmap/sqlmap/python3 等完整工具链与管道、重定向）。" +
			"需要【交互输入】的程序不要用 Bash（无 stdin），改用 shell_open。",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": "要执行的 bash 命令"},
				"timeout_ms": map[string]any{"type": "integer", "minimum": 1, "maximum": 600000},
			},
			"required": []any{"command"},
		},
		Permissions: func(ctx context.Context, in json.RawMessage, pc permission.Context) permission.Decision {
			if strings.EqualFold(os.Getenv("RESTXTRA_HOST_EXECUTION"), "disabled") {
				return permission.Denied("宿主命令执行已由部署策略禁用")
			}
			var a struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal(in, &a)
			for _, re := range hostDestructive {
				if re.MatchString(a.Command) {
					return permission.Denied("破坏性命令被安全边界拒绝")
				}
			}
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			timeout := HostBashTimeout
			var to struct {
				TimeoutMs int64 `json:"timeout_ms"`
			}
			_ = json.Unmarshal(in, &to)
			if to.TimeoutMs > 0 {
				timeout = time.Duration(to.TimeoutMs) * time.Millisecond
				if timeout > 10*time.Minute {
					timeout = 10 * time.Minute
				}
			}
			shell, flags := hostBashCmd()
			useWSL := runtime.GOOS == "windows"
			cmdLine := a.Command
			if useWSL && tc != nil && tc.WorkingDir != "" {
				cmdLine = "cd '" + strings.ReplaceAll(winToWslPath(tc.WorkingDir), "'", `'"'"'`) + "' && " + cmdLine
			}
			var cctx context.Context
			var cancel context.CancelFunc
			if timeout > 0 {
				cctx, cancel = context.WithTimeout(ctx, timeout)
			} else {
				cctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			cmd := exec.CommandContext(cctx, shell, append(flags, cmdLine)...)
			cmd.Env = ToolEnvironment(nil)
			if tc != nil {
				cmd.Dir = tc.WorkingDir
				if useWSL {
					cmd.Env = wslEnv(tc)
				} else {
					cmd.Env = ToolEnvironment(tc.Env)
				}
			}
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			out := actool.Capture(tc, buf.String())
			if cctx.Err() == context.DeadlineExceeded {
				return actool.Errorf(out + fmt.Sprintf("\n\n[command timed out after %s]", timeout)), nil
			}
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					if out == "" {
						out = "(no output)"
					}
					return actool.Errorf(fmt.Sprintf("%s\n\n[exit code %d]", out, ee.ExitCode())), nil
				}
				return actool.Errorf(fmt.Sprintf("%s\n\n[failed to run: %v]", out, err)), nil
			}
			if strings.TrimSpace(out) == "" {
				out = "(command produced no output)"
			}
			return actool.Text(out), nil
		},
	})
}

// withHostBash 把工具列表里的 SDK Bash 替换为走宿主 Linux bash 的实现。
func withHostBash(tools []actool.CoreTool) []actool.CoreTool {
	out := make([]actool.CoreTool, 0, len(tools))
	replaced := false
	for _, t := range tools {
		if t.Name() == "Bash" && !replaced {
			out = append(out, HostBash())
			replaced = true
			continue
		}
		out = append(out, t)
	}
	if !replaced {
		out = append(out, HostBash())
	}
	return out
}
