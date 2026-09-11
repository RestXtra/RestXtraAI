package server

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/RestXtra/RestXtraAI/db"
)

func TestTaskDelegationResultIsStructuredAndBounded(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	task, err := m.CreateTask("child", "verify only", nil, 60, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.DeleteTask(task.ID)
	contract := db.TaskDelegation{ChildTaskID: parseTaskID(task.ID), ParentRef: "parent-1", Objective: "verify only",
		AllowedTools: []string{"nmap"}, Budget: db.DelegationBudget{MaxInputTokens: 5, MaxToolCalls: 1}}
	if err := m.PG().SaveTaskDelegation(contract); err != nil {
		t.Fatal(err)
	}
	if _, err := task.Store.AddNode(db.KindFact, map[string]any{"summary": "positive", "evidence": "HTTP 200", "confidence": "observed"}, 5, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := task.Store.AddNode(db.KindFact, map[string]any{"summary": "closed", "evidence": "connection refused", "confidence": "observed", "negative": true}, 5, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := task.Store.AddNode(db.KindFinding, map[string]any{"summary": "confirmed issue", "severity": "high", "evidence": "request -> response"}, 9, "confirmed", "worker", nil); err != nil {
		t.Fatal(err)
	}
	artifactPath := t.TempDir() + string(os.PathSeparator) + "evidence.txt"
	if err := os.WriteFile(artifactPath, []byte("artifact evidence\n"), 0600); err != nil {
		t.Fatal(err)
	}
	input, output := 10, 2
	if _, err := task.Store.AppendActivity(db.Activity{Worker: "worker", Kind: "result", Summary: "done",
		InputTokens: &input, OutputTokens: &output, Artifacts: []db.ArtifactCandidate{{Path: artifactPath, Summary: "evidence", PermissionLabel: "task"}}}); err != nil {
		t.Fatal(err)
	}
	task.Status = "done"
	s := &Server{m: m}
	result, err := s.taskDelegationResult(task)
	if err != nil {
		t.Fatal(err)
	}
	if len(result["facts"].([]map[string]any)) != 1 || len(result["negative_results"].([]map[string]any)) != 1 ||
		len(result["findings"].([]map[string]any)) != 1 || len(result["artifact_refs"].([]map[string]any)) != 1 {
		t.Fatalf("unexpected structured result: %+v", result)
	}
	usage := result["usage"].(map[string]any)
	if usage["input_tokens"] != 10 || usage["output_tokens"] != 2 {
		t.Fatalf("usage mismatch: %+v", usage)
	}
	exceeded := result["budget_exceeded"].([]string)
	if len(exceeded) != 1 || exceeded[0] != "max_input_tokens" {
		t.Fatalf("budget comparison mismatch: %v", exceeded)
	}
}

func TestDelegationBudgetDisabledByDefault(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	task, err := m.CreateTask("unbounded child", "full verification", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.DeleteTask(task.ID)
	task.DelegationBudget = db.DelegationBudget{MaxInputTokens: 10, MaxOutputTokens: 2, MaxToolCalls: 1}
	m.tokenBudgetEnforced = false // 默认：不按 token/工具数硬熔断

	input, output := 9999, 9999
	if _, err := task.Store.AppendActivity(db.Activity{Worker: "work#1", Kind: "result",
		InputTokens: &input, OutputTokens: &output}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(m)
	if engine.enforceDelegationBudget(task) {
		t.Fatal("token budget must NOT stop the task when enforcement is off")
	}
	if status := m.TaskStatus(task.ID); status == "timeout" {
		t.Fatalf("task must not be marked timeout by advisory budget (status=%q)", status)
	}
}

func TestDelegationBudgetEnforcementIsTerminalAndIdempotent(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	task, err := m.CreateTask("budgeted child", "bounded verification", nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.DeleteTask(task.ID)
	task.DelegationBudget = db.DelegationBudget{MaxInputTokens: 10, MaxOutputTokens: 2, MaxToolCalls: 1}
	m.tokenBudgetEnforced = true // 预算硬熔断默认关；测试显式打开以验证熔断路径

	input, output := 10, 2
	if _, err := task.Store.AppendActivity(db.Activity{Worker: "work#1", Kind: "tool_use", Tool: "nmap"}); err != nil {
		t.Fatal(err)
	}
	if _, err := task.Store.AppendActivity(db.Activity{Worker: "work#1", Kind: "result",
		InputTokens: &input, OutputTokens: &output}); err != nil {
		t.Fatal(err)
	}

	engine := NewEngine(m)
	if !engine.enforceDelegationBudget(task) {
		t.Fatal("expected budget enforcement to stop the task")
	}
	if status := m.TaskStatus(task.ID); status != "timeout" {
		t.Fatalf("task status=%q, want timeout", status)
	}
	if !engine.enforceDelegationBudget(task) {
		t.Fatal("terminal budget check should remain enforced")
	}

	events, _, err := task.Store.AgentEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var budgetEvents []db.AgentEvent
	for _, event := range events {
		if event.EventType == db.EventBudgetChanged {
			budgetEvents = append(budgetEvents, event)
		}
	}
	if len(budgetEvents) != 1 {
		t.Fatalf("budget event count=%d, want 1", len(budgetEvents))
	}
	var payload struct {
		Reason   string   `json:"reason"`
		Exceeded []string `json:"exceeded"`
	}
	if err := json.Unmarshal(budgetEvents[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Reason != "delegation_budget_exceeded" || len(payload.Exceeded) != 3 {
		t.Fatalf("unexpected budget payload: %+v", payload)
	}
}

func parseTaskID(id string) int64 {
	var n int64
	for _, r := range id {
		n = n*10 + int64(r-'0')
	}
	return n
}
