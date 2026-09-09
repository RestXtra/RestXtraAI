package db

import "testing"

func TestConnActionApprovalFlow(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	defer func() {
		_, _ = d.Exec(`DELETE FROM conn_actions WHERE connection_id IN (SELECT id FROM connections WHERE name='conn-action-test')`)
		_, _ = d.Exec(`DELETE FROM connections WHERE name='conn-action-test'`)
	}()

	// a connection to attach the actions to
	cid, err := d.SaveConnection(&Connection{Name: "conn-action-test", Kind: "ssh", Host: "10.0.0.1", Port: 22})
	if err != nil {
		t.Fatal(err)
	}

	// propose a dangerous action (pending)
	aid, err := d.CreateConnAction(&ConnAction{
		ConnectionID: cid, Kind: "exec", Command: "rm -rf /tmp/x",
		Rationale: "test", RequestedBy: "agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	pend, _ := d.ListConnPendingApprovals()
	found := false
	for _, p := range pend {
		if p.ID == aid {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending action not listed: %+v", pend)
	}

	// approve -> not pending, then execute result
	if err := d.SetConnActionApproval(aid, "approved", "analyst"); err != nil {
		t.Fatal(err)
	}
	a, err := d.GetConnAction(aid)
	if err != nil || a == nil || a.State != "approved" || a.DecidedBy != "analyst" {
		t.Fatalf("approved state wrong: %+v", a)
	}
	if err := d.SetConnActionResult(aid, "executed", "ok"); err != nil {
		t.Fatal(err)
	}
	a, _ = d.GetConnAction(aid)
	if a.State != "executed" || a.Result != "ok" || a.ExecutedAt == nil {
		t.Fatalf("executed state wrong: %+v", a)
	}

	// reject -> rejected, not pending
	rid, err := d.CreateConnAction(&ConnAction{ConnectionID: cid, Kind: "contain", Action: "block_ip", Target: "1.2.3.4", Command: "iptables -A INPUT -s 1.2.3.4 -j DROP", Rationale: "x", RequestedBy: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetConnActionApproval(rid, "rejected", "analyst"); err != nil {
		t.Fatal(err)
	}
	r, _ := d.GetConnAction(rid)
	if r.State != "rejected" {
		t.Fatalf("rejected state wrong: %+v", r)
	}

	// history for the connection
	hist, err := d.ListConnActions(cid, 10)
	if err != nil || len(hist) != 2 {
		t.Fatalf("history wrong: %+v err=%v", hist, err)
	}
	t.Log("conn action approval flow OK")
}
