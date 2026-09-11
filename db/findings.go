package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DBFinding is a row in the standalone findings table (persists across task deletion).
type DBFinding struct {
	ID              int64
	TaskID          *int64
	NodeID          *int64
	VulnClass       string
	Severity        string
	Summary         string
	Evidence        string
	Worker          string
	AssetIDs        []int64
	Status          string
	Report          string
	RequestRaw      string
	ResponseRaw     string
	CreatedAt       time.Time
	TaskDescription string  // populated via LEFT JOIN on tasks
	CompanyIDs      []int64 // 派生：任务企业 + 资产企业并集
}

const (
	FindingPending       = "pending"
	FindingInProgress    = "in_progress"
	FindingConfirmed     = "confirmed"
	FindingResolved      = "resolved"
	FindingFalsePositive = "false_positive"
	FindingIgnored       = "ignored"
	FindingDuplicate     = "duplicate"
	FindingRiskAccepted  = "risk_accepted"
)

func ValidFindingStatus(status string) bool {
	switch status {
	case FindingPending, FindingInProgress, FindingConfirmed, FindingResolved,
		FindingFalsePositive, FindingIgnored, FindingDuplicate, FindingRiskAccepted:
		return true
	}
	return false
}

// AddFinding inserts a finding into the standalone findings table. taskID and
// nodeID may be 0 (stored as NULL). Returns the new finding id.
func (d *DB) AddFinding(taskID, nodeID int64, vulnclass, severity, summary, evidence, worker string, assetIDs []int64) (int64, error) {
	aidsJSON, _ := json.Marshal(assetIDs)
	if assetIDs == nil {
		aidsJSON = []byte("[]")
	}
	var tid, nid *int64
	if taskID > 0 {
		tid = &taskID
	}
	if nodeID > 0 {
		nid = &nodeID
	}
	var id int64
	err := d.QueryRow(
		`INSERT INTO findings (task_id, node_id, vulnclass, severity, summary, evidence, worker, asset_ids)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		tid, nid, vulnclass, severity, summary, evidence, worker, string(aidsJSON),
	).Scan(&id)
	return id, err
}

const findingSelectCols = `f.id, f.task_id, f.node_id, f.vulnclass, f.severity, f.summary,
	       f.evidence, f.worker, f.asset_ids, COALESCE(f.status, 'pending'), f.created_at,
	       COALESCE(f.report,''), COALESCE(f.request_raw,''), COALESCE(f.response_raw,''),
	       COALESCE(t.description, '') AS task_description,
	       COALESCE((
	         SELECT jsonb_agg(company_id ORDER BY company_id)
	         FROM (
	           SELECT tc.company_id FROM task_companies tc WHERE tc.task_id = f.task_id
	           UNION
	           SELECT a.company_id
	           FROM jsonb_array_elements_text(f.asset_ids) aid(value)
	           JOIN assets a ON a.id = aid.value::bigint
	           WHERE a.company_id IS NOT NULL
	         ) finding_companies
	       ), '[]'::jsonb) AS company_ids`

func scanFindings(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*DBFinding, error) {
	out := make([]*DBFinding, 0)
	for rows.Next() {
		f := &DBFinding{}
		var assetJSON, companyJSON string
		if err := rows.Scan(&f.ID, &f.TaskID, &f.NodeID, &f.VulnClass, &f.Severity,
			&f.Summary, &f.Evidence, &f.Worker, &assetJSON, &f.Status, &f.CreatedAt,
			&f.Report, &f.RequestRaw, &f.ResponseRaw,
			&f.TaskDescription, &companyJSON); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(assetJSON), &f.AssetIDs)
		_ = json.Unmarshal([]byte(companyJSON), &f.CompanyIDs)
		out = append(out, f)
	}
	return out, rows.Err()
}

type FindingFilter struct {
	Severity  string
	Status    string
	VulnClass string
	TaskID    string
	CompanyID int64
	Sort      string
}

type FindingStats struct {
	Total       int      `json:"total"`
	Pending     int      `json:"pending"`
	High        int      `json:"high"`
	Medium      int      `json:"medium"`
	Low         int      `json:"low"`
	Tasks       int      `json:"tasks"`
	VulnClasses []string `json:"vulnclasses"`
}

func (f FindingFilter) where() (string, []any) {
	conds := make([]string, 0, 5)
	args := make([]any, 0, 5)
	addText := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conds = append(conds, fmt.Sprintf("f.%s = $%d", column, len(args)))
	}
	addText("severity", f.Severity)
	addText("status", f.Status)
	addText("vulnclass", f.VulnClass)
	if taskID, err := strconv.ParseInt(f.TaskID, 10, 64); err == nil && taskID > 0 {
		args = append(args, taskID)
		conds = append(conds, fmt.Sprintf("f.task_id = $%d", len(args)))
	}
	if f.CompanyID > 0 {
		args = append(args, f.CompanyID)
		pos := len(args)
		conds = append(conds, fmt.Sprintf(`(EXISTS (
			SELECT 1 FROM task_companies tc WHERE tc.task_id=f.task_id AND tc.company_id=$%d
		) OR EXISTS (
			SELECT 1 FROM jsonb_array_elements_text(f.asset_ids) aid(value)
			JOIN assets a ON a.id=aid.value::bigint WHERE a.company_id=$%d
		))`, pos, pos))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListFindings keeps the existing dashboard/task consumers compatible while
// using the same bounded query and company filter as the paginated endpoint.
func (d *DB) ListFindings(limit int, companyID int64) ([]*DBFinding, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, _, err := d.ListFindingsPage(FindingFilter{CompanyID: companyID}, 1, limit)
	return rows, err
}

func (d *DB) ListFindingsPage(filter FindingFilter, page, pageSize int) ([]*DBFinding, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	where, args := filter.where()
	var total int
	if err := d.QueryRow(`SELECT COUNT(*) FROM findings f`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "f.created_at DESC"
	if filter.Sort == "severity" {
		order = `CASE f.severity WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC, f.created_at DESC`
	}
	pageArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`SELECT %s FROM findings f LEFT JOIN tasks t ON t.id=f.task_id%s
		ORDER BY %s LIMIT $%d OFFSET $%d`, findingSelectCols, where, order, len(args)+1, len(args)+2)
	rows, err := d.Query(query, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	findings, err := scanFindings(rows)
	return findings, total, err
}

func (d *DB) FindingStats(companyID int64) (*FindingStats, error) {
	where, args := (FindingFilter{CompanyID: companyID}).where()
	stats := &FindingStats{VulnClasses: []string{}}
	if err := d.QueryRow(`SELECT COUNT(*),
		COUNT(*) FILTER (WHERE f.status='pending'),
		COUNT(*) FILTER (WHERE f.severity='high'),
		COUNT(*) FILTER (WHERE f.severity='medium'),
		COUNT(*) FILTER (WHERE f.severity='low'),
		COUNT(DISTINCT f.task_id)
		FROM findings f`+where, args...).Scan(
		&stats.Total, &stats.Pending, &stats.High, &stats.Medium, &stats.Low, &stats.Tasks); err != nil {
		return nil, err
	}
	classWhere := " WHERE f.vulnclass <> ''"
	if where != "" {
		classWhere = where + " AND f.vulnclass <> ''"
	}
	rows, err := d.Query(`SELECT DISTINCT f.vulnclass FROM findings f`+classWhere+`
		ORDER BY f.vulnclass`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var vulnClass string
		if err := rows.Scan(&vulnClass); err != nil {
			return nil, err
		}
		stats.VulnClasses = append(stats.VulnClasses, vulnClass)
	}
	return stats, rows.Err()
}

func (d *DB) GetFinding(id int64) (*DBFinding, error) {
	f := &DBFinding{}
	var assetJSON, companyJSON string
	err := d.QueryRow(`SELECT `+findingSelectCols+`, COALESCE(f.report, '')
		FROM findings f LEFT JOIN tasks t ON t.id=f.task_id WHERE f.id=$1`, id).Scan(
		&f.ID, &f.TaskID, &f.NodeID, &f.VulnClass, &f.Severity, &f.Summary,
		&f.Evidence, &f.Worker, &assetJSON, &f.Status, &f.CreatedAt,
		&f.TaskDescription, &companyJSON, &f.Report)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(assetJSON), &f.AssetIDs)
	_ = json.Unmarshal([]byte(companyJSON), &f.CompanyIDs)
	return f, nil
}

func (d *DB) SetFindingStatus(id int64, status string) (int64, error) {
	result, err := d.Exec(`UPDATE findings SET status=$1 WHERE id=$2`, status, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *DB) SetFindingReport(id int64, report string) (int64, error) {
	result, err := d.Exec(`UPDATE findings SET report=$1 WHERE id=$2`, report, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetFindingPOC stores the raw request/response packets (structured PoC) on a
// finding, so the report can render an exact request-packet + response-packet.
func (d *DB) SetFindingPOC(id int64, requestRaw, responseRaw string) error {
	_, err := d.Exec(`UPDATE findings SET request_raw=$2, response_raw=$3 WHERE id=$1`, id, requestRaw, responseRaw)
	return err
}
