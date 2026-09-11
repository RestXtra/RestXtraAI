package agent

import "testing"

func TestReconGateMissingNilSafe(t *testing.T) {
	old := ReconGate
	t.Cleanup(func() { ReconGate = old })

	ReconGate = nil
	if got := (&ToolSet{}).reconGateMissing(); got != nil {
		t.Fatalf("nil gate should return nil, got %v", got)
	}

	ReconGate = func(int64) []string { return []string{"子域名"} }
	if got := (&ToolSet{taskID: 0}).reconGateMissing(); got != nil {
		t.Fatalf("unknown task should return nil, got %v", got)
	}
	got := (&ToolSet{taskID: 7}).reconGateMissing()
	if len(got) != 1 || got[0] != "子域名" {
		t.Fatalf("gate list not surfaced: %v", got)
	}
}
