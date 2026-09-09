package server

import (
	"fmt"
	"net/http"

	"github.com/RestXtra/RestXtraAI/db"
)

// ---- conn_* 危险动作 HITL 审批 ----
// soc-autopilot 风格:agent 只能 propose;人工批准后由服务端执行并回写结果。

// connApprovalsList 返回待人工审批的连接危险动作。
func (s *Server) connApprovalsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListConnPendingApprovals()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []*db.ConnAction{}
	}
	// 附带连接名方便审批人判断。
	type row struct {
		*db.ConnAction
		ConnName string `json:"conn_name"`
		ConnKind string `json:"conn_kind"`
		ConnHost string `json:"conn_host"`
	}
	out := make([]row, 0, len(items))
	for _, a := range items {
		cn := "?"
		ck := "?"
		ch := "?"
		if c, err := s.m.pg.GetConnection(a.ConnectionID); err == nil && c != nil {
			cn, ck, ch = c.Name, c.Kind, c.Host
		}
		out = append(out, row{ConnAction: a, ConnName: cn, ConnKind: ck, ConnHost: ch})
	}
	writeJSON(w, 200, map[string]any{"approvals": out})
}

// connActionsList 返回某个连接的动作历史。
func (s *Server) connActionsList(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	items, err := s.m.pg.ListConnActions(id, 100)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []*db.ConnAction{}
	}
	writeJSON(w, 200, map[string]any{"actions": items})
}

// connApprovalDecide 审批连接危险动作(approve/reject)。批准后立即在目标连接上执行。
func (s *Server) connApprovalDecide(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	action := r.PathValue("action") // approve|reject
	if action != "approve" && action != "reject" {
		writeErr(w, 400, "action 必须为 approve|reject")
		return
	}
	a, err := s.m.pg.GetConnAction(id)
	if err != nil || a == nil {
		writeErr(w, 404, "审批项不存在")
		return
	}
	if a.State != "pending" {
		writeErr(w, 400, "该审批项已处理(state="+a.State+")")
		return
	}
	actor := principalName(r)
	if actor == "" {
		actor = "console"
	}
	if action == "reject" {
		if err := s.m.pg.SetConnActionApproval(id, "rejected", actor); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "conn", Action: "approval", Result: "rejected",
			Message: fmt.Sprintf("拒绝连接危险动作 #%d: %s", id, a.Command)})
		writeJSON(w, 200, map[string]any{"ok": true, "id": id, "approval": "rejected"})
		return
	}
	// approve → 执行。
	c, err := s.m.pg.GetConnection(a.ConnectionID)
	if err != nil || c == nil {
		writeErr(w, 404, "连接不存在(可能已被删除)")
		return
	}
	if err := s.m.pg.SetConnActionApproval(id, "approved", actor); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out, err := s.runConnCommand(*c, a.Command)
	state := "executed"
	if err != nil {
		state = "failed"
		out = "执行失败: " + err.Error()
	}
	// 遏制动作执行后回查验证(verify,不假设已生效)。
	verified := false
	if a.Kind == "contain" && err == nil {
		vcmd := connContainVerifyCmd(a.Action, a.Target)
		if vcmd != "" {
			if vout, verr := s.runConnCommand(*c, vcmd); verr == nil {
				verified = verifyContainment(a.Action, a.Target, vout)
				out += "\n--- verify 回查 ---\n" + vout
			} else {
				out += "\n--- verify 回查失败(未验证) ---\n" + verr.Error()
			}
		}
		if !verified {
			state = "executed_unverified"
		}
	}
	if len(out) > 2000 {
		out = out[:2000] + "\n...[截断]"
	}
	_ = s.m.pg.SetConnActionResult(id, state, out)
	vnote := "(未验证)"
	if verified {
		vnote = "(已回查验证)"
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: actor, Category: "conn", Action: "approval", Result: "approved",
		Message: fmt.Sprintf("批准并执行连接危险动作 #%d: %s → %s%s", id, a.Command, state, vnote)})
	writeJSON(w, 200, map[string]any{
		"ok": true, "id": id, "approval": "approved", "state": state, "verified": verified,
		"result": truncateJSON(out),
	})
}

func truncateJSON(s string) string {
	if len(s) > 1000 {
		return s[:1000] + "..."
	}
	return s
}
