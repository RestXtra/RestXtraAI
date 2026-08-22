package db

import (
	"testing"
	"time"
)

func TestTaskPerformanceBaselineMeasuresResultEfficiency(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("baseline", "measure result efficiency", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	started := time.Now().UTC().Add(-20 * time.Second).Truncate(time.Second)
	if _, err := d.Exec(`UPDATE tasks SET first_run_at=$2 WHERE id=$1`, task.ID, started); err != nil {
		t.Fatal(err)
	}
	store := d.Exploration(task.ExplorationID)
	var intentIDs []int64
	for _, summary := range []string{"Check Login", " check   login "} {
		id, err := store.AddIntent(map[string]any{"summary": summary}, 1, nil, "planner")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(`UPDATE exploration_nodes SET attempt_count=2 WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		intentIDs = append(intentIDs, id)
	}
	factID, err := store.AddNode(KindFact, map[string]any{"summary": "login exists", "evidence": "GET /login -> 200"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Link(intentIDs[0], RelYields, factID); err != nil {
		t.Fatal(err)
	}
	for _, id := range intentIDs {
		if err := store.SetNodeState(id, "done"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.AddNode(KindFact, map[string]any{"summary": "admin closed", "negative": true}, 5, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddNode(KindFinding, map[string]any{"summary": "issue", "evidence": map[string]any{"poc": "request -> response"}}, 9, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	input, output, cache := 100, 20, 25
	for _, activity := range []Activity{
		{Worker: "worker", Kind: "tool_use", Tool: "curl"},
		{Worker: "worker", Kind: "tool_result", Tool: "curl", IsError: true},
		{Worker: "worker", Kind: "result", InputTokens: &input, OutputTokens: &output, CacheReadTokens: &cache},
	} {
		if _, err := store.AppendActivity(activity); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetStatus(task.ID, "done"); err != nil {
		t.Fatal(err)
	}

	got, err := d.TaskPerformanceBaseline(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SchemaVersion != TaskPerformanceSchemaV1 || !got.Repeatable || got.Status != "done" {
		t.Fatalf("unexpected baseline identity: %+v", got)
	}
	if got.Results.ConfirmedFacts != 2 || got.Results.NegativeResults != 1 || got.Results.EvidenceBackedFacts != 1 ||
		got.Results.ConfirmedFindings != 1 || got.Results.EvidenceBackedFindings != 1 {
		t.Fatalf("unexpected result yield: %+v", got.Results)
	}
	if got.Intents.Total != 2 || got.Intents.Attempts != 4 || got.Intents.RepeatedAttempts != 2 ||
		got.Intents.DuplicateIntents != 1 || got.Intents.ZeroYieldIntents != 1 {
		t.Fatalf("unexpected intent metrics: %+v", got.Intents)
	}
	if got.Usage.InputTokens != 100 || got.Usage.ToolCalls != 1 || got.Usage.ToolErrors != 1 {
		t.Fatalf("unexpected usage: %+v", got.Usage)
	}
	if got.Efficiency.ToolErrorRate != 1 || got.Efficiency.DuplicateIntentRate != 0.5 || got.Efficiency.ZeroYieldIntentRate != 0.5 ||
		got.Efficiency.InputTokensPerFinding == nil || *got.Efficiency.InputTokensPerFinding != 100 {
		t.Fatalf("unexpected efficiency ratios: %+v", got.Efficiency)
	}
	if got.TimeToFirstConfirmedFactSeconds == nil || got.TimeToFirstEvidenceFactSeconds == nil ||
		got.TimeToFirstConfirmedFindingSeconds == nil || got.TaskCompletionSeconds == nil {
		t.Fatalf("missing timing metrics: %+v", got)
	}
}

func TestSummarizeTaskPerformanceBaselinesExcludesMissingAndRunning(t *testing.T) {
	ptr := func(value int64) *int64 { return &value }
	items := []*TaskPerformanceBaseline{
		{TaskID: 1, Repeatable: true, TimeToFirstConfirmedFactSeconds: ptr(10), TimeToFirstEvidenceFactSeconds: ptr(15), TimeToFirstConfirmedFindingSeconds: ptr(30), TaskCompletionSeconds: ptr(100)},
		{TaskID: 2, Repeatable: true, TimeToFirstConfirmedFactSeconds: ptr(20), TimeToFirstEvidenceFactSeconds: ptr(25), TaskCompletionSeconds: ptr(200)},
		{TaskID: 3, Repeatable: true, TimeToFirstConfirmedFactSeconds: ptr(30), TimeToFirstEvidenceFactSeconds: ptr(35), TimeToFirstConfirmedFindingSeconds: ptr(90), TaskCompletionSeconds: ptr(300)},
		{TaskID: 4, Repeatable: false, TimeToFirstConfirmedFactSeconds: ptr(1)},
	}
	got := SummarizeTaskPerformanceBaselines(items)
	if got.RepeatableTasks != 3 || len(got.ExcludedTaskIDs) != 1 || got.ExcludedTaskIDs[0] != 4 {
		t.Fatalf("unexpected cohort membership: %+v", got)
	}
	if got.TimeToFirstConfirmedFact.Samples != 3 || *got.TimeToFirstConfirmedFact.P50 != 20 || *got.TimeToFirstConfirmedFact.P95 != 30 {
		t.Fatalf("unexpected fact percentiles: %+v", got.TimeToFirstConfirmedFact)
	}
	if got.TimeToFirstConfirmedFinding.Samples != 2 || *got.TimeToFirstConfirmedFinding.P50 != 30 || *got.TimeToFirstConfirmedFinding.P95 != 90 {
		t.Fatalf("missing finding should not become zero: %+v", got.TimeToFirstConfirmedFinding)
	}
}
