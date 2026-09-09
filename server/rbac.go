package server

import (
	"net/http"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

const authenticatedOnly = "-"

// authorizeAPI is the central authorization boundary for every API route. A
// route must be classified by apiPermission; unclassified routes fail closed.
// Handler-local rbac wrappers remain in place as defense in depth.
func (s *Server) authorizeAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perm, ok := apiPermission(r.Method, r.URL.Path)
		if !ok {
			writeErr(w, http.StatusForbidden, "该接口未配置访问权限")
			return
		}
		if perm == authenticatedOnly {
			next.ServeHTTP(w, r)
			return
		}
		if !s.can(r, perm) {
			actor := ""
			if p, exists := principalOf(r); exists {
				actor = p.Username
			}
			_ = s.m.pg.RecordAudit(db.AuditEntry{
				Actor: actor, Category: "rbac", Action: "denied", Result: "denied",
				Message: "权限不足：" + perm, IP: clientIP(r),
			})
			writeErr(w, http.StatusForbidden, "无权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readWrite(method, readPerm, writePerm string) (string, bool) {
	if method == http.MethodGet || method == http.MethodHead {
		return readPerm, true
	}
	return writePerm, true
}

// apiPermission maps the concrete request path to a capability. Keep the most
// specific prefixes before their broader parents.
func apiPermission(method, path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "/api/auth/"), path == "/api/health":
		return authenticatedOnly, true
	case path == "/api/platform/my":
		return authenticatedOnly, true
	case strings.HasPrefix(path, "/api/platform/users"):
		if method == http.MethodGet {
			return "platform.user.read", true
		}
		if strings.HasSuffix(path, "/roles") {
			return "platform.user.role", true
		}
		return "platform.user.write", true
	case strings.HasPrefix(path, "/api/platform/roles"), path == "/api/platform/permissions":
		return readWrite(method, "platform.role.read", "platform.role.write")
	case strings.HasPrefix(path, "/api/audit/logs"), strings.HasPrefix(path, "/api/audit/stats"):
		return "sec.audit.read", true
	case strings.HasPrefix(path, "/api/audit/"):
		return "sec.audit.export", true
	case path == "/api/audit":
		return "task.read", true
	case strings.HasPrefix(path, "/api/intercept/pending"):
		if method == http.MethodPost && strings.HasSuffix(path, "/decide") {
			return "sec.intercept.decide", true
		}
		return "sec.intercept.read", true
	case strings.HasPrefix(path, "/api/intercept/history"), strings.HasPrefix(path, "/api/intercept/task/"):
		return "sec.intercept.read", true
	case strings.HasPrefix(path, "/api/intercept/"):
		return readWrite(method, "sec.intercept.read", "sec.intercept.write")
	case strings.HasPrefix(path, "/api/llm/records"), strings.HasPrefix(path, "/api/logs"),
		strings.HasPrefix(path, "/api/traffic"), strings.HasPrefix(path, "/api/commands"),
		strings.HasPrefix(path, "/api/tokens/"):
		return readWrite(method, "worklog.read", "worklog.write")
	case path == "/api/llm" || strings.HasPrefix(path, "/api/llm/") || strings.HasPrefix(path, "/api/settings"):
		return readWrite(method, "platform.settings.read", "platform.settings.write")
	case strings.HasPrefix(path, "/api/tasks"):
		if path == "/api/tasks" && method == http.MethodPost {
			return "task.create", true
		}
		if method == http.MethodGet || method == http.MethodHead {
			return "task.read", true
		}
		if method == http.MethodDelete || strings.HasSuffix(path, "/chat/stop") {
			return "task.kill", true
		}
		return "task.run", true
	case strings.HasPrefix(path, "/api/conversations"), path == "/api/chat":
		return readWrite(method, "task.read", "task.run")
	case path == "/api/active":
		return "task.read", true
	case strings.HasPrefix(path, "/api/assets"), strings.HasPrefix(path, "/api/companies"):
		return readWrite(method, "task.read", "task.run")
	case strings.HasPrefix(path, "/api/exploration/"), path == "/api/stats",
		strings.HasPrefix(path, "/api/dashboard/"):
		return "task.read", true
	case path == "/api/gc":
		return "task.run", true
	case path == "/api/report":
		return "cap.report.read", true
	case path == "/api/metrics":
		return "platform.settings.read", true
	case strings.HasPrefix(path, "/api/agents"), strings.HasPrefix(path, "/api/triggers"),
		strings.HasPrefix(path, "/api/tools"), strings.HasPrefix(path, "/api/mcp"),
		strings.HasPrefix(path, "/api/skills"), strings.HasPrefix(path, "/api/visibility"),
		strings.HasPrefix(path, "/api/sync/scopesentry"):
		return readWrite(method, "agent.read", "agent.write")
	case strings.HasPrefix(path, "/api/playbook"):
		return readWrite(method, "playbook.read", "playbook.write")
	case strings.HasPrefix(path, "/api/batch"):
		return readWrite(method, "batch.read", "batch.write")
	case strings.HasPrefix(path, "/api/sandbox"):
		return readWrite(method, "sandbox.read", "sandbox.write")
	case strings.HasPrefix(path, "/api/workflow-runs"), strings.HasPrefix(path, "/api/workflows"):
		return readWrite(method, "workflow.read", "workflow.write")
	case strings.HasPrefix(path, "/api/workspace"):
		return readWrite(method, "workspace.read", "workspace.write")
	case strings.HasPrefix(path, "/api/knowledge"):
		return readWrite(method, "knowledge.read", "knowledge.write")
	case strings.HasPrefix(path, "/api/webshell"):
		return readWrite(method, "cap.webshell.read", "cap.webshell.write")
	case strings.HasPrefix(path, "/api/connections"):
		return readWrite(method, "cap.connection.read", "cap.connection.write")
	case strings.HasPrefix(path, "/api/incidents"):
		if strings.HasSuffix(path, "/respond") || strings.HasSuffix(path, "/webhook") {
			return "task.run", true
		}
		if method == http.MethodGet {
			return "task.read", true
		}
		return "task.create", true
	case strings.HasPrefix(path, "/api/c2"):
		return readWrite(method, "cap.c2.read", "cap.c2.write")
	case strings.HasPrefix(path, "/api/proxies"), strings.HasPrefix(path, "/api/proxy-sources"):
		return readWrite(method, "cap.proxy.read", "cap.proxy.write")
	case strings.HasPrefix(path, "/api/spacesearch"):
		if method == http.MethodGet || strings.HasSuffix(path, "/search") || strings.HasSuffix(path, "/test") {
			return "cap.spacesearch.read", true
		}
		return "cap.spacesearch.write", true
	case strings.HasPrefix(path, "/api/benchmark"):
		if method == http.MethodGet {
			return "benchmark.read", true
		}
		return "benchmark.run", true
	default:
		return "", false
	}
}

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
