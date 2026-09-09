package db

import (
	"time"
)

// ConnAction is one proposed (or executed) connection action behind the HITL
// fence. The agent can only propose; a human approves/rejects, then the command
// is executed server-side (soc-autopilot style: LLM can't mutate, only propose).
type ConnAction struct {
	ID           int64      `json:"id"`
	ConnectionID int64      `json:"connection_id"`
	Kind         string     `json:"kind"`   // exec|contain|collect
	Action       string     `json:"action"` // contain: isolate|block_ip|kill_process
	Target       string     `json:"target"` // contain 的目标(源IP/pid)
	Command      string     `json:"command"`
	Rationale    string     `json:"rationale"`
	State        string     `json:"state"` // pending|approved|rejected|executed|failed
	RequestedBy  string     `json:"requested_by"`
	DecidedBy    string     `json:"decided_by"`
	Result       string     `json:"result"`
	CreatedAt    time.Time  `json:"created_at"`
	DecidedAt    *time.Time `json:"decided_at"`
	ExecutedAt   *time.Time `json:"executed_at"`
}

const connActionCols = `id, connection_id, COALESCE(kind,'exec'), COALESCE(action,''), COALESCE(target,''),
	COALESCE(command,''), COALESCE(rationale,''), COALESCE(state,'pending'),
	COALESCE(requested_by,''), COALESCE(decided_by,''), COALESCE(result,''),
	created_at, decided_at, executed_at`

func scanConnAction(rows interface{ Scan(...any) error }) (*ConnAction, error) {
	var a ConnAction
	if err := rows.Scan(&a.ID, &a.ConnectionID, &a.Kind, &a.Action, &a.Target, &a.Command, &a.Rationale,
		&a.State, &a.RequestedBy, &a.DecidedBy, &a.Result, &a.CreatedAt, &a.DecidedAt, &a.ExecutedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateConnAction submits a proposed dangerous connection action (state=pending).
func (d *DB) CreateConnAction(a *ConnAction) (int64, error) {
	if a.Kind == "" {
		a.Kind = "exec"
	}
	if a.State == "" {
		a.State = "pending"
	}
	var id int64
	err := d.QueryRow(`INSERT INTO conn_actions(connection_id,kind,action,target,command,rationale,state,requested_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		a.ConnectionID, a.Kind, a.Action, a.Target, a.Command, a.Rationale, a.State, a.RequestedBy).Scan(&id)
	return id, err
}

func (d *DB) GetConnAction(id int64) (*ConnAction, error) {
	row := d.QueryRow(`SELECT `+connActionCols+` FROM conn_actions WHERE id=$1`, id)
	a, err := scanConnAction(row)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListConnPendingApprovals returns dangerous connection actions awaiting a human
// decision (state='pending').
func (d *DB) ListConnPendingApprovals() ([]*ConnAction, error) {
	rows, err := d.Query(`SELECT ` + connActionCols + ` FROM conn_actions WHERE state='pending' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ConnAction
	for rows.Next() {
		a, err := scanConnAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListConnActions returns the action history for one connection (newest first).
func (d *DB) ListConnActions(connectionID int64, limit int) ([]*ConnAction, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.Query(`SELECT `+connActionCols+` FROM conn_actions WHERE connection_id=$1 ORDER BY id DESC LIMIT $2`, connectionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ConnAction
	for rows.Next() {
		a, err := scanConnAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetConnActionApproval records a human decision. approved → state stays pending
// until executed; rejected → state=rejected.
func (d *DB) SetConnActionApproval(id int64, approval, decidedBy string) error {
	state := "approved"
	if approval == "rejected" {
		state = "rejected"
	}
	_, err := d.Exec(`UPDATE conn_actions SET state=$2, decided_by=$3, decided_at=now() WHERE id=$1`, id, state, decidedBy)
	return err
}

// SetConnActionResult records the post-execution outcome (executed|failed) + output.
func (d *DB) SetConnActionResult(id int64, state, result string) error {
	_, err := d.Exec(`UPDATE conn_actions SET state=$2, result=$3, executed_at=now() WHERE id=$1`, id, state, result)
	return err
}
