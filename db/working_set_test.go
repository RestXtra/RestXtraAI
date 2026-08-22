package db

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExplicitConstraintsOnlyKeepsExplicitPolicy(t *testing.T) {
	got := explicitConstraints("测试 example.com。仅限已授权资产；普通背景信息\n禁止破坏性操作; only use test accounts")
	want := []string{"仅限已授权资产", "禁止破坏性操作", "only use test accounts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("constraints mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func TestWorkingSetProjectionIsVersionedAndIdempotent(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("仅限已授权资产；禁止破坏性操作", "确认目标风险", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID) //nolint:errcheck
	store := d.Exploration(task.ExplorationID)
	intentID, err := store.AddIntent(map[string]any{"summary": "验证登录接口"}, 5, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddNode(KindFact, map[string]any{"summary": "识别到登录接口", "evidence": "GET /login -> 200"}, 5, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendActivity(Activity{EventType: EventToolResult, Kind: "tool_result", NodeID: &intentID,
		Tool: "http_get", ToolUseID: "tool-1", IsError: true, Summary: "connection refused", EventKey: "ws-error"}); err != nil {
		t.Fatal(err)
	}
	summary := Activity{EventType: EventSummaryCreated, EventOnly: true, Detail: "deterministic summary", EventKey: "ws-summary"}
	if _, err := store.AppendActivity(summary); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendActivity(summary); err != nil {
		t.Fatal(err)
	}
	ws, err := store.WorkingSet()
	if err != nil {
		t.Fatal(err)
	}
	if ws == nil || ws.Version != 1 || ws.Fixed.Objective != "确认目标风险" || len(ws.Fixed.ConfirmedFacts) != 1 {
		t.Fatalf("unexpected working set: %+v", ws)
	}
	if len(ws.Sliding.PendingEvidence) != 1 || len(ws.Sliding.RecentToolErrors) != 1 || len(ws.Fixed.RiskPolicy) != 1 {
		t.Fatalf("missing projected layers: %+v", ws)
	}
	var payload json.RawMessage
	if err := d.QueryRow(`SELECT payload FROM agent_events WHERE id=$1`, ws.SourceEventID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil || event["working_set"] == nil {
		t.Fatalf("summary event missing replay payload: %s (err=%v)", payload, err)
	}
}

func TestWorkingSetRestoredEventIsAuditable(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	task, err := d.CreateTask("restore audit", "resume safely", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID) //nolint:errcheck
	store := d.Exploration(task.ExplorationID)
	payload := json.RawMessage(`{"working_set_id":7,"version":2,"content_hash":"sha256:test","source_event_id":11,"agent":"planner"}`)
	if _, err := store.AppendActivity(Activity{EventType: EventWorkingSetRestored, EventOnly: true, Worker: "planner", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	events, _, err := store.AgentEvents(0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != EventWorkingSetRestored || string(events[0].Payload) == "" {
		t.Fatalf("unexpected restore audit events: %+v", events)
	}
}
