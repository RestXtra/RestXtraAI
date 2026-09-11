package db

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// DelegationBudget is the bounded execution budget handed to a child task.
// Wall time is enforced by the task deadline. Token/tool limits are recorded for
// result accounting and policy decisions without pretending the provider can
// always interrupt a partially streamed response exactly at the boundary.
type DelegationBudget struct {
	MaxWallTimeSeconds int `json:"max_wall_time_seconds,omitempty"`
	MaxInputTokens     int `json:"max_input_tokens,omitempty"`
	MaxOutputTokens    int `json:"max_output_tokens,omitempty"`
	MaxToolCalls       int `json:"max_tool_calls,omitempty"`
}

// TaskDelegation is the protocol-level handoff from a parent agent to a child
// task. It carries references and constraints only, never parent transcripts.
type TaskDelegation struct {
	SchemaVersion    int              `json:"schema_version"`
	ChildTaskID      int64            `json:"child_task_id"`
	ParentRef        string           `json:"parent_ref,omitempty"`
	Objective        string           `json:"objective"`
	AgentKey         string           `json:"agent_key,omitempty"` // 专用 agent 身份（空=通用 planner/worker）
	AssetIDs         []int64          `json:"asset_ids,omitempty"`
	RequiredEvidence []string         `json:"required_evidence,omitempty"`
	AllowedTools     []string         `json:"allowed_tools,omitempty"`
	Budget           DelegationBudget `json:"budget"`
	CreatedAt        time.Time        `json:"created_at,omitempty"`
}

func normalizeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

// SaveTaskDelegation persists a bounded child-agent contract. Re-saving is
// idempotent and updates the policy before the child engine is started.
func (d *DB) SaveTaskDelegation(contract TaskDelegation) error {
	contract.SchemaVersion = 1
	contract.Objective = strings.TrimSpace(contract.Objective)
	contract.AssetIDs = dedupeIDs(contract.AssetIDs)
	contract.RequiredEvidence = normalizeStrings(contract.RequiredEvidence)
	contract.AllowedTools = normalizeStrings(contract.AllowedTools)
	if contract.Budget.MaxWallTimeSeconds < 0 {
		contract.Budget.MaxWallTimeSeconds = 0
	}
	if contract.Budget.MaxInputTokens < 0 {
		contract.Budget.MaxInputTokens = 0
	}
	if contract.Budget.MaxOutputTokens < 0 {
		contract.Budget.MaxOutputTokens = 0
	}
	if contract.Budget.MaxToolCalls < 0 {
		contract.Budget.MaxToolCalls = 0
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	_, err = d.Exec(`INSERT INTO task_delegations(child_task_id,parent_ref,contract)
VALUES ($1,NULLIF($2,''),$3)
ON CONFLICT (child_task_id) DO UPDATE SET parent_ref=EXCLUDED.parent_ref, contract=EXCLUDED.contract`,
		contract.ChildTaskID, contract.ParentRef, raw)
	return err
}

// GetTaskDelegation returns nil for ordinary top-level tasks.
func (d *DB) GetTaskDelegation(childTaskID int64) (*TaskDelegation, error) {
	var raw []byte
	var created time.Time
	err := d.QueryRow(`SELECT contract, created_at FROM task_delegations WHERE child_task_id=$1`, childTaskID).Scan(&raw, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var contract TaskDelegation
	if err := json.Unmarshal(raw, &contract); err != nil {
		return nil, err
	}
	contract.ChildTaskID = childTaskID
	contract.CreatedAt = created
	return &contract, nil
}
