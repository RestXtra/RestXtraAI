package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityPayloadMergesLifecycleAndProjectionData(t *testing.T) {
	raw := activityPayload(Activity{
		Kind: "result", Summary: "done", Detail: "full", IsError: true,
		Payload: json.RawMessage(`{"reason":"completed","turns":2}`),
	})
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["reason"] != "completed" || got["kind"] != "result" || got["detail"] != "full" || got["is_error"] != true {
		t.Fatalf("incomplete merged payload: %s", raw)
	}
}

func TestEventTypeForActivity(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{"tool_use", EventToolCalled},
		{"tool_result", EventToolResult},
		{"usage", EventBudgetChanged},
		{"result", EventTurnFinished},
		{"text", EventAssistantText},
		{"thinking", EventAssistantThinking},
		{"user", EventUserMessage},
	}
	for _, tt := range tests {
		if got := eventTypeForActivity(Activity{Kind: tt.kind}); got != tt.want {
			t.Fatalf("kind %q: want %q, got %q", tt.kind, tt.want, got)
		}
	}
	if got := eventTypeForActivity(Activity{Kind: "text", EventType: EventSummaryCreated}); got != EventSummaryCreated {
		t.Fatalf("explicit event type lost: %q", got)
	}
}

func TestPrepareArtifactsHashesLargeSingleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	content := strings.Repeat("x", 5*1024*1024) + "\ntail"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := prepareArtifacts([]ArtifactCandidate{{Path: path, MIMEType: "text/plain"}})
	if len(got) != 1 {
		t.Fatalf("want one prepared artifact, got %d", len(got))
	}
	if got[0].ByteSize != int64(len(content)) || got[0].LineCount != 2 {
		t.Fatalf("unexpected dimensions: bytes=%d lines=%d", got[0].ByteSize, got[0].LineCount)
	}
	if !strings.HasPrefix(got[0].ContentHash, "sha256:") || len(got[0].ContentHash) != len("sha256:")+64 {
		t.Fatalf("unexpected hash: %q", got[0].ContentHash)
	}
}
