package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	jwtKeyFilename = "jwt.key"
	authPassKey    = "auth.password_hash"
	jwtTTL         = 7 * 24 * time.Hour
	keyChars       = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	adminUsername  = "ARTEX" // 默认首个管理员用户名（保持与旧前端兼容）
)

// loadOrCreateJWTKey reads the 32-byte signing key from dataDir/jwt.key.
// On first run it generates a cryptographically random key and persists it.
func loadOrCreateJWTKey(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, jwtKeyFilename)
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 32 {
		return []byte(strings.TrimSpace(string(data))), nil
	}
	buf := make([]byte, 32)
	for i := range buf {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(keyChars))))
		if err != nil {
			return nil, fmt.Errorf("generate jwt key: %w", err)
		}
		buf[i] = keyChars[n.Int64()]
	}
	if err := os.WriteFile(path, buf, 0600); err != nil {
		return nil, fmt.Errorf("write jwt key: %w", err)
	}
	log.Printf("[auth] 新 JWT key 已写入 %s", path)
	return buf, nil
}

// tokenClaims carries the platform user identity inside the JWT.
type tokenClaims struct {
	UID int64 `json:"uid"`
	jwt.RegisteredClaims
}

// signUserJWT issues a 7-day HS256 token for a platform user.
func signUserJWT(key []byte, uid int64, username string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}).SignedString(key)
}

// verifyUserJWT returns the parsed claims when the token is valid + non-expired.
func verifyUserJWT(tokenStr string, key []byte) (*tokenClaims, bool) {
	claims := &tokenClaims{}
	t, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return key, nil
	})
	return claims, err == nil && t.Valid
}

// extractToken reads the JWT from Authorization: Bearer header,
// artex_token cookie, or ?token= query param (for SSE connections).
func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie("artex_token"); err == nil && c.Value != "" {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

type principalKey struct{}

// principal is the authenticated platform user carried in the request context.
type principal struct {
	UID      int64
	Username string
}

// withPrincipal stores the authenticated identity into the request context.
func withPrincipal(r *http.Request, uid int64, username string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{UID: uid, Username: username}))
}

// principalOf reads the authenticated identity set by requireAuth.
func principalOf(r *http.Request) (principal, bool) {
	p, ok := r.Context().Value(principalKey{}).(principal)
	return p, ok
}

// requireAuth wraps h with JWT validation.
// /api/auth/* and /api/health are exempt.
func (s *Server) requireAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/auth/") || p == "/api/health" {
			h.ServeHTTP(w, r)
			return
		}
		tok := extractToken(r)
		if tok == "" {
			writeErr(w, 401, "未授权")
			return
		}
		claims, ok := verifyUserJWT(tok, s.jwtKey)
		if !ok {
			writeErr(w, 401, "token 无效或已过期")
			return
		}
		r = withPrincipal(r, claims.UID, claims.Subject)
		h.ServeHTTP(w, r)
	})
}

// authInitialized reports whether a platform admin password exists (users table
// or the legacy settings hash).
func (s *Server) authInitialized() bool {
	if s.m.pg == nil {
		return false
	}
	if n, err := s.m.pg.PlatformUserCount(); err == nil && n > 0 {
		return true
	}
	hash, _, _ := s.m.pg.GetSetting(authPassKey)
	return hash != ""
}

// clientIP extracts the peer address for audit records.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// recordAudit writes one platform audit row (best-effort) using the request's
// authenticated principal as actor.
func (s *Server) recordAudit(r *http.Request, category, action, result, message string) {
	actor := ""
	if p, ok := principalOf(r); ok {
		actor = p.Username
	}
	s.recordAuditAs(r, actor, category, action, result, message)
}

// recordAuditAs writes one platform audit row with an explicit actor (used by
// auth endpoints that run before a principal is attached to the request).
func (s *Server) recordAuditAs(r *http.Request, actor, category, action, result, message string) {
	if s.m.pg == nil {
		return
	}
	ip := ""
	if r != nil {
		ip = clientIP(r)
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{
		Actor: actor, Category: category, Action: action,
		Result: result, Message: message, IP: ip,
	})
}

// GET /api/auth/status — reports whether the admin password has been initialised.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"initialized": s.authInitialized()})
}

