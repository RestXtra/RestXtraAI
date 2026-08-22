package agent

import (
	"strings"
	"testing"

	"github.com/RestXtra/RestXtraAI/db"
)

func TestRenderWorkingSetRecoveryIsBoundedAndStructured(t *testing.T) {
	long := strings.Repeat("x", 500)
	ws := &db.WorkingSet{
		Version: 2, SchemaVersion: 1, ContentHash: "sha256:abc", SourceEventID: 41,
		Fixed:        db.WorkingSetFixedLayer{Objective: "test objective", Constraints: []string{"do not write"}},
		Sliding:      db.WorkingSetSlidingLayer{PendingDependencies: []db.WorkingSetNodeRef{{ID: 9, State: "open", Summary: long}}},
		External:     db.WorkingSetExternalLayer{EventCursor: 41, EventsAPI: "/api/tasks/7/events", ArtifactsAPI: "/api/tasks/7/artifacts"},
		ModelSummary: "SECRET FULL MODEL SUMMARY",
	}
	got := renderWorkingSetRecovery(ws)
	for _, want := range []string{"version=2", "hash=sha256:abc", "node=9", "实时图为准", "/api/tasks/7/events"} {
		if !strings.Contains(got, want) {
			t.Fatalf("recovery missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, ws.ModelSummary) || strings.Contains(got, long) {
		t.Fatalf("recovery leaked unbounded/model summary content: %s", got)
	}
	if len([]rune(got)) > 2000 {
		t.Fatalf("recovery unexpectedly large: %d runes", len([]rune(got)))
	}
}

func TestRenderWorkingSetRecoveryNilIsNoop(t *testing.T) {
	if got := renderWorkingSetRecovery(nil); got != "" {
		t.Fatalf("nil working set rendered %q", got)
	}
}
