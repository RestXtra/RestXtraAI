package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureEmitsMachineReadableArtifact(t *testing.T) {
	work := t.TempDir()
	tc := &ToolContext{WorkingDir: work, OutputDir: filepath.Join(work, "cmd-output"), MaxOutputChars: 8}
	original := "first line\nsecond line"
	visible := Capture(tc, original)
	refs := ParsePersistedOutputs(visible)
	if len(refs) != 1 {
		t.Fatalf("want one artifact ref, got %d in %q", len(refs), visible)
	}
	ref := refs[0]
	if filepath.IsAbs(ref.Path) || ref.Bytes != int64(len(original)) || ref.Lines != 2 {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	fullPath := filepath.Join(work, ref.Path)
	b, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != original || !strings.Contains(ref.MIME, "text/plain") {
		t.Fatalf("artifact mismatch: ref=%+v content=%q", ref, b)
	}
}

func TestParsePersistedOutputsIgnoresInvalidEnvelope(t *testing.T) {
	if got := ParsePersistedOutputs("<persisted-output>not-json</persisted-output>"); len(got) != 0 {
		t.Fatalf("want invalid metadata ignored, got %+v", got)
	}
}