// POST /api/auth/init — creates the first platform admin; rejected if one exists.
func (s *Server) authInit(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	if s.authInitialized() {
		writeErr(w, 403, "已初始化")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil || req.Password == "" {
		writeErr(w, 400, "密码不能为空")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "密码加密失败")
		return
	}
	userID, err := pg.CreateUser(adminUsername, "管理员", string(hash))
	if err != nil {
		writeErr(w, 500, "创建管理员失败: "+err.Error())
		return
	}
	// bind the built-in admin role (idempotent)
	if role, err := pg.GetRoleByName(db.RoleAdmin); err == nil && role != nil {
		_ = pg.AssignRoleToUser(userID, role.ID)
	}
	// keep the legacy settings hash in sync (migration / compatibility)
	_ = pg.SetSetting(authPassKey, string(hash))
	tok, err := signUserJWT(s.jwtKey, userID, adminUsername)
	if err != nil {
		writeErr(w, 500, "token 生成失败")
		return
	}
	log.Printf("[auth] 首次初始化管理员账户 %s (uid=%d)", adminUsername, userID)
	s.recordAuditAs(r, adminUsername, "auth", "init", "success", "初始化平台管理员")
	writeJSON(w, 200, map[string]any{"token": tok})
}

// POST /api/auth/change-password — changes the caller's password. Requires a
// valid token (this route is under /api/auth/* which requireAuth exempts, so the
// token is validated here) AND the current password.
func (s *Server) authChangePassword(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	claims, ok := verifyUserJWT(extractToken(r), s.jwtKey)
	if !ok {
		writeErr(w, 401, "未授权")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if req.NewPassword == "" {
		writeErr(w, 400, "新密码不能为空")
		return
	}
	user, err := pg.GetUserByID(claims.UID)
	if err != nil || user == nil {
		writeErr(w, 401, "用户不存在")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)); err != nil {
		s.recordAuditAs(r, user.Username, "auth", "change_password", "failure", "当前密码错误")
		writeErr(w, 401, "当前密码错误")
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, 500, "密码加密失败")
		return
	}
	if err := pg.SetUserPassword(user.ID, string(newHash)); err != nil {
		writeErr(w, 500, "保存失败: "+err.Error())
		return
	}
	_ = pg.SetSetting(authPassKey, string(newHash)) // keep legacy hash in sync
	s.recordAuditAs(r, user.Username, "auth", "change_password", "success", "修改密码")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/auth/login — validates username/password against the platform users
// table and returns a JWT. Legacy fallback: a pre-existing settings hash with
// username "ARTEX" (from before multi-user) is adopted into the users table on
// first successful login.
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = adminUsername
	}
	user, err := pg.GetUserByUsername(username)
	if err != nil {
		writeErr(w, 500, "查询用户失败")
		return
	}
	if user == nil && username == adminUsername {
		// migration: adopt the legacy settings hash into a fresh admin user
		legacyHash, _, _ := pg.GetSetting(authPassKey)
		if legacyHash != "" && bcrypt.CompareHashAndPassword([]byte(legacyHash), []byte(req.Password)) == nil {
			id, err := pg.CreateUser(adminUsername, "管理员", legacyHash)
			if err != nil {
				writeErr(w, 500, "迁移管理员失败: "+err.Error())
				return
			}
			if role, err := pg.GetRoleByName(db.RoleAdmin); err == nil && role != nil {
				_ = pg.AssignRoleToUser(id, role.ID)
			}
			user, _ = pg.GetUserByID(id)
		}
	}
	if user == nil || !user.Enabled {
		s.recordAuditAs(r, username, "auth", "login", "failure", "登录失败：用户不存在或已禁用")
		writeErr(w, 401, "用户名或密码错误")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		s.recordAuditAs(r, username, "auth", "login", "failure", "登录失败：密码错误")
		writeErr(w, 401, "用户名或密码错误")
		return
	}
	tok, err := signUserJWT(s.jwtKey, user.ID, user.Username)
	if err != nil {
		writeErr(w, 500, "token 生成失败")
		return
	}
	s.recordAuditAs(r, user.Username, "auth", "login", "success", "登录成功")
	writeJSON(w, 200, map[string]any{"token": tok})
}
