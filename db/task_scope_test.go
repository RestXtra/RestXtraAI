package db

import "testing"

// TestTaskAllowedAssetIDs verifies the task-boundary read: a delegation's
// asset_ids bound the child task's exploration scope; ordinary tasks are nil.
func TestTaskAllowedAssetIDs(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	task, err := d.CreateTask("scope test", "only dispatched assets", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.DeleteTask(task.ID)
		d.Close()
	})

	// No delegation → unrestricted.
	if ids := d.Assets().TaskAllowedAssetIDs(task.ID); ids != nil {
		t.Fatalf("no delegation should be unrestricted, got %v", ids)
	}

	if err := d.SaveTaskDelegation(TaskDelegation{ChildTaskID: task.ID, Objective: "o", AssetIDs: []int64{7, 8}}); err != nil {
		t.Fatal(err)
	}
	ids := d.Assets().TaskAllowedAssetIDs(task.ID)
	if len(ids) != 2 || ids[0] != 7 || ids[1] != 8 {
		t.Fatalf("got %v, want [7 8]", ids)
	}
}
