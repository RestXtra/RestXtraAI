package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
	"golang.org/x/crypto/bcrypt"
)

// Platform RBAC management API (members / roles / permissions), built into
// RestXtra on the unified JWT + net/http layer.

// resolveUID reads the {id} path segment as a user id.
func pathID(r *http.Request) int64 {
	n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return n
}

// GET /api/platform/my — the caller's resolved identity + permissions (menu gate).
func (s *Server) platformMy(w http.ResponseWriter, r *http.Request) {
	acc := s.currentAccess(r)
	if acc == nil {
		writeErr(w, 401, "未授权")
		return
	}
	perms := make([]string, 0, len(acc.Permissions))
	for k := range acc.Permissions {
		perms = append(perms, k)
	}
	roleNames := make([]string, 0, len(acc.Roles))
	for _, rl := range acc.Roles {
		roleNames = append(roleNames, rl.Name)
	}
	writeJSON(w, 200, map[string]any{
		"user":        acc.User,
		"roles":       roleNames,
		"permissions": perms,
		"scope":       acc.Scope,
		"admin":       acc.Admin,
	})
}

// GET /api/platform/permissions — the full permission catalog.
func (s *Server) platformPermissions(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	list, err := pg.ListPermissions()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []db.Permission{}
	}
	writeJSON(w, 200, map[string]any{"permissions": list})
}

// ------------------------------- users ----------------------------------

// GET /api/platform/users — list members with their role names.
func (s *Server) platformListUsers(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	users, err := pg.ListUsers()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type userDTO struct {
		ID          int64    `json:"id"`
		Username    string   `json:"username"`
		DisplayName string   `json:"display_name"`
		Enabled     bool     `json:"enabled"`
		IsBuiltin   bool     `json:"is_builtin"`
		CreatedAt   string   `json:"created_at"`
		Roles       []string `json:"roles"`
	}
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		roles, _ := pg.ListUserRoles(u.ID)
		names := make([]string, 0, len(roles))
		for _, rl := range roles {
			names = append(names, rl.Name)
		}
		out = append(out, userDTO{
			ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
			Enabled: u.Enabled, IsBuiltin: u.IsBuiltin,
			CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), Roles: names,
		})
	}
	writeJSON(w, 200, map[string]any{"users": out})
}

// POST /api/platform/users — create a member (password required, roles optional).
func (s *Server) platformCreateUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Username    string   `json:"username"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		Roles       []string `json:"roles"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeErr(w, 400, "用户名不能为空")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, 400, "密码至少 8 位")
		return
	}
	existing, err := pg.GetUserByUsername(username)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if existing != nil {
		writeErr(w, 409, "用户名已存在")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "密码加密失败")
		return
	}
	id, err := pg.CreateUser(username, req.DisplayName, string(hash))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, name := range req.Roles {
		if role, err := pg.GetRoleByName(strings.TrimSpace(name)); err == nil && role != nil {
			_ = pg.AssignRoleToUser(id, role.ID)
		}
	}
	s.recordAudit(r, "platform", "create_user", "success", "创建成员 "+username)
	writeJSON(w, 201, map[string]any{"id": id})
}

// PATCH /api/platform/users/{id} — update display_name / enabled.
func (s *Server) platformUpdateUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		DisplayName string `json:"display_name"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	// 禁止禁用/删除内置管理员 RestXtra（最后防线）
	u, err := pg.GetUserByID(id)
	if err != nil || u == nil {
		writeErr(w, 404, "用户不存在")
		return
	}
	if u.IsBuiltin && req.Enabled != nil && !*req.Enabled {
		writeErr(w, 403, "不能禁用内置管理员")
		return
	}
	if err := pg.UpdateUser(id, req.DisplayName, req.Enabled); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "update_user", "success", "更新成员 "+u.Username)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// DELETE /api/platform/users/{id} — delete a member (not yourself, not the last admin).
func (s *Server) platformDeleteUser(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	me, _ := principalOf(r)
	if me.UID == id {
		writeErr(w, 403, "不能删除当前登录的账户")
		return
	}
	u, err := pg.GetUserByID(id)
	if err != nil || u == nil {
		writeErr(w, 404, "用户不存在")
		return
	}
	if u.IsBuiltin {
		writeErr(w, 403, "不能删除内置管理员")
		return
	}
	// refuse to remove the last remaining admin-role holder
	if roles, _ := pg.ListUserRoles(id); hasRole(roles, db.RoleAdmin) {
		admins := s.countRoleHolders(db.RoleAdmin)
		if admins <= 1 {
			writeErr(w, 403, "不能删除最后一个管理员")
			return
		}
	}
	if err := pg.DeleteUser(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "delete_user", "success", "删除成员 "+u.Username)
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// POST /api/platform/users/{id}/password — reset a member's password.
func (s *Server) platformResetPassword(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil || len(req.Password) < 8 {
		writeErr(w, 400, "密码至少 8 位")
		return
	}
	u, err := pg.GetUserByID(id)
	if err != nil || u == nil {
		writeErr(w, 404, "用户不存在")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "密码加密失败")
		return
	}
	if err := pg.SetUserPassword(id, string(hash)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "reset_password", "success", "重置成员密码 "+u.Username)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/platform/users/{id}/roles — set a member's roles (by name).
func (s *Server) platformSetUserRoles(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Roles []string `json:"roles"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	u, err := pg.GetUserByID(id)
	if err != nil || u == nil {
		writeErr(w, 404, "用户不存在")
		return
	}
	// replace role bindings
	cur, _ := pg.ListUserRoles(id)
	for _, rl := range cur {
		_ = pg.RemoveRoleFromUser(id, rl.ID)
	}
	for _, name := range req.Roles {
		if role, err := pg.GetRoleByName(strings.TrimSpace(name)); err == nil && role != nil {
			_ = pg.AssignRoleToUser(id, role.ID)
		}
	}
	s.recordAudit(r, "platform", "set_user_roles", "success", "设置成员角色 "+u.Username)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ------------------------------- roles ----------------------------------

