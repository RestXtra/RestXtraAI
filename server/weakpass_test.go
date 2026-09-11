package server

import (
	"context"
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
		return false, calls == 3, nil // 第 3 次触发锁定
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

	if ok, blocked, err := verifyHTTPBasic(context.Background(), srv.URL, "admin", "secret"); !ok || blocked || err != nil {
		t.Fatalf("valid creds: ok=%v blocked=%v err=%v", ok, blocked, err)
	}
	if ok, _, _ := verifyHTTPBasic(context.Background(), srv.URL, "admin", "wrong"); ok {
		t.Fatal("wrong creds must not be accepted")
	}
}

func TestVerifyHTTPBasicLockout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ok, blocked, err := verifyHTTPBasic(context.Background(), srv.URL, "admin", "x")
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
	if ok, _, err := verifyHTTPForm(context.Background(), spec, "admin", "secret"); !ok || err != nil {
		t.Fatalf("valid form creds: ok=%v err=%v", ok, err)
	}
	if ok, _, _ := verifyHTTPForm(context.Background(), spec, "admin", "wrong"); ok {
		t.Fatal("wrong form creds must not be accepted")
	}
}

func TestVerifyHTTPFormLockoutBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>登录失败次数过多，请稍后再试</html>"))
	}))
	defer srv.Close()
	spec := weakpassSpec{Kind: "http-form", URL: srv.URL, SuccessMarker: "Welcome"}
	ok, blocked, err := verifyHTTPForm(context.Background(), spec, "admin", "x")
	if ok || !blocked || err != nil {
		t.Fatalf("lockout body should abort: ok=%v blocked=%v err=%v", ok, blocked, err)
	}
}
