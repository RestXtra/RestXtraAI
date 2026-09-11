package agent

import "testing"

func TestFindingDedupKey(t *testing.T) {
	a := findingDedupKey("SQLi", "https://ex.com/a?id=1", []string{"https://ex.com/a?id=1"}, []int64{3, 1})
	b := findingDedupKey("sqli", "HTTPS://EX.COM/a?id=1 ", []string{"https://ex.com/a?id=1"}, []int64{1, 3})
	if a == "" || a != b {
		t.Fatalf("equivalent findings should share a key: %q vs %q", a, b)
	}
	c := findingDedupKey("SQLi", "https://ex.com/b?id=2", []string{"https://ex.com/b?id=2"}, []int64{1, 3})
	if c == a {
		t.Fatalf("different target must not collide: %q", c)
	}
	if got := findingDedupKey("SQLi", "", nil, nil); got != "" {
		t.Fatalf("class-only (no target) should not dedup, got %q", got)
	}
	if got := findingDedupKey("", "https://ex.com/a", nil, nil); got != "" {
		t.Fatalf("missing vulnclass should not dedup, got %q", got)
	}
}
