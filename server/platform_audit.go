package server

import (
	"net/http"

	"github.com/RestXtra/RestXtraAI/db"
)

// GET /api/audit/logs — paginated platform audit query (newest first).
// Query params: category, action, result, actor, limit, offset.
func (s *Server) platformListAudit(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	q := r.URL.Query()
	f := db.AuditFilter{
		Category: q.Get("category"),
		Action:   q.Get("action"),
		Result:   q.Get("result"),
		Actor:    q.Get("actor"),
		Limit:    atoiDefault(q.Get("limit"), 100),
		Offset:   atoiDefault(q.Get("offset"), 0),
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	items, total, err := pg.ListAudit(f)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if items == nil {
		items = []db.AuditEntry{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": f.Limit, "offset": f.Offset})
}

// GET /api/audit/stats — aggregate counts for the audit page header.
func (s *Server) platformAuditStats(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	n, err := pg.CountAudit()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"total": n})
}

// POST /api/audit/gc — purge audit rows older than ?days= (default 90).
func (s *Server) platformAuditGC(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	days := atoiDefault(r.URL.Query().Get("days"), 90)
	if days <= 0 {
		days = 90
	}
	n, err := pg.AuditRetentionDays(days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"removed": n})
}

// POST /api/audit/clear — empty the entire audit log.
func (s *Server) platformAuditClear(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	n, err := pg.ClearAudit()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"removed": n})
}
