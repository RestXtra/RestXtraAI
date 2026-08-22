package db

import "testing"

func TestTaskDelegationPersistenceAndCascade(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("delegated", "verify target", nil, 120, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	contract := TaskDelegation{
		ChildTaskID: task.ID, ParentRef: "42", Objective: " verify target ",
		AssetIDs: []int64{7, 7, -1}, RequiredEvidence: []string{"proof", " proof ", ""},
		AllowedTools: []string{"nmap", "NMAP", " Bash "},
		Budget:       DelegationBudget{MaxWallTimeSeconds: 120, MaxInputTokens: 5000, MaxToolCalls: 12},
	}
	if err := d.SaveTaskDelegation(contract); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTaskDelegation(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SchemaVersion != 1 || got.Objective != "verify target" || got.ParentRef != "42" {
		t.Fatalf("bad delegation: %+v", got)
	}
	if len(got.AssetIDs) != 1 || got.AssetIDs[0] != 7 || len(got.RequiredEvidence) != 1 ||
		len(got.AllowedTools) != 2 || got.AllowedTools[0] != "nmap" || got.AllowedTools[1] != "Bash" {
		t.Fatalf("delegation not normalized: %+v", got)
	}
	if err := d.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if after, err := d.GetTaskDelegation(task.ID); err != nil || after != nil {
		t.Fatalf("delegation did not cascade: got=%+v err=%v", after, err)
	}
}
