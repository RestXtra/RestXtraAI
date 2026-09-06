package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

// c2DangerousPostex marks post-exploitation modules that require human approval
// when the HITL fence is enabled (DesRedTeam-style dangerous-task gate).
var c2DangerousPostex = map[string]bool{
	"upload":   true, // 向目标写入文件（可能投放持久化/后门）
	"persist":  true, // 建立持久化
	"escalate": true, // 提权操作
}

// c2HitlSetting gates the dangerous-task approval fence. Default on.
const c2HitlSetting = "c2_hitl_enabled"

func (s *Server) c2HitlEnabled() bool {
	v, _, _ := s.m.pg.GetSetting(c2HitlSetting)
	return v != "off"
}

// c2ApprovalsList 返回待人工审批的危险 C2 任务。
func (s *Server) c2ApprovalsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListC2PendingApprovals()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []*db.C2Task{}
	}
	writeJSON(w, 200, map[string]any{"approvals": items})
}

// c2ApprovalDecide 审批危险任务（approve/reject）。
func (s *Server) c2ApprovalDecide(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	action := r.PathValue("action") // approve|reject
	if action != "approve" && action != "reject" {
		writeErr(w, 400, "action 必须为 approve|reject")
		return
	}
	t, err := s.m.pg.GetC2TaskByID(id)
	if err != nil || t == nil {
		writeErr(w, 404, "任务不存在")
		return
	}
	approval := "approved"
	if action == "reject" {
		approval = "rejected"
	}
	if err := s.m.pg.SetC2TaskApproval(id, approval); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{
		Actor: principalName(r), Category: "c2", Action: "approval",
		Result: action, Message: fmt.Sprintf("C2 危险任务 #%d %s 已%s", id, t.Command, map[string]string{"approve": "批准", "reject": "拒绝"}[action]),
		IP: clientIP(r),
	})
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "approval": approval})
}

// c2HitlGet / c2HitlSet 读取/切换危险任务审批围栏。
func (s *Server) c2HitlGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"enabled": s.c2HitlEnabled()})
}

func (s *Server) c2HitlSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	val := "off"
	if req.Enabled {
		val = "on"
	}
	if err := s.m.pg.SetSetting(c2HitlSetting, val); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": req.Enabled})
}

// logPostexKnowledge 边渗透边记录：后渗透任务完成后自动写入知识库。
// response 形如 {"module":"info","result":{...},"error":""}。
func (s *Server) logPostexKnowledge(t *db.C2Task) {
	if t == nil || !strings.HasPrefix(t.Command, "postex ") || t.State != "completed" {
		return
	}
	module := strings.TrimSpace(strings.TrimPrefix(t.Command, "postex "))
	if i := strings.IndexAny(module, " \t"); i >= 0 {
		module = module[:i]
	}
	var resp struct {
		Module string         `json:"module"`
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	_ = json.Unmarshal(t.Response, &resp)
	if resp.Error != "" || len(resp.Result) == 0 {
		return
	}
	snippet, _ := json.Marshal(resp.Result)
	if len(snippet) > 800 {
		snippet = snippet[:800]
	}
	_, _ = s.m.pg.SaveKnowledge(&db.KnowledgeItem{
		Title:   fmt.Sprintf("C2 后渗透 · %s · %s", t.SessionID, module),
		Content: fmt.Sprintf("会话 %s 后渗透模块 %s 结果：%s", t.SessionID, module, string(snippet)),
		Tags:    "c2,postex",
	})
}

func principalName(r *http.Request) string {
	if p, ok := principalOf(r); ok {
		return p.Username
	}
	return ""
}

// c2AutoPostexSetting gates the automatic post-exploitation flow on new beacon
// sessions. Default on.
const c2AutoPostexSetting = "c2_auto_postex"

func (s *Server) c2AutoPostexEnabled() bool {
	v, _, _ := s.m.pg.GetSetting(c2AutoPostexSetting)
	return v != "off"
}

// startAutoPostex launches a RestXtraAI task that drives the post-exploitation
// flow against a freshly-registered C2 beacon session. The task runs on the
// platform's planner/worker engine; the worker has c2_postex / c2_task_result
// bound so the AI can execute modules and collect structured results.
func (s *Server) startAutoPostex(listenerID int64, sessionID, host string) {
	if s.c2m == nil || !s.c2AutoPostexEnabled() {
		return
	}
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	desc := fmt.Sprintf("C2 后渗透 · %s", sessionID)
	goal := fmt.Sprintf(
		"对 C2 会话 %s（主机 %s）执行标准后渗透流程并输出发现报告。\n"+
			"流程：\n"+
			"1. 用 c2_postex 依次执行 info、whoami、netstat、ps、users、env 收集基础信息（每次用 c2_task_result 轮询结果）；\n"+
			"2. 根据系统类型执行 escalate（提权侦察）与 persist（持久化侦察）；\n"+
			"3. 需要时用 download 获取关键文件、ls 浏览目录；\n"+
			"4. 汇总输出：主机指纹(OS/内核/主机名)、开放端口与网络连接、当前用户与权限、提权机会、持久化机会、可疑进程。\n"+
			"工具约定：c2_postex 的 session_id 固定为 %s，module 为模块名；c2_task_result 传回 task_id 取结果。",
		sessionID, host, sessionID)

	t, err := s.m.CreateTask(desc, goal, nil, 0, 0, nil)
	if err != nil {
		log.Printf("[c2] 自动后渗透任务创建失败 (%s): %v", sessionID, err)
		return
	}
	log.Printf("[c2] 新会话 %s 自动启动后渗透任务 #%s", sessionID, t.ID)
	s.seed(t, desc+" "+goal)

	// Mirror the interactive createTask flow: decompose goals, then run the engine.
	go func() {
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "round",
			Summary: "第 0 轮目标拆解（自动后渗透）"})
		goals := s.createGoals(s.ctx, t, func(r db.Activity) {
			s.engine.emitActivity(t, r)
		})
		for _, g := range goals {
			summary := g.Text
			if g.VulnClass != "" {
				summary = fmt.Sprintf("[%s] %s", g.VulnClass, g.Text)
			}
			s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "text", Summary: summary})
		}
		s.engine.Run(s.ctx, t)
	}()
}
