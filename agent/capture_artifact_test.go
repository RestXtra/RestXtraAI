package agent

import (
	"path/filepath"
	"testing"
)

func TestResolvePersistedArtifactConfinedToOutputDir(t *testing.T) {
	work := t.TempDir()
	want := filepath.Join(work, "cmd-output", "output-1.txt")
	got, ok := resolvePersistedArtifact(work, filepath.Join("cmd-output", "output-1.txt"))
	if !ok || got != want {
		t.Fatalf("want %q accepted, got %q ok=%v", want, got, ok)
	}
	if got, ok := resolvePersistedArtifact(work, filepath.Join("..", "secret.txt")); ok {
		t.Fatalf("outside path accepted: %q", got)
	}
	if got, ok := resolvePersistedArtifact(work, filepath.Join(work, "other", "secret.txt")); ok {
		t.Fatalf("sibling path accepted: %q", got)
	}
}
