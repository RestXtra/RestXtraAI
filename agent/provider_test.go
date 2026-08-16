package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

// TestBearerHTTPClient rewrites the Anthropic credential header: x-api-key is
// dropped and Authorization: Bearer is set (ANTHROPIC_AUTH_TOKEN-style relays).
func TestBearerHTTPClient(t *testing.T) {
	var gotXKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"type":"message_start"}`))
	}))
	defer srv.Close()

	cfg := Config{
		Format:   llm.FormatAnthropic,
		BaseURL:  srv.URL,
		APIKey:   "sk-test",
		Model:    "deepseek-v4-flash",
		AuthMode: AuthModeBearer,
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range prov.Stream(ctx, llm.CompletionRequest{MaxTokens: 8}) {
		// drain the stream so the HTTP round trip actually happens
	}
	if gotXKey != "" {
		t.Errorf("x-api-key should be removed for bearer auth, got %q", gotXKey)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization should be 'Bearer sk-test', got %q", gotAuth)
	}
}

// TestDefaultAuthKeepsXAPIKey ensures the default (x-api-key) path is unchanged.
func TestDefaultAuthKeepsXAPIKey(t *testing.T) {
	var gotXKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"type":"message_start"}`))
	}))
	defer srv.Close()

	cfg := Config{Format: llm.FormatAnthropic, BaseURL: srv.URL, APIKey: "sk-test", Model: "m"}
	prov, err := cfg.NewProvider()
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range prov.Stream(ctx, llm.CompletionRequest{MaxTokens: 8}) {
	}
	if gotXKey != "sk-test" {
		t.Errorf("x-api-key should stay 'sk-test' by default, got %q", gotXKey)
	}
	if gotAuth != "" {
		t.Errorf("Authorization should be empty by default, got %q", gotAuth)
	}
}
