package db

import (
	"sync"
	"testing"
	"time"
)

func TestExplorationFlow(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	expID, err := d.CreateExploration("test", "拿下测试目标")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID) // cascades nodes/edges/activity
	es := d.Exploration(expID)

	// goal node + two intents
	goal, err := es.AddGoal(map[string]any{"text": "getadmin", "vulnclass": "authz"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := es.AddIntent(map[string]any{"summary": "enumerate endpoints"}, 5, nil, "planner"); err != nil {
		t.Fatal(err)
	}
	i2, err := es.AddIntent(map[string]any{"summary": "test idor"}, 8, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}

	// frontier ordered by priority desc → i2(8) before i1(5)
	fr, err := es.Frontier(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fr) != 2 || fr[0].ID != i2 {
		t.Fatalf("frontier order wrong: %+v", fr)
	}

	// lease claim selects the highest-priority ready intent.
	claimed, err := es.ClaimNextIntentLease("worker-1/attempt-1", "worker-1", time.Minute)
	if err != nil || claimed == nil || claimed.ID != i2 {
		t.Fatalf("claim i2: claimed=%+v err=%v", claimed, err)
	}
	if claimed.AttemptCount != 1 || claimed.LeaseExpiresAt == nil {
		t.Fatalf("claim lease metadata missing: %+v", claimed)
	}
	var eventAgent, eventOwner string
	if err := d.QueryRow(`SELECT agent, payload->>'owner' FROM agent_events
WHERE exploration_id=$1 AND event_type=$2 ORDER BY id DESC LIMIT 1`, expID, EventIntentClaimed).Scan(&eventAgent, &eventOwner); err != nil {
		t.Fatal(err)
	}
	if eventAgent != "worker-1" || eventOwner != "worker-1/attempt-1" {
		t.Fatalf("claim event identity: agent=%q owner=%q", eventAgent, eventOwner)
	}

	// finding yields from intent, proves goal
	find, err := es.AddNode("finding", map[string]any{"vulnclass": "idor", "severity": "high"}, 9, "confirmed", "worker-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(i2, "yields", find); err != nil {
		t.Fatal(err)
	}
	if err := es.Link(find, "proves", goal); err != nil {
		t.Fatal(err)
	}
	if err := es.SetNodeState(goal, "met"); err != nil {
		t.Fatal(err)
	}

	// activity poll by id cursor
	id1, err := es.AppendActivity(Activity{Worker: "worker-1", Kind: "tool_use", Tool: "Bash", Summary: "ran curl", Detail: "full output"})
	if err != nil {
		t.Fatal(err)
	}
	items, cursor, err := es.ActivityList(nil, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || cursor != id1 {
		t.Fatalf("activity list: items=%d cursor=%d", len(items), cursor)
	}
	det, _ := es.ActivityDetail(id1)
	if det != "full output" {
		t.Fatalf("detail want 'full output', got %q", det)
	}
	// incremental: nothing new after cursor
	items2, _, _ := es.ActivityList(nil, cursor, 100)
	if len(items2) != 0 {
		t.Fatalf("incremental poll should be empty, got %d", len(items2))
	}

	// stats
	st, _ := es.Stats()
	if st["intent"] != 2 || st["goal"] != 1 || st["finding"] != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestIntentLeaseWorkGraph(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	expID, err := d.CreateExploration("lease-graph", "lease graph test")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	es := d.Exploration(expID)

	parent, err := es.AddIntent(map[string]any{"summary": "parent"}, 10, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	child, err := es.AddIntent(map[string]any{"summary": "child"}, 100, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	low, err := es.AddIntent(map[string]any{"summary": "low"}, 1, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(parent, RelDerivedFrom, child); err != nil {
		t.Fatal(err)
	}

	// The high-priority child is gated, so the ready parent wins.
	n, err := es.ClaimNextIntentLease("owner-a", "worker-a", time.Minute)
	if err != nil || n == nil || n.ID != parent {
		t.Fatalf("want ready parent, got node=%+v err=%v", n, err)
	}
	if ok, err := es.RenewIntentLease(parent, "owner-b", time.Minute); err != nil || ok {
		t.Fatalf("wrong owner renewed lease: ok=%v err=%v", ok, err)
	}
	if ok, err := es.FinishIntentLease(parent, "owner-b", "done"); err != nil || ok {
		t.Fatalf("wrong owner finished lease: ok=%v err=%v", ok, err)
	}
	if ok, err := es.RenewIntentLease(parent, "owner-a", time.Minute); err != nil || !ok {
		t.Fatalf("owner renew failed: ok=%v err=%v", ok, err)
	}
	if ok, err := es.FinishIntentLease(parent, "owner-a", "done"); err != nil || !ok {
		t.Fatalf("owner finish failed: ok=%v err=%v", ok, err)
	}

	// A done parent without a yield still does not unlock the child.
	n, err = es.ClaimNextIntentLease("owner-low", "worker-low", time.Minute)
	if err != nil || n == nil || n.ID != low {
		t.Fatalf("want low while child gated, got node=%+v err=%v", n, err)
	}
	if ok, err := es.FinishIntentLease(low, "owner-low", "done"); err != nil || !ok {
		t.Fatalf("finish low: ok=%v err=%v", ok, err)
	}
	fact, err := es.AddNode("fact", map[string]any{"summary": "dependency output"}, 0, "confirmed", "owner-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := es.Link(parent, RelYields, fact); err != nil {
		t.Fatal(err)
	}

	n, err = es.ClaimNextIntentLease("owner-child-a", "worker-child", time.Minute)
	if err != nil || n == nil || n.ID != child || n.AttemptCount != 1 {
		t.Fatalf("want unlocked child attempt 1, got node=%+v err=%v", n, err)
	}
	if _, err := d.Exec(`UPDATE exploration_nodes SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, child); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := es.ClaimNextIntentLease("owner-child-b", "worker-child", time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != child || reclaimed.AttemptCount != 2 {
		t.Fatalf("expired reclaim failed: node=%+v err=%v", reclaimed, err)
	}
	if ok, err := es.FinishIntentLease(child, "owner-child-a", "done"); err != nil || ok {
		t.Fatalf("stale owner overwrote reclaim: ok=%v err=%v", ok, err)
	}
	if ok, err := es.FinishIntentLease(child, "owner-child-b", "done"); err != nil || !ok {
		t.Fatalf("new owner finish failed: ok=%v err=%v", ok, err)
	}

	recovery, err := es.AddIntent(map[string]any{"summary": "recover on startup"}, 0, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := es.ClaimNextIntentLease("dead-owner", "dead-worker", time.Minute); err != nil || n == nil || n.ID != recovery {
		t.Fatalf("claim recovery intent: node=%+v err=%v", n, err)
	}
	if _, err := d.Exec(`UPDATE exploration_nodes SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, recovery); err != nil {
		t.Fatal(err)
	}
	if count, err := es.RequeueExpiredIntentLeases(); err != nil || count != 1 {
		t.Fatalf("requeue expired: count=%d err=%v", count, err)
	}
	reopened, err := es.GetNode(recovery)
	if err != nil || reopened == nil || reopened.State != "open" || reopened.Owner != "" || reopened.LeaseExpiresAt != nil {
		t.Fatalf("requeued node metadata: node=%+v err=%v", reopened, err)
	}
}

func TestConcurrentIntentLeaseClaimIsUnique(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	expID, err := d.CreateExploration("lease-concurrent", "concurrent lease test")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
	es := d.Exploration(expID)
	intentID, err := es.AddIntent(map[string]any{"summary": "only"}, 1, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan *Node, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, owner := range []string{"concurrent-a", "concurrent-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			<-start
			n, err := es.ClaimNextIntentLease(owner, owner, time.Minute)
			results <- n
			errs <- err
		}(owner)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	winners := 0
	for n := range results {
		if n != nil {
			winners++
			if n.ID != intentID {
				t.Fatalf("claimed wrong intent: %+v", n)
			}
		}
	}
	if winners != 1 {
		t.Fatalf("want exactly one lease winner, got %d", winners)
	}
	var events int
	if err := d.QueryRow(`SELECT count(*) FROM agent_events WHERE exploration_id=$1 AND event_type=$2`, expID, EventIntentClaimed).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("want one intent_claimed event, got %d", events)
	}
}
