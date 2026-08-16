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

	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/permission"
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

// wslEnv 过滤掉指向 Windows 本机（localhost/127.0.0.1）的代理与 CA 变量——
// WSL2 NAT 用不了它们，反而触发 localhost 代理提示并污染输出。保留其余 env。
func wslEnv(tc *actool.ToolContext) []string {
	env := os.Environ()
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
	if tc != nil {
		for _, kv := range tc.Env {
			i := strings.IndexByte(kv, '=')
			if i <= 0 {
				continue
			}
			k, v := kv[:i], kv[i+1:]
			if isProxy(k) && hasLocalRef(v) {
				continue // 指向 Windows 本机代理：WSL 用不了，丢弃
			}
			env = append(env, kv)
		}
	}
	return env
}

var hostDestructive = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+-rf\s+(/|/\*|~|/ [a-z])`),
	regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=.*of=/dev/sd`),
	regexp.MustCompile(`(?i)\bshutdown\b|\breboot\b|\bhalt\b|\bpoweroff\b`),
	regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:\s*&\s*\};:`), // fork bomb
}

// HostBash 构建一个在宿主 Linux bash（Windows=WSL）执行的 Bash 工具。
func HostBash() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "Bash",
		Description: "在宿主机执行 shell 命令（Windows 上经 WSL bash.exe 的 Linux bash 执行，" +
			"支持 curl/nmap/sqlmap/python3 等完整工具链与管道、重定向）。" +
			"需要【交互输入】的程序不要用 Bash（无 stdin），改用 shell_open。",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"command": map[string]any{"type": "string", "description": "要执行的 bash 命令"}},
			"required":   []any{"command"},
		},
		Permissions: func(ctx context.Context, in json.RawMessage, pc permission.Context) permission.Decision {
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
			timeout := 120 * time.Second
			var to struct {
				TimeoutMs int64 `json:"timeout_ms"`
			}
			_ = json.Unmarshal(in, &to)
			if to.TimeoutMs > 0 {
				timeout = time.Duration(to.TimeoutMs) * time.Millisecond
			}
			shell, flags := hostBashCmd()
			useWSL := runtime.GOOS == "windows"
			cmdLine := a.Command
			if useWSL && tc != nil && tc.WorkingDir != "" {
				cmdLine = "cd " + winToWslPath(tc.WorkingDir) + " && " + cmdLine
			}
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(cctx, shell, append(flags, cmdLine)...)
			if tc != nil {
				cmd.Dir = tc.WorkingDir
				if len(tc.Env) > 0 {
					if useWSL {
						cmd.Env = wslEnv(tc)
					} else {
						cmd.Env = append(os.Environ(), tc.Env...)
					}
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
