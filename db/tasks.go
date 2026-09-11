package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

// MinPlanHeartbeatSeconds 是 planner 心跳间隔下限 = 默认 = 10min。
// db.CreateTask 归一低于 600 一律抬到 600，防止心跳过频空转 planner。
const MinPlanHeartbeatSeconds = 600

// CompanyRef is a lightweight company reference attached to a task (id + name).
type CompanyRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Task is a row in the task registry (1:1 with an exploration).
type Task struct {
	ID            int64  `json:"id"`
	Description   string `json:"description"`
	Goal          string `json:"goal"`
	ExplorationID int64  `json:"exploration_id"`
	Status        string `json:"status"`
	Paused        bool   `json:"paused"`
	LLMProfileID  *int64 `json:"llm_profile_id,omitempty"`
	ParentRef     string `json:"parent_ref,omitempty"` // 父任务 id(编排 spawn 记录;空=顶层)
	AgentKey      string `json:"agent_key,omitempty"`  // 专用 agent 身份（spawn_task 指定）；空=通用 planner/worker
	// ConversationID 记录本任务由哪个会话(chat page)派生，用于把会话级编排归到一棵树；
	// 任务内 spawn 用 ParentRef 关联，会话根层的 spawn 用本字段分组。0=非会话派生。
	ConversationID int64      `json:"conversation_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"` // 进入终态(done/failed/timeout)的时刻;非终态为 nil
	// 任务级超时(见 docs/任务级超时与收尾设计.md)。
	TimeoutSeconds int        `json:"timeout_seconds"`        // 0=不限时
	FirstRunAt     *time.Time `json:"first_run_at,omitempty"` // 首次真正开始运行的时刻(非 created_at);nil=尚未运行
	DeadlineAt     *time.Time `json:"deadline_at,omitempty"`  // = first_run_at + timeout_seconds;nil=不限或未运行
	// PlannerHeartbeatSeconds is the planner's periodic wake-up interval (心跳触发,
	// 0 = disabled). While workers run, the planner wakes on this timer to re-inspect
	// running intents (steer/kill) and decide whether new directions opened up.
	PlanHeartbeatSeconds int `json:"plan_heartbeat_seconds"`
	// 企业归属：主企业(tasks.company_id) + 多企业关联(task_companies)。companies
	// 由 ListTasks/GetTask 填充（nil 时前端按未归属处理）。
	CompanyID int64        `json:"company_id,omitempty"`
	Companies []CompanyRef `json:"companies,omitempty"`
}

// IsTerminal reports whether a task status is a terminal (finished) state.
// 单一真源，替换散落各处的 done/failed 硬编码判定。
func IsTerminal(status string) bool {
	return status == "done" || status == "failed" || status == "timeout"
}

