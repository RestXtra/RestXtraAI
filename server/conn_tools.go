package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/RestXtra/RestXtraAI/db"
	"golang.org/x/crypto/ssh"
)

// ---- 连接管理 agent 工具 (conn_*) ----
// P3: 让 agent 经受管连接(SSH/WebShell;RDP/Telnet 后续)在目标主机执行命令。
// P4: 危险命令在 conn_hitl_enabled 开启时拒绝自动执行(需人工批准,见 responder 切片)。

const connHitlSetting = "conn_hitl_enabled"

func (s *Server) connHitlEnabled() bool {
	v, _, _ := s.m.pg.GetSetting(connHitlSetting)
	return v != "off"
}

// connDangerousRe 匹配视为危险、需人工批准的远程命令(保守默认:宁可拒绝)。
var connDangerousRe = regexp.MustCompile(`(?i)(\brm\s+-[a-z]*|\bshutdown\b|\breboot\b|\bhalt\b|\bpoweroff\b|\biptables\b|\bnft\b|\bfirewall-cmd\b|\bufw\b|\bservice\s+\S+\s+stop\b|\bsystemctl\s+(stop|disable|mask)\b|\bkill\s+-9\b|\bpkill\b|\btaskkill\b|\bdel\s+/[a-z]*[qf]\b|\bformat\s+[a-z]:\b|\bdd\s+if=|\bmkfs\b|\buserdel\b|\bgroupdel\b|(^|[^/\w])passwd\b|\bchmod\s+[0-7]{3,}\s+|\bchown\s+\S+\s+/)`)

// sshRun 通过 x/crypto/ssh 在受管连接上执行一条命令并返回输出。
func sshRun(c db.Connection, cmd string) (string, error) {
	port := c.Port
	if port == 0 {
		port = 22
	}
	user := c.Username
	if user == "" {
		user = "root"
	}
	var cfg struct {
		PrivateKey string `json:"private_key"`
	}
	_ = json.Unmarshal(c.Config, &cfg)
	auth := []ssh.AuthMethod{}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return "", fmt.Errorf("私钥解析失败: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if c.Secret != "" {
		auth = append(auth, ssh.Password(c.Secret))
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("未配置密码或私钥")
	}
	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, port), sshCfg)
	if err != nil {
		return "", err
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

// toolConnList 让 agent 列出已配置的受管连接,作为执行命令的目标清单。
func (s *Server) toolConnList() actool.CoreTool {
	return roTool("conn_list",
		"列出所有已配置的受管连接(SSH/WebShell/RDP/Telnet),返回 id/name/kind/host/port/username,供选择目标主机执行命令。",
		objSchema(map[string]any{}),
		func(_ context.Context, _ json.RawMessage) (actool.Result, error) {
			conns, err := s.m.pg.ListConnections("")
			if err != nil {
				return actool.Errorf("查询失败: " + err.Error()), nil
			}
			type row struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Kind     string `json:"kind"`
				Host     string `json:"host"`
				Port     int    `json:"port"`
				Username string `json:"username"`
				Enabled  bool   `json:"enabled"`
			}
			rows := make([]row, 0, len(conns))
			for _, c := range conns {
				rows = append(rows, row{ID: c.ID, Name: c.Name, Kind: c.Kind, Host: c.Host, Port: c.Port, Username: c.Username, Enabled: c.Enabled})
			}
			raw, _ := json.Marshal(map[string]any{"count": len(rows), "connections": rows})
			return actool.Text(string(raw)), nil
		})
}