// GET /api/platform/roles — list roles (with permission counts).
func (s *Server) platformListRoles(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	roles, err := pg.ListRoles()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type roleDTO struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		IsSystem    bool   `json:"is_system"`
		PermCount   int    `json:"perm_count"`
	}
	out := make([]roleDTO, 0, len(roles))
	for _, rl := range roles {
		keys, _ := pg.RolePermissionKeys(rl.ID)
		out = append(out, roleDTO{
			ID: rl.ID, Name: rl.Name, Description: rl.Description,
			Scope: rl.Scope, IsSystem: rl.IsSystem, PermCount: len(keys),
		})
	}
	writeJSON(w, 200, map[string]any{"roles": out})
}

// POST /api/platform/roles — create a role.
func (s *Server) platformCreateRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		Permissions []string `json:"permissions"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, 400, "角色名不能为空")
		return
	}
	if req.Scope == "" {
		req.Scope = "own"
	}
	if existing, _ := pg.GetRoleByName(name); existing != nil {
		writeErr(w, 409, "角色名已存在")
		return
	}
	id, err := pg.CreateRole(name, req.Description, req.Scope)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(req.Permissions) > 0 {
		if err := pg.SetRolePermissions(id, req.Permissions); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	s.recordAudit(r, "platform", "create_role", "success", "创建角色 "+name)
	writeJSON(w, 201, map[string]any{"id": id})
}

// PATCH /api/platform/roles/{id} — update description / scope.
func (s *Server) platformUpdateRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Description string  `json:"description"`
		Scope       *string `json:"scope"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	rl, err := pg.GetRole(id)
	if err != nil || rl == nil {
		writeErr(w, 404, "角色不存在")
		return
	}
	if rl.IsSystem && req.Scope != nil && *req.Scope != "all" {
		writeErr(w, 403, "系统角色必须保持 all 范围")
		return
	}
	if err := pg.UpdateRole(id, req.Description, req.Scope); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "update_role", "success", "更新角色 "+rl.Name)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// DELETE /api/platform/roles/{id} — delete a role (system roles are protected).
func (s *Server) platformDeleteRole(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	rl, err := pg.GetRole(id)
	if err != nil || rl == nil {
		writeErr(w, 404, "角色不存在")
		return
	}
	if rl.IsSystem {
		writeErr(w, 403, "不能删除系统角色")
		return
	}
	if err := pg.DeleteRole(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "delete_role", "success", "删除角色 "+rl.Name)
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// GET /api/platform/roles/{id}/permissions — a role's permission keys.
func (s *Server) platformGetRolePermissions(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	keys, err := pg.RolePermissionKeys(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, 200, map[string]any{"keys": keys})
}

// PUT /api/platform/roles/{id}/permissions — set a role's permission keys.
func (s *Server) platformSetRolePermissions(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	rl, err := pg.GetRole(id)
	if err != nil || rl == nil {
		writeErr(w, 404, "角色不存在")
		return
	}
	if err := pg.SetRolePermissions(id, req.Keys); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "platform", "set_role_permissions", "success", "设置角色权限 "+rl.Name)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ------------------------------- helpers --------------------------------

func hasRole(roles []db.Role, name string) bool {
	for _, rl := range roles {
		if rl.Name == name {
			return true
		}
	}
	return false
}

// countRoleHolders returns how many users hold the named role.
func (s *Server) countRoleHolders(roleName string) int {
	if s.m.pg == nil {
		return 0
	}
	users, err := s.m.pg.ListUsers()
	if err != nil {
		return 0
	}
	n := 0
	for _, u := range users {
		roles, err := s.m.pg.ListUserRoles(u.ID)
		if err == nil && hasRole(roles, roleName) {
			n++
		}
	}
	return n
}
