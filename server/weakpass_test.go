package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWeakpassToolConstructs(t *testing.T) {
	tool := (&Server{}).toolWeakPasswordProbe()
	if tool.Name() != "weak_password_probe" {
		t.Fatalf("name = %q", tool.Name())
	}
	if tool.InputSchema() == nil {
		t.Fatal("schema nil")
	}
}

func TestWeakpassCleanList(t *testing.T) {
	got := weakpassCleanList([]string{" a ", "a", "", "b", "c", "d"}, 3)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("cleanList = %#v", got)
	}
}

func TestWeakpassLoopStopsOnFirstHit(t *testing.T) {
	calls := 0
	verify := func(_ context.Context, u, p string) (bool, bool, error) {
		calls++
		return u == "admin" && p == "admin123", false, nil
	}
	out := weakpassLoop(context.Background(), weakpassSpec{Kind: "http-form"},
		[]string{"admin"}, []string{"x", "admin123", "y"}, 50, 0, verify)
	if len(out.Found) != 1 || out.Found[0].Username != "admin" || out.Found[0].Password != "admin123" {
		t.Fatalf("found = %#v", out.Found)
	}
	if calls != 2 {
		t.Fatalf("expected stop after hit (2 calls), got %d", calls)
	}
	if !out.Aborted || out.Attempts != 2 {
		t.Fatalf("aborted=%v attempts=%d", out.Aborted, out.Attempts)
	}
}

func TestWeakpassLoopLockoutAborts(t *testing.T) {
	calls := 0
	verify := func(_ context.Context, _, _ string) (bool, bool, error) {
		calls++
		return false, calls == 3, nil // third attempt triggers lockout
	}
	out := weakpassLoop(context.Background(), weakpassSpec{Kind: "http-basic"},
		[]string{"admin"}, []string{"a", "b", "c", "d"}, 50, 0, verify)
	if !out.Locked || !out.Aborted {
		t.Fatalf("locked=%v aborted=%v", out.Locked, out.Aborted)
	}
	if calls != 3 {
		t.Fatalf("expected abort at 3rd call, got %d", calls)
	}
}

func TestWeakpassLoopRespectsMaxAttempts(t *testing.T) {
	calls := 0
	verify := func(_ context.Context, _, _ string) (bool, bool, error) { calls++; return false, false, nil }
	passwords := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		passwords = append(passwords, "p")
	}
	out := weakpassLoop(context.Background(), weakpassSpec{Kind: "ssh"},
		[]string{"root"}, passwords, 5, 0, verify)
	if calls != 5 || out.Attempts != 5 {
		t.Fatalf("max not enforced: calls=%d attempts=%d", calls, out.Attempts)
	}
	if !out.Aborted {
		t.Fatal("expected aborted at cap")
	}
}

func TestWeakpassLoopNoHitCompletes(t *testing.T) {
	verify := func(_ context.Context, _, _ string) (bool, bool, error) { return false, false, nil }
	out := weakpassLoop(context.Background(), weakpassSpec{Kind: "rdp"},
		[]string{"admin"}, []string{"a", "b"}, 50, 0, verify)
	if len(out.Found) != 0 || out.Locked || out.Aborted {
		t.Fatalf("outcome = %#v", out)
	}
	if out.Attempts != 2 {
		t.Fatalf("attempts = %d", out.Attempts)
	}
}

func TestWeakpassLoopDelayHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	verify := func(_ context.Context, _, _ string) (bool, bool, error) { return false, false, nil }
	out := weakpassLoop(ctx, weakpassSpec{Kind: "ssh"}, []string{"a"}, []string{"b"}, 50, time.Millisecond, verify)
	if !out.Aborted {
		t.Fatal("expected abort on cancelled ctx")
	}
}

