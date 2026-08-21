package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkAPIPermissionRouting(b *testing.B) {
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/tasks"},
		{http.MethodPost, "/api/tasks/42/control"},
		{http.MethodGet, "/api/platform/users"},
		{http.MethodPost, "/api/benchmark/submit"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := paths[i%len(paths)]
		if _, ok := apiPermission(p.method, p.path); !ok {
			b.Fatal("route unexpectedly unclassified")
		}
	}
}

func BenchmarkJWTVerification(b *testing.B) {
	key := []byte("0123456789abcdef0123456789abcdef")
	token, err := signUserJWT(key, 42, "benchmark-user", "password-hash")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := verifyUserJWT(token, key); !ok {
			b.Fatal("token verification failed")
		}
	}
}

func BenchmarkTSecHTTPTransport(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("BENCHMARK_TOKEN") != "local-token" {
			b.Error("missing benchmark token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		code, body, err := benchHTTP(context.Background(), "local-token", upstream.URL, http.MethodPost,
			"/openapi/v1/challenges/start?unique_code="+urlEncode("web/app test"), nil)
		if err != nil || code != http.StatusOK || len(body) == 0 {
			b.Fatalf("benchHTTP: code=%d body=%q err=%v", code, body, err)
		}
	}
}
