package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Platform RBAC (users / roles / permissions), ported from Pentest-RestXtra
// and adapted to PostgreSQL + the ARTEX *DB store. A user resolves to a set of
// permissions through role_permissions × user_roles; the built-in "admin" role
// bypasses the catalog (everything is allowed).

const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleAuditor  = "auditor"
	RoleViewer   = "viewer"
)

// User is a platform account.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	PasswordHash string    `json:"-"`
	Enabled      bool      `json:"enabled"`
	IsBuiltin    bool      `json:"is_builtin"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Roles        []Role    `json:"roles,omitempty"`
}

// Role groups permissions and a resource visibility scope.
type Role struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Scope       string    `json:"scope"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Permission is one named capability point (e.g. "platform.user.write").
type Permission struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// ResolvedAccess is the authorization profile computed for one user.
type ResolvedAccess struct {
	User        User              `json:"user"`
	Roles       []Role            `json:"roles"`
	Permissions map[string]bool   `json:"permissions"`
	Scope       string            `json:"scope"`
	RoleIDs     map[int64]bool    `json:"-"`
	Admin       bool              `json:"admin"` // 拥有内置 admin 角色（或系统内置用户）→ 全量权限
}

// ------------------------------- users ---------------------------------

// PlatformUserCount returns the number of users in the platform.
func (d *DB) PlatformUserCount() (int, error) {
	var n int
	err := d.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a platform user and returns its id. username is unique.
func (d *DB) CreateUser(username, displayName, passwordHash string) (int64, error) {
	var id int64
	err := d.QueryRow(`
INSERT INTO users(username, display_name, password_hash)
VALUES ($1, $2, $3)
RETURNING id`, username, displayName, passwordHash).Scan(&id)
	return id, err
}

// GetUserByID returns one user (without roles).
func (d *DB) GetUserByID(id int64) (*User, error) {
	return d.scanUser(`SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at FROM users WHERE id=$1`, id)
}

// GetUserByUsername returns one user (without roles).
func (d *DB) GetUserByUsername(username string) (*User, error) {
	return d.scanUser(`SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at FROM users WHERE username=$1`, username)
}

func (d *DB) scanUser(q string, arg any) (*User, error) {
	u := &User{}
	err := d.QueryRow(q, arg).Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Enabled, &u.IsBuiltin, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all users ordered by id.
func (d *DB) ListUsers() ([]*User, error) {
	rows, err := d.Query(`SELECT id, username, display_name, password_hash, enabled, is_builtin, created_at, updated_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Enabled, &u.IsBuiltin, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateUser updates display_name and/or enabled (nil fields are unchanged).
func (d *DB) UpdateUser(id int64, displayName string, enabled *bool) error {
	cur, err := d.GetUserByID(id)
	if err != nil || cur == nil {
		if err == nil {
			err = fmt.Errorf("user %d not found", id)
		}
		return err
	}
	if displayName == "" {
		displayName = cur.DisplayName
	}
	enabledVal := cur.Enabled
	if enabled != nil {
		enabledVal = *enabled
	}
	_, err = d.Exec(`UPDATE users SET display_name=$1, enabled=$2 WHERE id=$3`, displayName, enabledVal, id)
	return err
}

// DeleteUser removes a user (role bindings cascade).
func (d *DB) DeleteUser(id int64) error {
	_, err := d.Exec(`DELETE FROM users WHERE id=$1`, id)
	return err
}

// SetUserPassword resets a user's bcrypt hash.
func (d *DB) SetUserPassword(id int64, hash string) error {
	_, err := d.Exec(`UPDATE users SET password_hash=$1 WHERE id=$2`, hash, id)
	return err
}

// ------------------------------- roles ----------------------------------

// CreateRole inserts a role and returns its id.
func (d *DB) CreateRole(name, description, scope string) (int64, error) {
	var id int64
	err := d.QueryRow(`
INSERT INTO roles(name, description, scope)
VALUES ($1, $2, $3)
RETURNING id`, name, description, scope).Scan(&id)
	return id, err
}

// GetRole returns one role.
func (d *DB) GetRole(id int64) (*Role, error) {
	r := &Role{}
	err := d.QueryRow(`SELECT id, name, description, scope, is_system, created_at, updated_at FROM roles WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetRoleByName returns one role by its (unique) name.
func (d *DB) GetRoleByName(name string) (*Role, error) {
	r := &Role{}
	err := d.QueryRow(`SELECT id, name, description, scope, is_system, created_at, updated_at FROM roles WHERE name=$1`, name).
		Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ListRoles returns all roles ordered by id.
func (d *DB) ListRoles() ([]*Role, error) {
	rows, err := d.Query(`SELECT id, name, description, scope, is_system, created_at, updated_at FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Role
	for rows.Next() {
		r := &Role{}
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRole updates description and scope (nil scope keeps current).
func (d *DB) UpdateRole(id int64, description string, scope *string) error {
	r, err := d.GetRole(id)
	if err != nil || r == nil {
		if err == nil {
			err = fmt.Errorf("role %d not found", id)
		}
		return err
	}
	if scope == nil {
		s := r.Scope
		scope = &s
	}
	_, err = d.Exec(`UPDATE roles SET description=$1, scope=$2 WHERE id=$3`, description, *scope, id)
	return err
}

// DeleteRole removes a role (permission/user bindings cascade).
func (d *DB) DeleteRole(id int64) error {
	_, err := d.Exec(`DELETE FROM roles WHERE id=$1`, id)
	return err
}

// --------------------------- role ↔ user --------------------------------

// AssignRoleToUser binds a role to a user (idempotent).
func (d *DB) AssignRoleToUser(userID, roleID int64) error {
	_, err := d.Exec(`INSERT INTO user_roles(user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, roleID)
	return err
}

// RemoveRoleFromUser unbinds a role from a user.
func (d *DB) RemoveRoleFromUser(userID, roleID int64) error {
	_, err := d.Exec(`DELETE FROM user_roles WHERE user_id=$1 AND role_id=$2`, userID, roleID)
	return err
}

// ListUserRoles returns the roles bound to a user.
func (d *DB) ListUserRoles(userID int64) ([]Role, error) {
	rows, err := d.Query(`
SELECT r.id, r.name, r.description, r.scope, r.is_system, r.created_at, r.updated_at
FROM roles r JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id=$1 ORDER BY r.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &r.IsSystem, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --------------------------- role ↔ permission --------------------------

// RolePermissionKeys returns the permission keys bound to a role.
func (d *DB) RolePermissionKeys(roleID int64) ([]string, error) {
	rows, err := d.Query(`SELECT permission_key FROM role_permissions WHERE role_id=$1 ORDER BY permission_key`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// SetRolePermissions replaces a role's permission set (transactional).
func (d *DB) SetRolePermissions(roleID int64, keys []string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_id=$1`, roleID); err != nil {
		return err
	}
	for _, k := range keys {
		if _, err := tx.Exec(`INSERT INTO role_permissions(role_id, permission_key) VALUES ($1, $2) ON CONFLICT DO NOTHING`, roleID, k); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListPermissions returns the whole permission catalog.
func (d *DB) ListPermissions() ([]Permission, error) {
	rows, err := d.Query(`SELECT key, description FROM permissions ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.Key, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EnsurePermission inserts a permission point if absent (idempotent).
func (d *DB) EnsurePermission(key, description string) error {
	_, err := d.Exec(`INSERT INTO permissions(key, description) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`, key, description)
	return err
}

// ------------------------------ resolution -------------------------------

// ResolveAccess computes the full authorization profile for a user: roles,
// resolved permission set, and whether the user is an admin. Returns nil when
// the user does not exist or is disabled.
func (d *DB) ResolveAccess(userID int64) (*ResolvedAccess, error) {
	u, err := d.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.Enabled {
		return nil, nil
	}
	roles, err := d.ListUserRoles(userID)
	if err != nil {
		return nil, err
	}
	acc := &ResolvedAccess{
		User:        *u,
		Roles:       roles,
		Permissions: map[string]bool{},
		Scope:       "own",
		RoleIDs:     map[int64]bool{},
	}
	for _, r := range roles {
		acc.RoleIDs[r.ID] = true
		if r.Name == RoleAdmin {
			acc.Admin = true
			acc.Scope = "all"
		}
		// scope: most permissive wins
		if (r.Scope == "all" && acc.Scope != "all") || (r.Scope == "assigned" && acc.Scope == "own") {
			acc.Scope = r.Scope
		}
	}
	if acc.Admin {
		return acc, nil // admin bypasses the catalog
	}
	// gather distinct permission keys across all roles
	seen := map[string]bool{}
	rows, err := d.Query(`
SELECT DISTINCT rp.permission_key
FROM role_permissions rp JOIN user_roles ur ON ur.role_id = rp.role_id
WHERE ur.user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		if !seen[k] {
			seen[k] = true
			acc.Permissions[k] = true
		}
	}
	return acc, rows.Err()
}

// ResolveAccessByUsername resolves access for a username (used at login).
func (d *DB) ResolveAccessByUsername(username string) (*ResolvedAccess, error) {
	u, err := d.GetUserByUsername(username)
	if err != nil || u == nil {
		return nil, err
	}
	return d.ResolveAccess(u.ID)
}
