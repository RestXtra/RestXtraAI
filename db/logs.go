package db

import (
	"context"
	"time"
)

// DBLog is one persisted backend log row from the server_logs table.
type DBLog struct {
	ID        int64
	CreatedAt time.Time
	Level     string
	Tag       string
	Text      string
}

// InsertLog appends one log line and returns its auto-assigned id.
func (d *DB) InsertLog(level, tag, text string) (int64, error) {
	var id int64
	err := d.QueryRowContext(context.Background(),
		"INSERT INTO server_logs(level,tag,text) VALUES($1,$2,$3) RETURNING id",
		level, tag, text,
	).Scan(&id)
	return id, err
}

// RecentLogs returns the most recent limit rows, oldest-first.
func (d *DB) RecentLogs(limit int) ([]*DBLog, error) {
	rows, err := d.QueryContext(context.Background(),
		`SELECT id, created_at, level, tag, text
		   FROM server_logs
		  ORDER BY id DESC
		  LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DBLog
	for rows.Next() {
		l := &DBLog{}
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.Level, &l.Tag, &l.Text); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	// Reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ListLogsBefore returns up to limit rows with id < beforeID, oldest-first.
func (d *DB) ListLogsBefore(beforeID int64, limit int) ([]*DBLog, error) {
	rows, err := d.QueryContext(context.Background(),
		`SELECT id, created_at, level, tag, text
		   FROM server_logs
		  WHERE id < $1
		  ORDER BY id DESC
		  LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DBLog
	for rows.Next() {
		l := &DBLog{}
		if err := rows.Scan(&l.ID, &l.CreatedAt, &l.Level, &l.Tag, &l.Text); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	// Reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ClearServerLogs empties the server_logs table and returns rows removed.
func (d *DB) ClearServerLogs() (int64, error) {
	res, err := d.ExecContext(context.Background(), `DELETE FROM server_logs`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteLogs removes a set of server_logs rows by id. Returns rows removed.
func (d *DB) DeleteLogs(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.ExecContext(context.Background(),
		`DELETE FROM server_logs WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
