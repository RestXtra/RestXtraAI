package db

import "testing"

// TestTaskConversationRoundTrip verifies conversation ownership is persisted and
// read back on the task DTO (used to group chat-driven orchestration).
func TestTaskConversationRoundTrip(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}

	var convID int64
	if err := d.QueryRow(`INSERT INTO conversations(agent_key,title) VALUES ('worker','conv test') RETURNING id`).Scan(&convID); err != nil {
		t.Fatal(err)
	}
	task, err := d.CreateTask("conv test", "group by conversation", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tasks WHERE id=$1`, task.ID)
		_, _ = d.Exec(`DELETE FROM conversations WHERE id=$1`, convID)
		d.Close()
	})

	if err := d.SetTaskConversation(task.ID, convID); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetTask(task.ID)
	if err != nil || got == nil {
		t.Fatalf("GetTask: %v %v", got, err)
	}
	if got.ConversationID != convID {
		t.Fatalf("conversation_id = %d, want %d", got.ConversationID, convID)
	}
}
