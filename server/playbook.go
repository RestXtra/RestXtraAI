package server

import (
	"net/http"

	"github.com/RestXtra/RestXtraAI/db"
)

// Attack-pattern library / playbook API (platform built-in).

// GET /api/playbook/patterns — list with filters (technique/cve/verification/tag).
func (s *Server) playbookListPatterns(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	q := r.URL.Query()
	limit := atoiDefault(q.Get("limit"), 100)
	offset := atoiDefault(q.Get("offset"), 0)
	items, total, err := pg.ListAttackPatterns(limit, offset, q.Get("technique"), q.Get("cve"), q.Get("verification"), q.Get("tag"))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []*db.AttackPattern{}
	}
	writeJSON(w, 200, map[string]any{"patterns": items, "total": total})
}

// POST /api/playbook/patterns — create a pattern.
func (s *Server) playbookCreatePattern(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var p db.AttackPattern
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if p.Title == "" {
		writeErr(w, 400, "标题不能为空")
		return
	}
	created, err := pg.CreateAttackPattern(&p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "playbook", "create_pattern", "success", "新增攻击模式 "+created.Title)
	writeJSON(w, 201, created)
}

// DELETE /api/playbook/patterns/{id} — delete a pattern.
func (s *Server) playbookDeletePattern(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := r.PathValue("id")
	n, err := pg.DeleteAttackPattern(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "playbook", "delete_pattern", "success", "删除攻击模式 "+id)
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// POST /api/playbook/search — dual-path retrieval over the library.
func (s *Server) playbookSearch(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		CVE        string   `json:"cve"`
		Technique  string   `json:"technique"`
		Components []string `json:"components"`
		Keywords   string   `json:"keywords"`
		Limit      int      `json:"limit"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	results, err := pg.SearchPlaybook(db.PlaybookQuery{
		CVE: req.CVE, Technique: req.Technique, Components: req.Components, Keywords: req.Keywords,
	}, req.Limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if results == nil {
		results = []*db.PlaybookSearchResult{}
	}
	writeJSON(w, 200, map[string]any{"results": results})
}

// GET /api/playbook/stats — counts by verification + total.
func (s *Server) playbookStats(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	patterns, total, err := pg.ListAttackPatterns(100000, 0, "", "", "", "")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	counts := map[string]int{"draft": 0, "validated": 0, "reference": 0}
	for _, p := range patterns {
		if _, ok := counts[p.Verification]; ok {
			counts[p.Verification]++
		}
	}
	writeJSON(w, 200, map[string]any{"total": total, "counts": counts})
}
