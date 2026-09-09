package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

// ---- 安全事件(incident) + 自动 spawn responder ----
// P7: incidents 表承载事件简报；respond 端点/webhook 自动 spawn responder 任务
// (goal=brief,复用 startAutoPostex 模式;worker 已绑 conn_* 工具)。

// incidentsList 返回事件列表(可按状态过滤)。
func (s *Server) incidentsList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	items, err := s.m.pg.ListIncidents(status, 100)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []*db.Incident{}
	}
	writeJSON(w, 200, map[string]any{"incidents": items})
}

func (s *Server) incidentsCreate(w http.ResponseWriter, r *http.Request) {
	var i db.Incident
	if err := decode(r, &i); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(i.Title) == "" {
		writeErr(w, 400, "标题必填")
		return
	}
	id, err := s.m.pg.CreateIncident(&i)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: principalName(r), Category: "incident", Action: "create",
		Result: "success", Message: fmt.Sprintf("创建事件 #%d %s", id, i.Title), IP: clientIP(r)})
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) incidentsGet(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	i, err := s.m.pg.GetIncident(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if i == nil {
		writeErr(w, 404, "事件不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"incident": i})
}

func (s *Server) incidentsUpdate(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	var i db.Incident
	if err := decode(r, &i); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	i.ID = id
	if err := s.m.pg.UpdateIncident(&i); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: principalName(r), Category: "incident", Action: "update",
		Result: "success", Message: fmt.Sprintf("更新事件 #%d → status=%s", id, i.Status), IP: clientIP(r)})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) incidentsDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteIncident(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// spawnIncidentResponder 启动一个 responder 任务(goal=事件简报)并关联到事件。
func (s *Server) spawnIncidentResponder(inc *db.Incident) (string, error) {
	brief := fmt.Sprintf("标题: %s\n来源: %s\n严重级别: %s\n告警信息: %s\n受影响资产: %s\nIOC: %s\n与运维/开发交流的零散信息: %s",
		inc.Title, inc.Source, inc.Severity, inc.AlertInfo, inc.Assets, inc.IOCs, inc.Notes)
	goal := fmt.Sprintf(
		"对事件简报执行标准应急响应全流程并输出发现报告。\n事件简报：\n%s\n\n流程：\n"+
			"1. 研判简报：梳理已知事实/疑点/范围；\n"+
			"2. 用 conn_list 看可用连接，按受影响资产选目标；\n"+
			"3. 用 conn_exec 排查（进程/网络/日志/计划任务/异常文件），每次用真实返回推进；\n"+
			"4. 确认 IOC/恶意进程后用 conn_contain 提出遏制（危险动作自动走人工审批，带 rationale，不重复提交）；\n"+
			"5. 整理证据 search_knowledge/insert_assets 登记；\n"+
			"6. 用人话总结并用 report_finding 登记高危结论。\n"+
			"工具约定：conn_exec 的 id 来自 conn_list；conn_contain 需带 rationale。", brief)

	desc := fmt.Sprintf("应急响应 · %s", inc.Title)
	t, err := s.m.CreateTask(desc, goal, nil, 0, 0, nil)
	if err != nil {
		return "", err
	}
	s.seed(t, desc+" "+goal)
	go func() {
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "round",
			Summary: "第 0 轮目标拆解（自动应急响应）"})
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
	return t.ID, nil
}

// incidentsRespond 手动触发:对事件 spawn responder 任务。
func (s *Server) incidentsRespond(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	inc, err := s.m.pg.GetIncident(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if inc == nil {
		writeErr(w, 404, "事件不存在")
		return
	}
	taskID, err := s.spawnIncidentResponder(inc)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.m.pg.AttachIncidentTask(id, taskID)
	_ = s.m.pg.SetIncidentStatus(id, "triaging")
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: principalName(r), Category: "incident", Action: "respond",
		Result: "success", Message: fmt.Sprintf("事件 #%d spawn responder 任务 #%s", id, taskID), IP: clientIP(r)})
	writeJSON(w, 200, map[string]any{"ok": true, "task_id": taskID})
}

// incidentsWebhook 外部 SIEM/工单推送:建事件并自动 spawn responder(镜像 startAutoPostex)。
// 请求体: {title,severity,source,alert_info,notes,assets,iocs}
func (s *Server) incidentsWebhook(w http.ResponseWriter, r *http.Request) {
	var i db.Incident
	if err := decode(r, &i); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(i.Title) == "" {
		writeErr(w, 400, "title 必填")
		return
	}
	id, err := s.m.pg.CreateIncident(&i)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	inc, _ := s.m.pg.GetIncident(id)
	taskID, err := s.spawnIncidentResponder(inc)
	if err != nil {
		log.Printf("[incident] webhook #%d spawn responder 失败: %v", id, err)
		writeJSON(w, 200, map[string]any{"id": id, "responder": "failed"})
		return
	}
	_ = s.m.pg.AttachIncidentTask(id, taskID)
	_ = s.m.pg.SetIncidentStatus(id, "triaging")
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: principalName(r), Category: "incident", Action: "webhook",
		Result: "success", Message: fmt.Sprintf("webhook 建事件 #%d → responder 任务 #%s", id, taskID), IP: clientIP(r)})
	writeJSON(w, 200, map[string]any{"id": id, "task_id": taskID})
}
