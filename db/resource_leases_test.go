package db

import (
	"testing"
	"time"
)

func TestIntentResourceLeasesCoordinateConflicts(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("resource coordination", "serialize account use", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID) //nolint:errcheck
	store := d.Exploration(task.ExplorationID)
	for _, summary := range []string{"first", "second"} {
		if _, err := store.AddIntent(map[string]any{"summary": summary, "intent_class": "auth", "account_scopes": []string{"admin-session"}}, 5, nil, "planner"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ClaimNextIntentLease("owner-a", "worker-a", time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first claim: node=%+v err=%v", first, err)
	}
	blocked, err := store.ClaimNextIntentLease("owner-b", "worker-b", time.Minute)
	if err != nil || blocked != nil {
		t.Fatalf("exclusive claim should wait: node=%+v err=%v", blocked, err)
	}
	var waits int
	if err := d.QueryRow(`SELECT count(*) FROM agent_events WHERE exploration_id=$1 AND event_type=$2`, task.ExplorationID, EventResourceConflictWait).Scan(&waits); err != nil {
		t.Fatal(err)
	}
	if waits != 1 {
		t.Fatalf("want one deduplicated conflict event, got %d", waits)
	}
	if _, err := store.ClaimNextIntentLease("owner-b", "worker-b", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM agent_events WHERE exploration_id=$1 AND event_type=$2`, task.ExplorationID, EventResourceConflictWait).Scan(&waits); err != nil {
		t.Fatal(err)
	}
	if waits != 1 {
		t.Fatalf("same blocking lease should stay deduplicated, got %d", waits)
	}
	if ok, err := store.FinishIntentLease(first.ID, "owner-a", "done"); err != nil || !ok {
		t.Fatalf("release first lease: ok=%v err=%v", ok, err)
	}
	second, err := store.ClaimNextIntentLease("owner-b", "worker-b", time.Minute)
	if err != nil || second == nil || second.ID == first.ID {
		t.Fatalf("second claim after release: node=%+v err=%v", second, err)
	}
}

func TestSharedIntentResourcesCanRunTogether(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("shared coordination", "parallel reads", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID) //nolint:errcheck
	store := d.Exploration(task.ExplorationID)
	for _, summary := range []string{"read one", "read two"} {
		if _, err := store.AddIntent(map[string]any{"summary": summary, "intent_class": "recon", "rate_limit_domains": []string{"example.test"}}, 5, nil, "planner"); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ClaimNextIntentLease("shared-a", "worker-a", time.Minute)
	if err != nil || first == nil {
		t.Fatalf("first shared claim: node=%+v err=%v", first, err)
	}
	second, err := store.ClaimNextIntentLease("shared-b", "worker-b", time.Minute)
	if err != nil || second == nil {
		t.Fatalf("shared claim blocked: node=%+v err=%v", second, err)
	}
}