// toolConnExec 让 agent 在指定受管连接上执行一条命令。SSH/WebShell 真实执行;
// RDP/Telnet 待接;危险命令在审批围栏开启时提交人工审批(只能 propose)。
func (s *Server) toolConnExec() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "conn_exec",
		Description: "在指定受管连接(id 来自 conn_list)上执行一条命令并返回输出。支持 SSH 与 WebShell;RDP/Telnet 待接。危险命令(删除/关机/防火墙/停止服务/杀进程/格式化/改密码等)在审批围栏开启时提交人工审批,需用户批准后才会执行。",
		Schema: objSchema(map[string]any{
			"id":        strParam("受管连接 ID(conn_list 返回的 id)"),
			"command":   strParam("要执行的远程命令,如 'whoami'、'ipconfig'、'cat /etc/passwd'"),
			"rationale": strParam("危险命令时的执行理由(可选,审批人可见)"),
		}, "id", "command"),
		ReadOnly:   func(json.RawMessage) bool { return false },
		Concurrent: func(json.RawMessage) bool { return false },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var a struct {
				ID        int64  `json:"id"`
				Command   string `json:"command"`
				Rationale string `json:"rationale"`
			}
			_ = json.Unmarshal(in, &a)
			if a.ID == 0 {
				return actool.Errorf("connection id 必填"), nil
			}
			if strings.TrimSpace(a.Command) == "" {
				return actool.Errorf("command 必填"), nil
			}
			c, err := s.m.pg.GetConnection(a.ID)
			if err != nil || c == nil {
				return actool.Errorf(fmt.Sprintf("连接 %d 不存在", a.ID)), nil
			}
			if !c.Enabled {
				return actool.Errorf(fmt.Sprintf("连接 %q 已停用", c.Name)), nil
			}
			actor := "agent"
			if tc != nil && tc.AgentID != "" {
				actor = tc.AgentID
			}
			// P4/P5: 危险命令门控——开启时提交人工审批(LLM 只能 propose)。
			if s.connHitlEnabled() && connDangerousRe.MatchString(a.Command) {
				aid, err := s.m.pg.CreateConnAction(&db.ConnAction{
					ConnectionID: a.ID, Kind: "exec", Command: a.Command,
					Rationale: a.Rationale, RequestedBy: actor,
				})
				if err != nil {
					return actool.Errorf(fmt.Sprintf("提交审批失败: %v", err)), nil
				}
				_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "conn", Action: "exec_submit", Result: "pending",
					Message: fmt.Sprintf("连接 %s(%s) 危险命令已提交审批 #%d: %s", c.Name, c.Kind, aid, a.Command)})
				return actool.Text(fmt.Sprintf("该命令被判定为危险操作(%s),已提交人工审批(审批 id=%d)。请用户到「连接管理→审批」页批准后才执行,不要重复提交。", c.Kind, aid)), nil
			}
			out, err := s.runConnCommand(*c, a.Command)
			res := "success"
			if err != nil {
				res = "failure"
			}
			_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "conn", Action: "exec", Result: res,
				Message: fmt.Sprintf("连接 %s(%s) 执行命令: %s", c.Name, c.Kind, a.Command)})
			if err != nil {
				return actool.Errorf(fmt.Sprintf("执行失败: %v", err)), nil
			}
			if len(out) > 8000 {
				out = out[:8000] + "\n...[输出已截断]"
			}
			return actool.Text(fmt.Sprintf("连接 %s(%s) 执行结果:\n%s", c.Name, c.Kind, out)), nil
		},
	})
}

// runConnCommand dispatches a command to a connection by kind (ssh / webshell;
// rdp / telnet 占位)。
func (s *Server) runConnCommand(c db.Connection, cmd string) (string, error) {
	switch c.Kind {
	case "ssh":
		return sshRun(c, cmd)
	case "rdp":
		return "", fmt.Errorf("kind=rdp 是图形协议，无命令执行通道（grdp 用于验凭据/截图，见 §15.2）")
	case "telnet":
		return telnetRun(c.Host, c.Port, cmd, 8*time.Second)
	default: // webshell
		var cfg struct {
			Type    string `json:"type"`
			Headers string `json:"headers"`
		}
		_ = json.Unmarshal(c.Config, &cfg)
		if cfg.Headers == "" {
			cfg.Headers = "{}"
		}
		out, _, err := webshellExec(c.Host, cfg.Type, c.Secret, cfg.Headers, cmd)
		return out, err
	}
}

// connContainCommand 把遏制动作翻译成可执行的远程命令(按动作/协议尽力而为)。
func connContainCommand(action, target string) string {
	switch action {
	case "isolate", "block_ip":
		return "iptables -A INPUT -s " + target + " -j DROP"
	case "kill_process":
		return "kill -9 " + target
	default:
		return ""
	}
}

// connContainVerifyCmd 返回确认遏制动作是否生效的回查命令(仅 Linux 语义,尽力而为)。
func connContainVerifyCmd(action, target string) string {
	switch action {
	case "isolate", "block_ip":
		return "iptables -L INPUT -n | grep " + target
	case "kill_process":
		return "ps -p " + target
	default:
		return ""
	}
}

// verifyContainment 判定回查输出是否符合预期:
//   - block_ip/isolate: 回查输出应包含目标 IP(iptables 规则已存在)
//   - kill_process: 回查输出不应包含目标 pid(进程已终止)
func verifyContainment(action, target, out string) bool {
	switch action {
	case "isolate", "block_ip":
		return strings.Contains(out, target)
	case "kill_process":
		return !strings.Contains(out, target)
	default:
		return false
	}
}

