package db

import (
	"errors"
	"testing"
)

func TestAddIntentWithLineageIsAtomicAndEvidenceAware(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("intent dedupe", "avoid duplicate exploration", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	store := d.Exploration(task.ExplorationID)

	first, created, err := store.AddIntentWithLineage(map[string]any{"summary": " Check   Login "}, 5, nil, nil, "planner")
	if err != nil || !created {
		t.Fatalf("first intent: id=%d created=%v err=%v", first, created, err)
	}
	duplicate, created, err := store.AddIntentWithLineage(map[string]any{"summary": "check login"}, 9, nil, nil, "planner")
	if err != nil || created || duplicate != first {
		t.Fatalf("duplicate intent: id=%d created=%v err=%v", duplicate, created, err)
	}

	fact, err := store.AddNode(KindFact, map[string]any{"summary": "new login parameter"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	derived, created, err := store.AddIntentWithLineage(map[string]any{"summary": "check login"}, 5, nil, []int64{fact}, "planner")
	if err != nil || !created || derived == first {
		t.Fatalf("new-evidence intent: id=%d created=%v err=%v", derived, created, err)
	}

	goal, err := store.AddGoal(map[string]any{"summary": "invalid parent"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddIntentWithLineage(map[string]any{"summary": "bad lineage"}, 5, nil, []int64{goal}, "planner"); err == nil {
		t.Fatal("goal parent should be rejected")
	}
	if _, _, err := store.AddIntentWithLineage(map[string]any{"summary": "bad asset"}, 5, []int64{1 << 62}, []int64{fact}, "planner"); err == nil {
		t.Fatal("missing asset should fail the transaction")
	}
	intents, err := store.ListByKind(KindIntent, 100)
	if err != nil || len(intents) != 2 {
		t.Fatalf("invalid parent left a partial intent: count=%d err=%v", len(intents), err)
	}
}

func TestAddIntentWithLineageFusesRepeatedZeroYieldScope(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("scope fuse", "stop repeated no-result work", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	store := d.Exploration(task.ExplorationID)

	for _, summary := range []string{"try login form", "probe session endpoint"} {
		id, created, err := store.AddIntentWithLineage(map[string]any{"summary": summary, "intent_class": " Auth "}, 5, nil, nil, "planner")
		if err != nil || !created {
			t.Fatalf("seed intent %q: id=%d created=%v err=%v", summary, id, created, err)
		}
		if err := store.SetNodeState(id, "done"); err != nil {
			t.Fatal(err)
		}
	}
	if id, created, err := store.AddIntentWithLineage(map[string]any{"summary": "try alternate cookie", "intent_class": "auth"}, 5, nil, nil, "planner"); id != 0 || created || !errors.Is(err, ErrIntentScopeFused) {
		t.Fatalf("expected scope fuse: id=%d created=%v err=%v", id, created, err)
	}

	fact, err := store.AddNode(KindFact, map[string]any{"summary": "new auth parameter"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed, created, err := store.AddIntentWithLineage(map[string]any{"summary": "recheck with new parameter", "intent_class": "auth"}, 5, nil, []int64{fact}, "planner")
	if err != nil || !created || allowed == 0 {
		t.Fatalf("new evidence should bypass fuse: id=%d created=%v err=%v", allowed, created, err)
	}
	result, err := store.AddNode(KindFact, map[string]any{"summary": "auth behavior confirmed"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Link(allowed, RelYields, result); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNodeState(allowed, "done"); err != nil {
		t.Fatal(err)
	}
	if id, created, err := store.AddIntentWithLineage(map[string]any{"summary": "follow successful auth lead", "intent_class": "auth"}, 5, nil, nil, "planner"); err != nil || !created || id == 0 {
		t.Fatalf("latest successful intent should reset fuse: id=%d created=%v err=%v", id, created, err)
	}

	var duplicateEvents, fuseEvents int
	if err := d.QueryRow(`SELECT
COUNT(*) FILTER (WHERE payload->>'reason'='duplicate_intent'),
COUNT(*) FILTER (WHERE payload->>'reason'='zero_yield_scope_fuse')
FROM agent_events WHERE exploration_id=$1 AND event_type=$2`, task.ExplorationID, EventIntentRejected).Scan(&duplicateEvents, &fuseEvents); err != nil {
		t.Fatal(err)
	}
	if duplicateEvents != 0 || fuseEvents != 1 {
		t.Fatalf("unexpected rejection events: duplicate=%d fuse=%d", duplicateEvents, fuseEvents)
	}
}

func TestAddIntentWithLineageUpgradesLegacyExplorationOrigin(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	explorationID, err := d.CreateExploration("legacy", "continue safely")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, explorationID) })
	store := d.Exploration(explorationID)
	intentID, created, err := store.AddIntentWithLineage(map[string]any{"summary": "first direction"}, 5, nil, nil, "planner")
	if err != nil || !created || intentID <= 0 {
		t.Fatalf("legacy intent: id=%d created=%v err=%v", intentID, created, err)
	}
	originID, err := store.OriginFactID()
	if err != nil || originID <= 0 {
		t.Fatalf("legacy origin was not created: id=%d err=%v", originID, err)
	}
	edges, err := store.Edges(100)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, edge := range edges {
		if edge.From == originID && edge.To == intentID && edge.Rel == RelDerivedFrom {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("intent is not linked to upgraded origin: %+v", edges)
	}
}
