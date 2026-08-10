package server

import (
	"net/http"

	"github.com/RestXtra/RestXtraAI/db"
)

// RBAC middleware: resolves the authenticated principal's access profile and
// requires the named permission (admin bypasses the catalog). Wrap handlers that
// sit behind requireAuth. Returns 403 without invoking h on denial.
func (s *Server) rbac(perm string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.can(r, perm) {
			actor := ""
			if p, ok := principalOf(r); ok {
				actor = p.Username
			}
			_ = s.m.pg.RecordAudit(db.AuditEntry{
				Actor: actor, Category: "rbac", Action: "denied",
				Result: "denied", Message: "权限不足：" + perm, IP: clientIP(r),
			})
			writeErr(w, 403, "无权限")
			return
		}
		h(w, r)
	}
}

// can resolves the current request principal's access and checks a permission.
func (s *Server) can(r *http.Request, perm string) bool {
	acc := s.currentAccess(r)
	if acc == nil {
		return false
	}
	if acc.Admin {
		return true
	}
	return acc.Permissions[perm]
}

// currentAccess resolves the authenticated principal's RBAC profile.
func (s *Server) currentAccess(r *http.Request) *db.ResolvedAccess {
	p, ok := principalOf(r)
	if !ok || s.m.pg == nil {
		return nil
	}
	acc, err := s.m.pg.ResolveAccess(p.UID)
	if err != nil || acc == nil {
		return nil
	}
	return acc
}
