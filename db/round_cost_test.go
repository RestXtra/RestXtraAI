package db

import "testing"

func TestRoundCostsByWorkerCountsTerminalUsageOnce(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	expID, err := d.CreateExploration("round cost test", "verify aggregate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, expID) })
	store := d.Exploration(expID)
	input, output := 100, 25
	if _, err := store.AppendActivity(Activity{Worker: "worker-1", Kind: "usage", InputTokens: &input, OutputTokens: &output}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendActivity(Activity{Worker: "worker-1", Kind: "tool_use", Tool: "http_get"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendActivity(Activity{Worker: "worker-1", Kind: "tool_result", Tool: "http_get", IsError: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendActivity(Activity{Worker: "worker-1", Kind: "result", InputTokens: &input, OutputTokens: &output}); err != nil {
		t.Fatal(err)
	}

	costs, err := store.RoundCostsByWorker()
	if err != nil {
		t.Fatal(err)
	}
	if len(costs) != 1 {
		t.Fatalf("cost rows = %d, want 1", len(costs))
	}
	got := costs[0]
	if got.Worker != "worker-1" || got.Rounds != 1 || got.ToolCalls != 1 || got.ToolErrors != 1 {
		t.Fatalf("unexpected execution counters: %+v", got)
	}
	if got.InputTokens != input || got.OutputTokens != output {
		t.Fatalf("terminal usage should be counted once: %+v", got)
	}
}
