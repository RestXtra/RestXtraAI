package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIPermissionFailsClosed(t *testing.T) {
	if _, ok := apiPermission(http.MethodGet, "/api/unknown"); ok {
		t.Fatal("unknown API route must fail closed")
	}
}

func TestAPIPermissionSensitiveRoutes(t *testing.T) {
	tests := []struct{ method, path, want string }{
		{http.MethodPost, "/api/tools/custom/test", "agent.write"},
		{http.MethodPut, "/api/skills/demo/files/run.py", "agent.write"},
		{http.MethodDelete, "/api/workspace/delete", "workspace.write"},
		{http.MethodGet, "/api/workspace/list", "workspace.read"},
		{http.MethodGet, "/api/platform/users", "platform.user.read"},
		{http.MethodPost, "/api/platform/users/7/roles", "platform.user.role"},
		{http.MethodGet, "/api/benchmark/challenges", "benchmark.read"},
	}
	for _, tt := range tests {
		got, ok := apiPermission(tt.method, tt.path)
		if !ok || got != tt.want {
			t.Errorf("apiPermission(%s, %s) = %q, %v; want %q, true", tt.method, tt.path, got, ok, tt.want)
		}
	}
}

func TestCORSOriginPolicy(t *testing.T) {
	if corsOriginAllowed("https://evil.example") {
		t.Fatal("unconfigured origins must not be allowed")
	}
}

func TestExtractTokenRejectsQueryTokens(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/tasks?token=secret", nil)
	if got := extractToken(r); got != "" {
		t.Fatalf("query token accepted on non-SSE route: %q", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/logs/stream?token=secret", nil)
	if got := extractToken(r); got != "" {
		t.Fatalf("query token accepted on SSE route: %q", got)
	}
}

func TestClientIPDoesNotTrustForwardedHeaderByDefault(t *testing.T) {
	t.Setenv("RESTXTRA_TRUST_PROXY", "")
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.4")
	if got := clientIP(r); got != "192.0.2.10" {
		t.Fatalf("clientIP = %q, want peer address", got)
	}
	t.Setenv("RESTXTRA_TRUST_PROXY", "true")
	if got := clientIP(r); got != "198.51.100.4" {
		t.Fatalf("trusted clientIP = %q, want forwarded address", got)
	}
}

func TestAuthAndAuthorizationMiddlewareOrder(t *testing.T) {
	called := false
	h := (&Server{}).requireAuth((&Server{}).authorizeAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})))
	// No token must be rejected by authentication before the authorization
	// layer evaluates a missing principal.
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("middleware order: status=%d called=%v", rec.Code, called)
	}
}
