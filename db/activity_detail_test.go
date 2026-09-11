package db

import (
	"database/sql"
	"strings"
	"testing"
)

// TestActivityDetailServedFromCanonicalEvent verifies the detail de-duplication:
// AppendActivity no longer stores detail in the activity row, but every reader
// (ActivityDetail / ActivityByIDs / ActivityTraceSearch) still returns it from the
// canonical agent_events payload.
func TestActivityDetailServedFromCanonicalEvent(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}

	var expID int64
	if err := d.QueryRow(`INSERT INTO explorations(description,goal) VALUES ('act detail','test') RETURNING id`).Scan(&expID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, expID)
		d.Close()
	})
	s := d.Exploration(expID)

	nodeID, err := s.AddNode(KindFact, map[string]any{"summary": "n"}, 5, "confirmed", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	const body = "FULL-DETAIL-BODY-12345"
	id, err := s.AppendActivity(Activity{Worker: "worker", NodeID: &nodeID, Kind: "tool_result", Tool: "bash",
		Summary: "preview", Detail: body})
	if err != nil {
		t.Fatal(err)
	}

	// The activity row must NOT carry the detail payload any more.
	var col sql.NullString
	if err := d.QueryRow(`SELECT detail FROM activity WHERE id=$1`, id).Scan(&col); err != nil {
		t.Fatal(err)
	}
	if col.Valid && col.String != "" {
		t.Fatalf("activity.detail should be empty, got %q", col.String)
	}

	if got, err := s.ActivityDetail(id); err != nil || got != body {
		t.Fatalf("ActivityDetail = %q, err=%v", got, err)
	}

	byIDs, err := s.ActivityByIDs([]int64{id})
	if err != nil || len(byIDs) != 1 || byIDs[0].Detail != body {
		t.Fatalf("ActivityByIDs = %+v, err=%v", byIDs, err)
	}

	found, err := s.ActivityTraceSearch(&nodeID, "DETAIL-BODY", 10)
	if err != nil || len(found) == 0 {
		t.Fatalf("ActivityTraceSearch = %+v, err=%v", found, err)
	}

	// ListCommands searches u.detail (command) and returns r.detail (output) —
	// both must come back from the canonical events, not the empty columns.
	if _, err := s.AppendActivity(Activity{Worker: "worker", NodeID: &nodeID, Kind: "tool_use", Tool: "bash",
		ToolUseID: "tu-1", Summary: "cmd", Detail: "SEARCHME-command-payload"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendActivity(Activity{Worker: "worker", NodeID: &nodeID, Kind: "tool_result", Tool: "bash",
		ToolUseID: "tu-1", Summary: "out", Detail: "SEARCHME-output-payload"}); err != nil {
		t.Fatal(err)
	}
	recs, total, err := d.ListCommands(&expID, "SEARCHME", 0, 10)
	if err != nil || total == 0 || len(recs) == 0 {
		t.Fatalf("ListCommands = %+v total=%d err=%v", recs, total, err)
	}
	if !strings.Contains(recs[0].Command, "SEARCHME-command-payload") || !strings.Contains(recs[0].Output, "SEARCHME-output-payload") {
		t.Fatalf("ListCommands detail not resolved from events: %+v", recs[0])
	}
}
