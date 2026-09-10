package db

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUTF8CleanSurrogates verifies utf8Clean replaces lone surrogates (which
// Go json.Marshal would emit as \ud800 — Postgres jsonb rejects those with
// "unsupported Unicode escape sequence", SQLSTATE 22P05).
func TestUTF8CleanSurrogates(t *testing.T) {
	in := "ok" + string(rune(0xD800)) + "bad" + string(rune(0xDFFF)) + "end" // lone high + low surrogate
	got := utf8Clean(in)
	if strings.ContainsRune(got, 0xD800) || strings.ContainsRune(got, 0xDFFF) {
		t.Fatalf("surrogates not replaced: %q", got)
	}
	if !strings.Contains(got, "\uFFFD") {
		t.Fatalf("expected replacement char, got %q", got)
	}
	// after clean, json.Marshal must not emit \ud800/\udfff escapes.
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), `\ud800`) || strings.Contains(string(b), `\udfff`) {
		t.Fatalf("json still contains surrogate escape: %s", b)
	}
}

func TestUTF8CleanNULAndInvalid(t *testing.T) {
	in := "a\x00b\xffc" // NUL + invalid UTF-8 byte
	got := utf8Clean(in)
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("NUL not removed: %q", got)
	}
	if !strings.Contains(got, "\uFFFD") {
		t.Fatalf("invalid UTF-8 should become replacement, got %q", got)
	}
}
