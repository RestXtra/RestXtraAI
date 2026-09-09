package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"strconv"
	"time"
)

// auditChainKey is a fixed advisory lock serializing audit writes so the SHA-256
// chain stays strictly linear (soc-autopilot-style tamper-evident audit).
const auditChainKey = 7337741101

// auditTSFmt formats the created_at used in the hash. Truncated to the second so
// the value inserted equals the value scanned back (Postgres TIMESTAMPTZ round-trip).
const auditTSFmt = "2006-01-02T15:04:05Z07:00"

// auditChainHash = sha256(prev_hash | actor | category | action | result | message | ip | ts).
// A tampered field breaks the recomputed hash; a deleted/reordered row breaks the
// prev_hash link.
func auditChainHash(prev, actor, category, action, result, message, ip string, ts time.Time) string {
	h := sha256.New()
	for _, s := range []string{prev, actor, category, action, result, message, ip, ts.UTC().Truncate(time.Second).Format(auditTSFmt)} {
		io.WriteString(h, s)
		io.WriteString(h, "|")
	}
	return hex.EncodeToString(h.Sum(nil))
}

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
	PrevHash  string    `json:"-"`
	Hash      string    `json:"-"`
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

// RecordAudit inserts one audit entry, chained to the previous entry's hash under
// a short advisory lock so concurrent writes cannot branch the chain.
func (d *DB) RecordAudit(e AuditEntry) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	ts := e.CreatedAt.UTC().Truncate(time.Second)
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, auditChainKey); err != nil {
		return err
	}
	var prev string
	_ = tx.QueryRow(`SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prev)
	h := auditChainHash(prev, e.Actor, e.Category, e.Action, e.Result, e.Message, e.IP, ts)
	if _, err := tx.Exec(`
INSERT INTO audit_logs(actor, category, action, result, message, ip, created_at, prev_hash, hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.Actor, e.Category, e.Action, e.Result, e.Message, e.IP, ts, prev, h); err != nil {
		return err
	}
	return tx.Commit()
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

// AuditChainCheck reports the outcome of walking the audit hash chain.
type AuditChainCheck struct {
	Total     int     `json:"total"`
	Legacy    int     `json:"legacy"`     // rows with empty hash (pre-chain, unverifiable)
	Broken    int     `json:"broken"`     // rows whose hash/link failed recomputation
	BrokenIDs []int64 `json:"broken_ids"` // ids of broken rows
}

// VerifyAuditChain walks audit_logs in id order, recomputes each row's SHA-256 and
// checks the prev_hash link, and reports any tampering/breaks.
func (d *DB) VerifyAuditChain() (*AuditChainCheck, error) {
	rows, err := d.Query(`SELECT id, actor, category, action, result, message, ip, created_at, prev_hash, hash FROM audit_logs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	check := &AuditChainCheck{}
	prev := ""
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Actor, &e.Category, &e.Action, &e.Result, &e.Message, &e.IP, &e.CreatedAt, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		check.Total++
		if e.Hash == "" {
			check.Legacy++
			continue
		}
		expected := auditChainHash(prev, e.Actor, e.Category, e.Action, e.Result, e.Message, e.IP, e.CreatedAt)
		if e.PrevHash != prev || e.Hash != expected {
			check.Broken++
			check.BrokenIDs = append(check.BrokenIDs, e.ID)
		}
		prev = e.Hash
	}
	return check, rows.Err()
}