func TestWeakpassVerifierRequiresFormSignal(t *testing.T) {
	s := &Server{}
	if _, err := s.weakpassVerifier(weakpassSpec{Kind: "http-form", URL: "http://x"}); err == nil {
		t.Fatal("expected error when http-form lacks success/fail signal")
	}
	if _, err := s.weakpassVerifier(weakpassSpec{Kind: "http-form", URL: "http://x", SuccessStatus: 302}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if _, err := s.weakpassVerifier(weakpassSpec{Kind: "ssh"}); err == nil {
		t.Fatal("expected error when ssh lacks host")
	}
	if _, err := s.weakpassVerifier(weakpassSpec{Kind: "bogus"}); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestVerifyHTTPBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if ok && u == "admin" && p == "secret" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if ok, blocked, err := verifyHTTPBasic(context.Background(), weakpassHTTPClient(), srv.URL, "admin", "secret"); !ok || blocked || err != nil {
		t.Fatalf("valid creds: ok=%v blocked=%v err=%v", ok, blocked, err)
	}
	if ok, _, _ := verifyHTTPBasic(context.Background(), weakpassHTTPClient(), srv.URL, "admin", "wrong"); ok {
		t.Fatal("wrong creds must not be accepted")
	}
}

func TestVerifyHTTPBasicLockout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ok, blocked, err := verifyHTTPBasic(context.Background(), weakpassHTTPClient(), srv.URL, "admin", "x")
	if ok || !blocked || err != nil {
		t.Fatalf("429 should be blocked: ok=%v blocked=%v err=%v", ok, blocked, err)
	}
}

func TestVerifyHTTPForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostFormValue("username") == "admin" && r.PostFormValue("password") == "secret" {
			_, _ = w.Write([]byte("<html>Dashboard Welcome</html>"))
			return
		}
		_, _ = w.Write([]byte("<html>invalid credentials</html>"))
	}))
	defer srv.Close()

	spec := weakpassSpec{Kind: "http-form", URL: srv.URL, SuccessMarker: "Dashboard"}
	if ok, _, err := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "secret"); !ok || err != nil {
		t.Fatalf("valid form creds: ok=%v err=%v", ok, err)
	}
	if ok, _, _ := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "wrong"); ok {
		t.Fatal("wrong form creds must not be accepted")
	}
}

func TestVerifyHTTPFormLockoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>login failed too many times, try later</html>"))
	}))
	defer srv.Close()
	spec := weakpassSpec{Kind: "http-form", URL: srv.URL, SuccessMarker: "Welcome"}
	ok, blocked, err := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "x")
	if ok || !blocked || err != nil {
		t.Fatalf("lockout body should abort: ok=%v blocked=%v err=%v", ok, blocked, err)
	}
}

func TestExtractCSRF(t *testing.T) {
	html := `<form><input type="hidden" name="csrf_token" value="abc123"><input name="username"></form>`
	if got := extractCSRF(html, "csrf_token"); got != "abc123" {
		t.Fatalf("name-before-value: got %q", got)
	}
	if got := extractCSRF(`<input value="xyz" name="_token">`, "_token"); got != "xyz" {
		t.Fatalf("value-before-name: got %q", got)
	}
	if got := extractCSRF(`<meta name="csrf-token" content="mm">`, "csrf-token"); got != "mm" {
		t.Fatalf("meta: got %q", got)
	}
	if got := extractCSRF(html, ""); got != "" {
		t.Fatalf("empty field: got %q", got)
	}
}

func TestVerifyHTTPFormCSRF(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`<form><input type="hidden" name="csrf_token" value="T0KEN"></form>`))
			return
		}
		_ = r.ParseForm()
		if r.PostFormValue("csrf_token") == "T0KEN" && r.PostFormValue("username") == "admin" && r.PostFormValue("password") == "secret" {
			_, _ = w.Write([]byte("Welcome"))
			return
		}
		_, _ = w.Write([]byte("bad"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	spec := weakpassSpec{Kind: "http-form", URL: srv.URL + "/login", SuccessMarker: "Welcome", CSRFField: "csrf_token"}
	if ok, _, err := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "secret"); !ok || err != nil {
		t.Fatalf("csrf form: ok=%v err=%v", ok, err)
	}
}

func TestVerifyHTTPJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]string
		_ = json.NewDecoder(r.Body).Decode(&m)
		w.Header().Set("Content-Type", "application/json")
		if m["username"] == "admin" && m["password"] == "secret" {
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"msg":"bad"}`))
	}))
	defer srv.Close()
	spec := weakpassSpec{Kind: "http-json", URL: srv.URL, BodyType: "json", SuccessMarker: `"code":0`}
	if ok, _, err := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "secret"); !ok || err != nil {
		t.Fatalf("json login: ok=%v err=%v", ok, err)
	}
	if ok, _, _ := verifyHTTPLogin(context.Background(), weakpassHTTPClient(), spec, "admin", "wrong"); ok {
		t.Fatal("wrong json creds must not be accepted")
	}
}
