package server

import (
	"context"
	"testing"
)

// TestResolveParentRef locks the spawn_task parent-link rule: an explicit
// parent_ref wins, otherwise the child is parented to the task running the call,
// so the orchestration tree is connected even when the model omits parent_ref.
func TestResolveParentRef(t *testing.T) {
	base := context.Background()

	if got := resolveParentRef(base, "explicit-1"); got != "explicit-1" {
		t.Fatalf("explicit parent should win, got %q", got)
	}
	if got := resolveParentRef(base, "   "); got != "" {
		t.Fatalf("empty explicit with no caller task should stay empty, got %q", got)
	}
	if got := currentTaskID(base); got != "" {
		t.Fatalf("base ctx should carry no task, got %q", got)
	}

	ctx := withCurrentTask(base, "caller-7")
	if got := currentTaskID(ctx); got != "caller-7" {
		t.Fatalf("withCurrentTask round-trip failed, got %q", got)
	}
	if got := resolveParentRef(ctx, ""); got != "caller-7" {
		t.Fatalf("should default to the caller, got %q", got)
	}
	if got := resolveParentRef(ctx, "explicit-9"); got != "explicit-9" {
		t.Fatalf("explicit should override the caller, got %q", got)
	}
	// withCurrentTask on an empty id is a no-op.
	if got := currentTaskID(withCurrentTask(base, "  ")); got != "" {
		t.Fatalf("blank task id should not be attached, got %q", got)
	}
}
