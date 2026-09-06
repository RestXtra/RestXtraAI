package db

import "testing"

func TestC2TaskApprovalFlow(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	defer func() { _, _ = d.ClearC2Sessions() }()

	if err := d.UpsertC2Session(&C2Session{SessionID: "appr-sess", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	// pending task must NOT be claimed
	pid, err := d.CreateC2TaskApproval("appr-sess", "postex persist", "", nil, "pending")
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := d.ClaimC2Tasks("appr-sess", 10)
	if len(claimed) != 0 {
		t.Fatalf("pending task was claimed: %+v", claimed)
	}

	// approve -> claimable
	if err := d.SetC2TaskApproval(pid, "approved"); err != nil {
		t.Fatal(err)
	}
	claimed, _ = d.ClaimC2Tasks("appr-sess", 10)
	if len(claimed) != 1 || claimed[0].ID != pid {
		t.Fatalf("approved task not claimed: %+v", claimed)
	}

	// a second pending task -> reject -> state failed, not claimed
	rid, err := d.CreateC2TaskApproval("appr-sess", "postex upload", "", nil, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetC2TaskApproval(rid, "rejected"); err != nil {
		t.Fatal(err)
	}
	t2, err := d.GetC2TaskByID(rid)
	if err != nil || t2 == nil || t2.State != "failed" || t2.Approval != "rejected" {
		t.Fatalf("rejected task state wrong: %+v", t2)
	}

	// pending list contains nothing now
	pend, _ := d.ListC2PendingApprovals()
	for _, p := range pend {
		if p.ID == pid || p.ID == rid {
			t.Fatalf("resolved task still pending: %+v", p)
		}
	}
	t.Log("approval flow OK")
}
