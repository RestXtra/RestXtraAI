package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/RestXtra/RestXtraAI/db"
	"github.com/RestXtra/RestXtraAI/intercept"
)

// ---- weak_password_probe agent 工具 ----
// 把有界弱口令探测（weakpass.go）暴露给进攻 agent，前置人工审批(HITL)+审计。
// 泄露点：仅命中口令会回显（探测目的），失败尝试在 Detail 中打码。

const weakpassHitlSetting = "weakpass_hitl_enabled"

func (s *Server) weakpassHITLEnabled() bool {
	v, _, _ := s.m.pg.GetSetting(weakpassHitlSetting)
	return v != "off"
}

func intParam(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func strArrParam(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func strMapParam(desc string) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": desc}
}

// toolWeakPasswordProbe 是有界弱口令探测工具：小范围、单线程、命中即停、遇锁定熔断、需审批。
func (s *Server) toolWeakPasswordProbe() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name: "weak_password_probe",
		Description: "有界弱口令探测：对**已授权**目标做小范围登录尝试（http-form/http-basic/ssh/rdp/telnet）。" +
			"严格受限：单线程、默认≤20 次(硬上限 50)、命中即停、遇锁定/限流/验证码立即熔断，且需人工审批。" +
			"不做字典爆破；仅用于授权范围内、且操作者已同意的小范围弱口令尝试。",
		Schema: objSchema(map[string]any{
			"kind":           strParam("http-form | http-basic | ssh | rdp | telnet"),
			"url":            strParam("http-* 的目标 URL（含 http://或 https://）"),
			"host":           strParam("ssh/rdp/telnet 的目标主机（IP 或域名）"),
			"port":           intParam("端口（可选；ssh 默认22 / rdp 3389 / telnet 23）"),
			"username":       strParam("单个用户名（与 users 二选一/可并用）"),
			"users":          strArrParam("用户名列表（≤5）"),
			"passwords":      strArrParam("自定义口令列表（≤50）；省略则用内置常见弱口令小字典"),
			"user_field":     strParam("http-form 用户名字段名（默认 username）"),
			"pass_field":     strParam("http-form 口令字段名（默认 password）"),
			"extra_fields":   strMapParam("http-form 附加表单字段（JSON 对象，如 {\"csrf\":\"...\"}）"),
			"success_marker": strParam("http-form/telnet 命中判定：响应体包含该子串即成功"),
			"fail_marker":    strParam("http-form 失败判定：响应体包含该子串即失败"),
			"success_status": intParam("http-form 命中判定：响应 HTTP 状态码（如 302）"),
			"user_prompt":    strParam("telnet 用户名提示符（可选）"),
			"pass_prompt":    strParam("telnet 口令提示符（可选）"),
			"max_attempts":   intParam("尝试上限（默认20，硬上限50）"),
			"delay_ms":       intParam("每次尝试间隔毫秒（默认250，最小单线程顺序执行）"),
			"rationale":      strParam("探测理由（审批人可见）"),
		}, "kind"),
		ReadOnly:   func(json.RawMessage) bool { return false },
		Concurrent: func(json.RawMessage) bool { return false },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(ctx context.Context, in json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			var a struct {
				weakpassSpec
				Rationale string `json:"rationale"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			// 预校验（构造验证器；出错即返回，不进入审批）。
			if _, err := s.weakpassVerifier(a.weakpassSpec); err != nil {
				return actool.Errorf(err.Error()), nil
			}

			actor := "agent"
			if tc != nil && tc.AgentID != "" {
				actor = tc.AgentID
			}
			est := weakpassSummary(a.weakpassSpec)

			// HITL：默认开启，提交人工审批；未批准不执行任何尝试。
			if s.weakpassHITLEnabled() {
				ok := false
				if s.m != nil && s.m.interceptor != nil {
					convID := intercept.ConvIDFromContext(ctx)
					dec := intercept.Decision{Action: "ask", Message: "弱口令探测需人工确认：" + est + "（单线程/命中即停/遇锁定即停）"}
					ok = s.m.interceptor.HandleAsk(ctx, convID, dec, "weak_password_probe", in)
				}
				if !ok {
					return actool.Text("弱口令探测需人工审批：已提交审批或未获批准，未执行任何尝试。请操作者到「审批」页批准后重试。"), nil
				}
			}

			_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "weakpass", Action: "probe_start", Result: "running",
				Message: "弱口令探测启动: " + est})

			runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			out, err := s.runWeakPasswordProbe(runCtx, a.weakpassSpec)
			if err != nil {
				_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "weakpass", Action: "probe", Result: "failure",
					Message: "弱口令探测失败: " + err.Error()})
				return actool.Errorf("探测失败: " + err.Error()), nil
			}
			res := "clean"
			if len(out.Found) > 0 {
				res = "found"
			} else if out.Locked {
				res = "locked"
			}
			_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "weakpass", Action: "probe", Result: res,
				Message: fmt.Sprintf("弱口令探测 %s 尝试%d次 命中%d 熔断=%v", out.Target, out.Attempts, len(out.Found), out.Locked)})
			return actool.Text(formatWeakpassOutcome(out)), nil
		},
	})
}

// weakpassSummary 生成审批用的一句话摘要（不执行探测）。
func weakpassSummary(spec weakpassSpec) string {
	nu := len(weakpassCleanList(spec.Users, weakpassMaxUsers))
	if strings.TrimSpace(spec.Username) != "" {
		nu++
	}
	np := len(weakpassCleanList(spec.Passwords, weakpassMaxPasswords))
	if np == 0 {
		np = len(weakpassBuiltin)
	}
	maxA := spec.MaxAttempts
	if maxA <= 0 {
		maxA = weakpassMaxAttemptsSoft
	}
	if maxA > weakpassMaxAttemptsHard {
		maxA = weakpassMaxAttemptsHard
	}
	return fmt.Sprintf("kind=%s target=%s 用户%d个 口令%d个 上限%d次", spec.Kind, spec.target(), nu, np, maxA)
}

func formatWeakpassOutcome(out *weakpassOutcome) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("弱口令探测结果（%s，目标 %s）\n", out.Kind, out.Target))
	b.WriteString(fmt.Sprintf("尝试次数: %d / 上限 %d\n", out.Attempts, out.MaxAttempts))
	if len(out.Found) > 0 {
		b.WriteString("命中弱口令:\n")
		for _, h := range out.Found {
			b.WriteString(fmt.Sprintf("  - %s / %s\n", h.Username, h.Password))
		}
	} else {
		b.WriteString("命中弱口令: 无\n")
	}
	if out.Locked {
		b.WriteString("注意: 疑似触发锁定/限流/验证码，已熔断（不要继续对该目标尝试）。\n")
	} else if out.Aborted {
		b.WriteString("已停止: " + out.AbortReason + "\n")
	}
	if len(out.Detail) > 0 {
		b.WriteString("过程摘要:\n")
		for _, d := range out.Detail {
			b.WriteString("  " + d + "\n")
		}
	}
	return b.String()
}

// seedWeakpassAgentBindings 一次性(flag 门控)把 weak_password_probe 绑定给进攻 agent。
func (s *Server) seedWeakpassAgentBindings() {
	const flag = "weakpass_agent_bindings_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	keys := []string{"weak_password_probe"}
	for _, ag := range []string{"worker", "auto", "web_vuln", "exploit", "pentest_chain", "red_team_lead", "evasion", "pentest"} {
		if err := s.m.pg.AddAgentToToolBinding(ag, keys); err != nil {
			log.Printf("[weakpass] %s 绑定失败: %v", ag, err)
			return
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
	s.toolCatalog.Invalidate()
}
