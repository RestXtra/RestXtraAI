package db

import (
	"database/sql"
	"strconv"
	"time"
)

// AuditEntry is one platform audit-log row.
type AuditEntry struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Category  string    `json:"category"`
	Action    string    `json:"action"`
	Result    string    `json:"result"` // success | failure | denied
	Message   string    `json:"message"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditFilter narrows the audit query.
type AuditFilter struct {
	Category string // "" = all
	Action   string // "" = all
	Result   string // "" = all
	Actor    string // "" = all
	Limit    int
	Offset   int
}

// RecordAudit inserts one audit entry.
func (d *DB) RecordAudit(e AuditEntry) error {
	_, err := d.Exec(`
INSERT INTO audit_logs(actor, category, action, result, message, ip)
VALUES ($1, $2, $3, $4, $5, $6)`,
		e.Actor, e.Category, e.Action, e.Result, e.Message, e.IP)
	return err
}

// ListAudit returns audit entries matching the filter, newest first.
func (d *DB) ListAudit(f AuditFilter) ([]AuditEntry, int, error) {
	q := `SELECT id, actor, category, action, result, message, ip, created_at FROM audit_logs`
	where := ""
	args := []any{}
	add := func(cond string, val any) {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += cond
		args = append(args, val)
	}
	if f.Category != "" {
		add("category=$"+strconv.Itoa(len(args)+1), f.Category)
	}
	if f.Action != "" {
		add("action=$"+strconv.Itoa(len(args)+1), f.Action)
	}
	if f.Result != "" {
		add("result=$"+strconv.Itoa(len(args)+1), f.Result)
	}
	if f.Actor != "" {
		add("actor=$"+strconv.Itoa(len(args)+1), f.Actor)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	count := 0
	_ = d.QueryRow(`SELECT count(*) FROM audit_logs`+where, args...).Scan(&count)
	q += where + " ORDER BY id DESC LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Category, &e.Action, &e.Result, &e.Message, &e.IP, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, count, rows.Err()
}

// AuditRetentionDays removes audit rows older than the given retention and
// returns how many were deleted.
func (d *DB) AuditRetentionDays(days int) (int64, error) {
	res, err := d.Exec(`DELETE FROM audit_logs WHERE created_at < now() - make_interval(days => $1)`, days)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ClearAudit empties the entire audit log and returns how many rows were removed.
func (d *DB) ClearAudit() (int64, error) {
	res, err := d.Exec(`DELETE FROM audit_logs`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountAudit returns the total number of audit rows (used by the dashboard).
func (d *DB) CountAudit() (int, error) {
	var n int
	err := d.QueryRow(`SELECT count(*) FROM audit_logs`).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
