package server

import "testing"

func TestParsePerformanceTaskIDsIsStrictAndStable(t *testing.T) {
	ids, err := parsePerformanceTaskIDs(" 3,1,3,2 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != 3 || ids[1] != 1 || ids[2] != 2 {
		t.Fatalf("unexpected task ids: %v", ids)
	}
	for _, raw := range []string{"", "1,bad", "1,", "0", "-1"} {
		if _, err := parsePerformanceTaskIDs(raw); err == nil {
			t.Fatalf("parsePerformanceTaskIDs(%q) should fail", raw)
		}
	}
}
