package db

import (
	"database/sql"
	"strconv"
	"time"
)

// Incident is a security incident (IR input). The brief = known alert info +
// scattered ops/dev notes. A responder task (task_id) can be attached to drive
// full-process IR against managed connections.
type Incident struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Severity  string    `json:"severity"` // low|medium|high|critical
	Status    string    `json:"status"`   // new|triaging|contained|resolved|closed_false_positive
	Source    string    `json:"source"`   // 告警来源(SIEM/工单/手工)
	AlertInfo string    `json:"alert_info"`
	Notes     string    `json:"notes"`
	Assets    string    `json:"assets"`
	IOCs      string    `json:"iocs"`
	TaskID    *string   `json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const incidentCols = `id, COALESCE(title,''), COALESCE(severity,'medium'), COALESCE(status,'new'),
	COALESCE(source,''), COALESCE(alert_info,''), COALESCE(notes,''),
	COALESCE(assets,''), COALESCE(iocs,''), task_id, created_at, updated_at`

func scanIncident(rows interface{ Scan(...any) error }) (*Incident, error) {
	var i Incident
	if err := rows.Scan(&i.ID, &i.Title, &i.Severity, &i.Status, &i.Source, &i.AlertInfo,
		&i.Notes, &i.Assets, &i.IOCs, &i.TaskID, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

// CreateIncident inserts a new incident (status=new).
func (d *DB) CreateIncident(i *Incident) (int64, error) {
	if i.Severity == "" {
		i.Severity = "medium"
	}
	if i.Status == "" {
		i.Status = "new"
	}
	var id int64
	err := d.QueryRow(`INSERT INTO incidents(title,severity,status,source,alert_info,notes,assets,iocs)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		i.Title, i.Severity, i.Status, i.Source, i.AlertInfo, i.Notes, i.Assets, i.IOCs).Scan(&id)
	return id, err
}

func (d *DB) GetIncident(id int64) (*Incident, error) {
	row := d.QueryRow(`SELECT `+incidentCols+` FROM incidents WHERE id=$1`, id)
	i, err := scanIncident(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// ListIncidents returns incidents (newest first), optionally filtered by status.
func (d *DB) ListIncidents(status string, limit int) ([]*Incident, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + incidentCols + ` FROM incidents`
	args := []any{}
	if status != "" {
		q += ` WHERE status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args)+1)
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// UpdateIncident updates editable fields (title/severity/status/source/alert_info/notes/assets/iocs).
func (d *DB) UpdateIncident(i *Incident) error {
	_, err := d.Exec(`UPDATE incidents SET title=$1,severity=$2,status=$3,source=$4,
		alert_info=$5,notes=$6,assets=$7,iocs=$8,updated_at=now() WHERE id=$9`,
		i.Title, i.Severity, i.Status, i.Source, i.AlertInfo, i.Notes, i.Assets, i.IOCs, i.ID)
	return err
}

// SetIncidentStatus updates an incident's status (status transition for the IR flow).
func (d *DB) SetIncidentStatus(id int64, status string) error {
	_, err := d.Exec(`UPDATE incidents SET status=$2,updated_at=now() WHERE id=$1`, id, status)
	return err
}

// AttachIncidentTask links a responder task (string id) to an incident.
func (d *DB) AttachIncidentTask(id int64, taskID string) error {
	_, err := d.Exec(`UPDATE incidents SET task_id=$2,updated_at=now() WHERE id=$1`, id, taskID)
	return err
}

func (d *DB) DeleteIncident(id int64) error {
	_, err := d.Exec(`DELETE FROM incidents WHERE id=$1`, id)
	return err
}