// CreateTask creates an exploration + task in one transaction and returns the task.
// timeoutSeconds is the task-level wall-clock budget (0 = 不限时); deadline_at is
// stamped later at first real run (see engine), not here.
// planHeartbeatSeconds is the planner periodic wake-up interval (0 = disabled).
// companyIDs are the companies the task belongs to (first = primary). Empty = unassigned.
func (d *DB) CreateTask(description, goal string, llmProfileID *int64, timeoutSeconds, planHeartbeatSeconds int, companyIDs []int64) (*Task, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var expID int64
	if err := tx.QueryRow(`INSERT INTO explorations(description, goal) VALUES ($1,$2) RETURNING id`, description, goal).Scan(&expID); err != nil {
		return nil, err
	}
	// origin fact: the exploration graph's root, a KindFact node (state='origin')
	// holding the original task description. Goals/intents/findings descend from
	// it, and the seeded target asset is anchored to it as lineage (the asset graph
	// is global and shared, not isolated per task). Being a fact (not a special 'begin' kind) lets every
	// intent uniformly connect to a fact node, including the first ones.
	originPayload, _ := json.Marshal(map[string]any{
		"summary":     "任务起点：" + description + "；目标：" + goal,
		"description": description,
		"goal":        goal,
	})
	var originID int64
	if err := tx.QueryRow(`
INSERT INTO exploration_nodes(exploration_id, kind, payload, priority, state, origin)
VALUES ($1, 'fact', $2, 0, 'origin', 'system') RETURNING id`, expID, string(originPayload)).Scan(&originID); err != nil {
		return nil, err
	}
	if timeoutSeconds < 0 {
		timeoutSeconds = 0
	}
	if planHeartbeatSeconds < 0 {
		planHeartbeatSeconds = 0
	}
	companyIDs = dedupeIDs(companyIDs)
	var primary *int64
	if len(companyIDs) > 0 {
		primary = &companyIDs[0]
	}
	t := &Task{Description: description, Goal: goal, ExplorationID: expID, LLMProfileID: llmProfileID, TimeoutSeconds: timeoutSeconds, PlanHeartbeatSeconds: planHeartbeatSeconds}
	if err := tx.QueryRow(`
INSERT INTO tasks(description, goal, exploration_id, llm_profile_id, timeout_seconds, plan_heartbeat_seconds, company_id) VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id, status, paused, created_at`, description, goal, expID, llmProfileID, timeoutSeconds, planHeartbeatSeconds, primary).Scan(&t.ID, &t.Status, &t.Paused, &t.CreatedAt); err != nil {
		return nil, err
	}
	if err := writeTaskCompanies(tx, t.ID, companyIDs); err != nil {
		return nil, err
	}
	if err := appendNodeCreatedEvent(tx, expID, originID, KindFact, originPayload, 0, StateOrigin, "system"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	t.CompanyID = derefID(primary)
	t.Companies = d.TaskCompanies(t.ID)
	return t, nil
}

const taskCols = `id, description, goal, exploration_id, status, paused, llm_profile_id, COALESCE(parent_ref,''), COALESCE(agent_key,''), created_at, completed_at, COALESCE(timeout_seconds,0), first_run_at, deadline_at, COALESCE(plan_heartbeat_seconds,0), COALESCE(company_id,0), COALESCE(conversation_id,0)`

func scanTask(sc interface{ Scan(...any) error }) (*Task, error) {
	var t Task
	if err := sc.Scan(&t.ID, &t.Description, &t.Goal, &t.ExplorationID, &t.Status, &t.Paused, &t.LLMProfileID, &t.ParentRef, &t.AgentKey, &t.CreatedAt, &t.CompletedAt, &t.TimeoutSeconds, &t.FirstRunAt, &t.DeadlineAt, &t.PlanHeartbeatSeconds, &t.CompanyID, &t.ConversationID); err != nil {
		return nil, err
	}
	return &t, nil
}

// dedupeIDs removes duplicate ids, preserving order.
func dedupeIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// writeTaskCompanies replaces a task's company association set inside a transaction.
func writeTaskCompanies(q execer, taskID int64, companyIDs []int64) error {
	if _, err := q.Exec(`DELETE FROM task_companies WHERE task_id=$1`, taskID); err != nil {
		return err
	}
	for _, cid := range companyIDs {
		if _, err := q.Exec(`INSERT INTO task_companies(task_id, company_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, taskID, cid); err != nil {
			return err
		}
	}
	return nil
}

// SetTaskCompanies replaces a task's company association set (primary = first).
func (d *DB) SetTaskCompanies(taskID int64, companyIDs []int64) error {
	companyIDs = dedupeIDs(companyIDs)
	var primary *int64
	if len(companyIDs) > 0 {
		primary = &companyIDs[0]
	}
	if _, err := d.Exec(`UPDATE tasks SET company_id=$2 WHERE id=$1`, taskID, primary); err != nil {
		return err
	}
	if err := writeTaskCompanies(d, taskID, companyIDs); err != nil {
		return err
	}
	return nil
}

// TaskCompanies returns the companies associated with a task (id + name).
func (d *DB) TaskCompanies(taskID int64) []CompanyRef {
	rows, err := d.Query(`
SELECT c.id, c.name FROM companies c
JOIN task_companies tc ON tc.company_id = c.id
WHERE tc.task_id=$1 ORDER BY c.id`, taskID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []CompanyRef
	for rows.Next() {
		var r CompanyRef
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return out
		}
		out = append(out, r)
	}
	return out
}

// TasksByCompany returns alive task ids associated with a company.
func (d *DB) TasksByCompany(companyID int64) ([]int64, error) {
	rows, err := d.Query(`
SELECT DISTINCT t.id FROM tasks t
JOIN task_companies tc ON tc.task_id = t.id
WHERE tc.company_id=$1 AND t.deleted_at IS NULL`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// SetParentRef records a task's parent task id (编排 agent spawn_task 关联).
func (d *DB) SetParentRef(id int64, parentRef string) error {
	_, err := d.Exec(`UPDATE tasks SET parent_ref=NULLIF($2,'') WHERE id=$1`, id, parentRef)
	return err
}

// SetTaskAgentKey records the specialized agent a task runs under (空=通用).
func (d *DB) SetTaskAgentKey(id int64, agentKey string) error {
	_, err := d.Exec(`UPDATE tasks SET agent_key=$2 WHERE id=$1`, id, agentKey)
	return err
}

// SetTaskConversation records the conversation (chat session) a task was spawned
// from, so chat-driven orchestrations can be grouped into one tree. 0 clears it.
func (d *DB) SetTaskConversation(id int64, conversationID int64) error {
	_, err := d.Exec(`UPDATE tasks SET conversation_id=NULLIF($2,0) WHERE id=$1`, id, conversationID)
	return err
}

// ListTasks returns alive tasks, newest first.
func (d *DB) ListTasks() ([]*Task, error) {
	rows, err := d.Query(`SELECT ` + taskCols + ` FROM tasks WHERE deleted_at IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	for _, t := range out {
		t.Companies = d.TaskCompanies(t.ID)
	}
	return out, rows.Err()
}

// GetTask returns one alive task (nil if not found/deleted).
func (d *DB) GetTask(id int64) (*Task, error) {
	t, err := scanTask(d.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=$1 AND deleted_at IS NULL`, id))
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	t.Companies = d.TaskCompanies(id)
	return t, nil
}

// SetPaused persists a task's paused flag.
func (d *DB) SetPaused(id int64, paused bool) error {
	_, err := d.Exec(`UPDATE tasks SET paused=$1 WHERE id=$2`, paused, id)
	return err
}

// SetStatus updates a task's lifecycle status. Entering a terminal state
// (done/failed/timeout) stamps completed_at once (COALESCE keeps the first stamp
// stable); moving back to a non-terminal state clears it, so a re-run has no stale
// finish time.
func (d *DB) SetStatus(id int64, status string) error {
	_, err := d.Exec(`
UPDATE tasks
   SET status = $1,
       completed_at = CASE WHEN $1 IN ('done','failed','timeout') THEN COALESCE(completed_at, now()) ELSE NULL END
 WHERE id = $2`, status, id)
	return err
}

// SetTerminalStatusGuarded sets a terminal status only when the task is NOT already
// terminal, so a completed↔timeout race resolves to the first writer (won=true).
// Returns won=false (no error) when another terminal status already stuck — the
// caller then leaves it alone. Non-terminal transitions / re-run still use SetStatus.
func (d *DB) SetTerminalStatusGuarded(id int64, status string) (won bool, err error) {
	res, err := d.Exec(`
UPDATE tasks
   SET status = $1,
       completed_at = COALESCE(completed_at, now())
 WHERE id = $2 AND status NOT IN ('done','failed','timeout')`, status, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// StampFirstRun records a task's first-real-run moment and computes its absolute
// deadline (= now + timeoutSeconds). Idempotent: only stamps when first_run_at is
// still NULL, so restarts / re-entries keep the original clock. timeoutSeconds<=0
// leaves deadline_at NULL (不限时). Returns the resulting deadline (nil = 不限/未变).
func (d *DB) StampFirstRun(id int64, timeoutSeconds int) (*time.Time, error) {
	var deadline *time.Time
	err := d.QueryRow(`
UPDATE tasks
   SET first_run_at = COALESCE(first_run_at, now()),
       deadline_at  = CASE
           WHEN first_run_at IS NOT NULL THEN deadline_at            -- 已盖过章：不动
           WHEN $2 > 0 THEN now() + make_interval(secs => $2)
           ELSE NULL END
 WHERE id = $1
 RETURNING deadline_at`, id, timeoutSeconds).Scan(&deadline)
	return deadline, err
}

// DeleteTask hard-deletes a task and its exploration subgraph (nodes/edges/
// activity via ON DELETE CASCADE). The task row is removed first so the
// ON DELETE RESTRICT on tasks.exploration_id does not block the exploration delete.
// Global assets are NOT touched.
func (d *DB) DeleteTask(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expID int64
	if err := tx.QueryRow(`SELECT exploration_id FROM tasks WHERE id=$1`, id).Scan(&expID); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil // already gone
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM explorations WHERE id=$1`, expID); err != nil {
		return err
	}
	return tx.Commit()
}
