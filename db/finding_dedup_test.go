package db

import (
	"fmt"
	"testing"
	"time"
)

func TestAddFindingDedup(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}

	suffix := time.Now().UnixNano()
	var explorationID, taskID int64
	if err := d.QueryRow(`INSERT INTO explorations(description,goal) VALUES ('dedup test','test') RETURNING id`).Scan(&explorationID); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`INSERT INTO tasks(description,goal,exploration_id) VALUES ('dedup test','test',$1) RETURNING id`, explorationID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	var id1, id2, id3 int64
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM findings WHERE task_id=$1`, taskID)
		_, _ = d.Exec(`DELETE FROM tasks WHERE id=$1`, taskID)
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, explorationID)
		d.Close()
	})

	key := fmt.Sprintf("key-%d", suffix)
	id1, merged, err := d.AddFindingDedup(taskID, 0, "SQLi", "medium", "s1", "e1", "w", nil, key)
	if err != nil || merged {
		t.Fatalf("first insert: id=%d merged=%v err=%v", id1, merged, err)
	}
	id2, merged, err = d.AddFindingDedup(taskID, 0, "SQLi", "high", "s2", "e2", "w", nil, key)
	if err != nil || !merged || id2 != id1 {
		t.Fatalf("duplicate should merge into #%d: id=%d merged=%v err=%v", id1, id2, merged, err)
	}
	// severity should have been bumped to the higher value.
	var sev string
	if err := d.QueryRow(`SELECT severity FROM findings WHERE id=$1`, id1).Scan(&sev); err != nil || sev != "high" {
		t.Fatalf("severity not bumped: %q err=%v", sev, err)
	}
	id3, merged, err = d.AddFindingDedup(taskID, 0, "SQLi", "medium", "s3", "e3", "w", nil, key+"-other")
	if err != nil || merged || id3 == id1 {
		t.Fatalf("different key must insert new: id=%d merged=%v err=%v", id3, merged, err)
	}
}