// runContainVerify 执行遏制动作 + 回查验证,返回动作输出与验证结果。
func (s *Server) runContainVerify(c db.Connection, action, target, cmd string) (string, bool, error) {
	out, err := s.runConnCommand(c, cmd)
	if err != nil {
		return out, false, err
	}
	vcmd := connContainVerifyCmd(action, target)
	if vcmd == "" {
		return out, false, nil
	}
	vout, verr := s.runConnCommand(c, vcmd)
	if verr != nil {
		return out, false, nil // 回查失败 → 未验证(不等于失败,保守置 unverified)
	}
	return out, verifyContainment(action, target, vout), nil
}

// toolConnContain 让 agent 提出遏制动作(隔离/封IP/杀进程),危险操作默认提交人工审批。
func (s *Server) toolConnContain() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "conn_contain",
		Description: "对受管连接提出遏制动作(隔离/封IP/杀进程)以遏制威胁。动作属于危险操作,默认提交人工审批,批准后才执行。action: isolate(隔离该源IP) | block_ip(封禁源IP) | kill_process(杀进程, target 填 pid)。",
		Schema: objSchema(map[string]any{
			"id":        strParam("受管连接 ID(conn_list 返回的 id)"),
			"action":    strParam("isolate | block_ip | kill_process"),
			"target":    strParam("目标:block_ip/isolate 填源 IP;kill_process 填 pid"),
			"rationale": strParam("遏制理由(必填,审批人可见)"),
		}, "id", "action", "target", "rationale"),
		ReadOnly:   func(json.RawMessage) bool { return false },
		Concurrent: func(json.RawMessage) bool { return false },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var a struct {
				ID        int64  `json:"id"`
				Action    string `json:"action"`
				Target    string `json:"target"`
				Rationale string `json:"rationale"`
			}
			_ = json.Unmarshal(in, &a)
			if a.ID == 0 {
				return actool.Errorf("connection id 必填"), nil
			}
			if a.Action != "isolate" && a.Action != "block_ip" && a.Action != "kill_process" {
				return actool.Errorf("action 需为 isolate | block_ip | kill_process"), nil
			}
			if strings.TrimSpace(a.Target) == "" {
				return actool.Errorf("target 必填(block_ip/isolate 填源 IP;kill_process 填 pid)"), nil
			}
			if strings.TrimSpace(a.Rationale) == "" {
				return actool.Errorf("rationale 必填(审批人可见)"), nil
			}
			c, err := s.m.pg.GetConnection(a.ID)
			if err != nil || c == nil {
				return actool.Errorf(fmt.Sprintf("连接 %d 不存在", a.ID)), nil
			}
			if !c.Enabled {
				return actool.Errorf(fmt.Sprintf("连接 %q 已停用", c.Name)), nil
			}
			actor := "agent"
			if tc != nil && tc.AgentID != "" {
				actor = tc.AgentID
			}
			cmd := connContainCommand(a.Action, a.Target)
			if cmd == "" {
				return actool.Errorf("未知遏制动作: " + a.Action), nil
			}
			// 遏制动作默认走人工审批(危险,常含不可逆操作)。
			if s.connHitlEnabled() {
				aid, err := s.m.pg.CreateConnAction(&db.ConnAction{
					ConnectionID: a.ID, Kind: "contain", Action: a.Action, Target: a.Target, Command: cmd,
					Rationale: a.Rationale, RequestedBy: actor,
				})
				if err != nil {
					return actool.Errorf(fmt.Sprintf("提交审批失败: %v", err)), nil
				}
				_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "conn", Action: "contain_submit", Result: "pending",
					Message: fmt.Sprintf("连接 %s(%s) 遏制动作已提交审批 #%d: %s %s", c.Name, c.Kind, aid, a.Action, a.Target)})
				return actool.Text(fmt.Sprintf("遏制动作 %s(%s) 已提交人工审批(审批 id=%d)。请用户到「连接管理→审批」页批准后才执行,不要重复提交。", a.Action, a.Target, aid)), nil
			}
			out, verified, err := s.runContainVerify(*c, a.Action, a.Target, cmd)
			if err != nil {
				return actool.Errorf(fmt.Sprintf("遏制执行失败: %v", err)), nil
			}
			vstr := "未验证"
			if verified {
				vstr = "已确认"
			}
			return actool.Text(fmt.Sprintf("遏制动作 %s(%s) 已执行(verify: %s):\n%s", a.Action, a.Target, vstr, out)), nil
		},
	})
}
