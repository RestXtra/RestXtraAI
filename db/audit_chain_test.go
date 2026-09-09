package db

import (
	"testing"
	"time"
)

func TestAuditHashChain(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()
	defer func() { _, _ = d.ClearAudit() }()

	// Seed a few chained entries.
	for i := 0; i < 3; i++ {
		if err := d.RecordAudit(AuditEntry{Actor: "alice", Category: "conn", Action: "exec", Result: "success", Message: "cmd" + string(rune('0'+i)), IP: "127.0.0.1"}); err != nil {
			t.Fatal(err)
		}
	}

	// Untampered chain must verify clean.
	check, err := d.VerifyAuditChain()
	if err != nil {
		t.Fatal(err)
	}
	if check.Broken != 0 || check.Legacy != 0 || check.Total != 3 {
		t.Fatalf("expected clean chain of 3, got %+v", check)
	}

	// Tamper with a row's message → broken.
	if _, err := d.Exec(`UPDATE audit_logs SET message='FORGED' WHERE id=(SELECT min(id) FROM audit_logs)`); err != nil {
		t.Fatal(err)
	}
	check, err = d.VerifyAuditChain()
	if err != nil {
		t.Fatal(err)
	}
	if check.Broken == 0 {
		t.Fatalf("tampered row not detected: %+v", check)
	}
	t.Logf("audit hash chain OK: broken=%d ids=%v", check.Broken, check.BrokenIDs)
}

func TestAuditChainHashStable(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Second)
	h1 := auditChainHash("prev", "a", "b", "c", "d", "e", "1.2.3.4", ts)
	h2 := auditChainHash("prev", "a", "b", "c", "d", "e", "1.2.3.4", ts)
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	h3 := auditChainHash("prev2", "a", "b", "c", "d", "e", "1.2.3.4", ts)
	if h3 == h1 {
		t.Fatal("different prev must change hash")
	}
}
