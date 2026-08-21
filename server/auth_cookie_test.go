package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractTokenNeverReadsQueryString(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/logs/stream?token=leaked", nil)
	if got := extractToken(r); got != "" {
		t.Fatalf("query token must be ignored, got %q", got)
	}
	r.AddCookie(&http.Cookie{Name: authCookieName, Value: "cookie-token"})
	if got := extractToken(r); got != "cookie-token" {
		t.Fatalf("cookie token = %q", got)
	}
	r.Header.Set("Authorization", "Bearer bearer-token")
	if got := extractToken(r); got != "bearer-token" {
		t.Fatalf("bearer token should take precedence, got %q", got)
	}
}

func TestAuthCookieSecurityAttributes(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "https://example.test/api/auth/login", nil)
	w := httptest.NewRecorder()
	setAuthCookie(w, r, "signed")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Fatalf("insecure auth cookie: %#v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("auth response must not be cached")
	}
}

func TestCookieRequestOriginPolicy(t *testing.T) {
	tests := []struct {
		origin string
		host   string
		want   bool
	}{
		{"https://app.example.test", "app.example.test", true},
		{"https://evil.example", "app.example.test", false},
		{"not a url", "app.example.test", false},
		{"", "app.example.test", true},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodPost, "https://"+tt.host+"/api/tasks", nil)
		r.Header.Set("Origin", tt.origin)
		if got := cookieRequestOriginAllowed(r); got != tt.want {
			t.Errorf("origin %q: got %v want %v", tt.origin, got, tt.want)
		}
	}
}
