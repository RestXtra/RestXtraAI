package agent

import "testing"

func TestFilterToScope(t *testing.T) {
	// Unrestricted (empty allowed) keeps everything.
	kept, dropped := filterToScope([]int64{1, 2}, nil)
	if len(kept) != 2 || dropped != nil {
		t.Fatalf("unrestricted: kept=%v dropped=%v", kept, dropped)
	}
	// Scoped: keep in-scope, drop out-of-scope.
	kept, dropped = filterToScope([]int64{1, 2, 3}, []int64{2, 3})
	if len(kept) != 2 || kept[0] != 2 || kept[1] != 3 || len(dropped) != 1 || dropped[0] != 1 {
		t.Fatalf("scoped: kept=%v dropped=%v", kept, dropped)
	}
	// All out of scope.
	kept, dropped = filterToScope([]int64{9}, []int64{1, 2})
	if len(kept) != 0 || len(dropped) != 1 || dropped[0] != 9 {
		t.Fatalf("all out: kept=%v dropped=%v", kept, dropped)
	}
	// No anchors: nothing to filter.
	if kept, dropped = filterToScope(nil, []int64{1}); kept != nil || dropped != nil {
		t.Fatalf("empty anchors: kept=%v dropped=%v", kept, dropped)
	}
}
