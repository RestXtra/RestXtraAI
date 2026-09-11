package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CommandRecord is a paired tool_use + tool_result from the activity table
// (any tool, not just Bash). Command holds the raw tool input (JSON).
type CommandRecord struct {
	ID        int64     `json:"id"`
	ExpID     int64     `json:"exploration_id"`
	Worker    string    `json:"worker"`
	Tool      string    `json:"tool"`
	Command   string    `json:"command"`
	Output    string    `json:"output"`
	IsError   bool      `json:"is_error"`
	CreatedAt time.Time `json:"created_at"`
}

// ListCommands returns tool executions (tool_use + paired tool_result) across all
// explorations, with optional filtering and pagination. Covers every tool, not
// just Bash; q matches the tool name or its input.
func (d *DB) ListCommands(expID *int64, q string, page, size int) ([]CommandRecord, int, error) {
	if size <= 0 {
		size = 50
	}
	if page < 0 {
		page = 0
	}
	offset := page * size

	where := `WHERE u.kind = 'tool_use'`
	args := []any{}
	argN := 1
	// detail lives in agent_events for rows written after the de-duplication;
	// fall back to the activity column for pre-existing rows.
	const fromU = ` FROM activity u LEFT JOIN agent_events ue ON ue.id = u.event_id `
	const uDetail = `COALESCE(NULLIF(u.detail,''), ue.payload->>'detail', '')`

	if expID != nil {
		where += fmt.Sprintf(` AND u.exploration_id = $%d`, argN)
		args = append(args, *expID)
		argN++
	}
	if q != "" {
		where += fmt.Sprintf(` AND (u.tool ILIKE $%d OR `+uDetail+` ILIKE $%d)`, argN, argN)
		args = append(args, "%"+q+"%")
		argN++
	}

	// count
	var total int
	countQ := `SELECT COUNT(*)` + fromU + where
	if err := d.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// data query: join tool_use with its tool_result
	const rDetail = `COALESCE(NULLIF(r.detail,''), re.payload->>'detail', '')`
	dataQ := `
SELECT u.id, u.exploration_id, COALESCE(u.worker,''), COALESCE(u.tool,''), ` + uDetail + `,
       ` + rDetail + `, COALESCE(r.is_error, false), u.created_at
FROM activity u
LEFT JOIN agent_events ue ON ue.id = u.event_id
LEFT JOIN activity r ON r.tool_use_id = u.tool_use_id AND r.kind = 'tool_result'
LEFT JOIN agent_events re ON re.id = r.event_id
` + where + `
ORDER BY u.id DESC
LIMIT $` + fmt.Sprintf("%d", argN) + ` OFFSET $` + fmt.Sprintf("%d", argN+1)

	args = append(args, size, offset)
	rows, err := d.Query(dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []CommandRecord{}
	for rows.Next() {
		var c CommandRecord
		if err := rows.Scan(&c.ID, &c.ExpID, &c.Worker, &c.Tool, &c.Command, &c.Output, &c.IsError, &c.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// LLMRecord is one recorded LLM API call (request + response).
type LLMRecord struct {
	ID           int64     `json:"id"`
	Ts           time.Time `json:"ts"`
	Model        string    `json:"model"`
	ProfileName  string    `json:"profile_name"`
	SessionID    string    `json:"session_id"`
	TaskID       string    `json:"task_id"`
	Worker       string    `json:"worker"`
	LatencyMs    int       `json:"latency_ms"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	CacheRead    int       `json:"cache_read"`
	CacheWrite   int       `json:"cache_write"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
	RequestBody  string    `json:"request_body,omitempty"`
	ResponseBody string    `json:"response_body,omitempty"`
}

const llmRecordsSchema = `
CREATE TABLE IF NOT EXISTS llm_records (
    id            BIGSERIAL PRIMARY KEY,
    ts            TIMESTAMPTZ DEFAULT now(),
    model         TEXT,
    profile_name  TEXT,
    session_id    TEXT,
    task_id       TEXT,
    worker        TEXT,
    latency_ms    INTEGER,
    input_tokens  INTEGER,
    output_tokens INTEGER,
    cache_read    INTEGER,
    cache_write   INTEGER,
    status        TEXT,
    error         TEXT,
    request_body  TEXT,
    response_body TEXT
);
CREATE INDEX IF NOT EXISTS idx_llm_records_ts ON llm_records(ts);
CREATE INDEX IF NOT EXISTS idx_llm_records_session ON llm_records(session_id);
`

// llmRecordsMigrate adds new columns to existing tables.
const llmRecordsMigrate = `
ALTER TABLE llm_records ADD COLUMN IF NOT EXISTS task_id TEXT;
ALTER TABLE llm_records ADD COLUMN IF NOT EXISTS worker TEXT;
ALTER TABLE llm_records ADD COLUMN IF NOT EXISTS profile_name TEXT;
`

// EnsureLLMRecordsTable creates the llm_records table if it does not exist.
func (d *DB) EnsureLLMRecordsTable() error {
	if _, err := d.Exec(llmRecordsSchema); err != nil {
		return err
	}
	_, err := d.Exec(llmRecordsMigrate)
	return err
}

// InsertLLMRecord stores one LLM call record.
func (d *DB) InsertLLMRecord(r *LLMRecord) error {
	_, err := d.Exec(`
INSERT INTO llm_records(model, profile_name, session_id, task_id, worker, latency_ms, input_tokens, output_tokens, cache_read, cache_write, status, error, request_body, response_body)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		r.Model, nullIfEmpty(r.ProfileName), r.SessionID, nullIfEmpty(r.TaskID), nullIfEmpty(r.Worker),
		r.LatencyMs, r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheWrite,
		r.Status, nullIfEmpty(r.Error), nullIfEmpty(r.RequestBody), nullIfEmpty(r.ResponseBody))
	return err
}

// ListLLMRecords returns paginated LLM records with optional filters.
func (d *DB) ListLLMRecords(model, session string, page, size int) ([]LLMRecord, int, error) {
	if size <= 0 {
		size = 50
	}
	if page < 0 {
		page = 0
	}
	offset := page * size

	where := `WHERE true`
	args := []any{}
	argN := 1
	if model != "" {
		where += fmt.Sprintf(` AND model = $%d`, argN)
		args = append(args, model)
		argN++
	}
	if session != "" {
		where += fmt.Sprintf(` AND session_id ILIKE $%d`, argN)
		args = append(args, "%"+session+"%")
		argN++
	}

	var total int
	if err := d.QueryRow(`SELECT COUNT(*) FROM llm_records `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataQ := `SELECT id, ts, COALESCE(model,''), COALESCE(profile_name,''), COALESCE(session_id,''), COALESCE(task_id,''), COALESCE(worker,''),
		COALESCE(latency_ms,0), COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cache_read,0), COALESCE(cache_write,0),
		COALESCE(status,''), COALESCE(error,'')
		FROM llm_records ` + where + ` ORDER BY id DESC LIMIT $` + fmt.Sprintf("%d", argN) + ` OFFSET $` + fmt.Sprintf("%d", argN+1)
	args = append(args, size, offset)

	rows, err := d.Query(dataQ, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []LLMRecord{}
	for rows.Next() {
		var r LLMRecord
		if err := rows.Scan(&r.ID, &r.Ts, &r.Model, &r.ProfileName, &r.SessionID, &r.TaskID, &r.Worker, &r.LatencyMs,
			&r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Status, &r.Error); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// GetLLMRecord returns a single LLM record with full request/response bodies.
func (d *DB) GetLLMRecord(id int64) (*LLMRecord, error) {
	var r LLMRecord
	var reqBody, respBody sql.NullString
	err := d.QueryRow(`SELECT id, ts, COALESCE(model,''), COALESCE(profile_name,''), COALESCE(session_id,''), COALESCE(task_id,''), COALESCE(worker,''),
		COALESCE(latency_ms,0), COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cache_read,0), COALESCE(cache_write,0),
		COALESCE(status,''), COALESCE(error,''), request_body, response_body
		FROM llm_records WHERE id=$1`, id).
		Scan(&r.ID, &r.Ts, &r.Model, &r.ProfileName, &r.SessionID, &r.TaskID, &r.Worker, &r.LatencyMs,
			&r.InputTokens, &r.OutputTokens, &r.CacheRead, &r.CacheWrite, &r.Status, &r.Error,
			&reqBody, &respBody)
	if err != nil {
		return nil, err
	}
	r.RequestBody = reqBody.String
	r.ResponseBody = respBody.String
	return &r, nil
}

// DeleteLLMRecord removes one LLM record.
func (d *DB) DeleteLLMRecord(id int64) error {
	_, err := d.Exec(`DELETE FROM llm_records WHERE id=$1`, id)
	return err
}

// DeleteLLMRecords removes a set of LLM records by id. Returns rows removed.
func (d *DB) DeleteLLMRecords(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.Exec(`DELETE FROM llm_records WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteCommands removes a set of tool-execution records (activity tool_use rows
// plus their paired tool_result rows). Returns rows removed.
func (d *DB) DeleteCommands(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// collect tool_use_ids for the requested tool_use rows so the paired
	// tool_result rows can be removed together.
	rows, err := d.Query(`SELECT tool_use_id FROM activity WHERE id IN (`+idList(ids)+`) AND kind='tool_use' AND tool_use_id IS NOT NULL`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	var toolUseIDs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return 0, err
		}
		toolUseIDs = append(toolUseIDs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	removed := int64(0)
	if len(toolUseIDs) > 0 {
		if res, err := d.Exec(`DELETE FROM activity WHERE tool_use_id IN (`+idListStr(toolUseIDs)+`) AND kind='tool_result'`, strsToArgs(toolUseIDs)...); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				removed += n
			}
		}
	}
	res, err := d.Exec(`DELETE FROM activity WHERE id IN (`+idList(ids)+`) AND kind='tool_use'`, idsToArgs(ids)...)
	if err != nil {
		return removed, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		removed += n
	}
	return removed, nil
}

// ClearCommands removes all tool-execution records (tool_use + paired tool_result).
// Returns rows removed.
func (d *DB) ClearCommands() (int64, error) {
	res, err := d.Exec(`DELETE FROM activity WHERE kind IN ('tool_use','tool_result')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearLLMRecords empties the entire LLM record table and returns rows removed.
func (d *DB) ClearLLMRecords() (int64, error) {
	res, err := d.Exec(`DELETE FROM llm_records`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// idList builds "($1,$2,…)" style placeholder text for an IN clause.
func idList(ids []int64) string {
	parts := make([]string, len(ids))
	for i := range ids {
		parts[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(parts, ",")
}

// idsToArgs converts int64 ids into a []any for query args.
func idsToArgs(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// idListStr builds "($1,$2,…)" placeholder text for a string IN clause.
func idListStr(ids []string) string {
	parts := make([]string, len(ids))
	for i := range ids {
		parts[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(parts, ",")
}

// strsToArgs converts string ids into a []any for query args.
func strsToArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
