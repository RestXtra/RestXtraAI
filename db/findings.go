package db

import (
	"encoding/json"
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
	CreatedAt       time.Time
	TaskDescription string // populated via LEFT JOIN on tasks
	CompanyIDs      []int64 // 派生：任务企业 + 资产企业并集
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

// ListFindings returns findings (newest first), joined with task description.
// companyID > 0 filters to findings whose task belongs to that company (via
// task_companies) or whose assets belong to it. 0 = all.
func (d *DB) ListFindings(limit int, companyID int64) ([]*DBFinding, error) {
	if limit <= 0 {
		limit = 500
	}
	// company_ids per finding = 任务企业 + 资产企业（并集）。asset_ids 为空时仅用任务企业。
	// 过滤（companyID>0）：命中任务企业或任一资产企业。
	rows, err := d.Query(`
		SELECT f.id, f.task_id, f.node_id, f.vulnclass, f.severity, f.summary,
		       f.evidence, f.worker, f.asset_ids, f.created_at,
		       COALESCE(t.description, '') AS task_description
		FROM findings f
		LEFT JOIN tasks t ON f.task_id = t.id
		WHERE ($1::bigint = 0
		       OR EXISTS (SELECT 1 FROM task_companies tc WHERE tc.task_id = f.task_id AND tc.company_id = $1)
		       OR EXISTS (
		           SELECT 1 FROM unnest(f.asset_ids) AS aid
		           JOIN assets a ON a.id = aid
		           WHERE a.company_id = $1
		       ))
		ORDER BY f.created_at DESC
		LIMIT $2`, companyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DBFinding
	for rows.Next() {
		f := &DBFinding{}
		var aidsJSON string
		if err := rows.Scan(&f.ID, &f.TaskID, &f.NodeID, &f.VulnClass, &f.Severity,
			&f.Summary, &f.Evidence, &f.Worker, &aidsJSON, &f.CreatedAt, &f.TaskDescription); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aidsJSON), &f.AssetIDs)
		out = append(out, f)
	}
	// 派生每条 finding 的企业归属：任务企业 + 资产企业。
	for _, f := range out {
		f.CompanyIDs = d.findingCompanyIDs(f)
	}
	return out, rows.Err()
}

// findingCompanyIDs derives a finding's company set: task companies (from
// task_companies) plus the companies of its assets. Deduplicated.
func (d *DB) findingCompanyIDs(f *DBFinding) []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(id int64) {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if f.TaskID != nil {
		rows, err := d.Query(`SELECT company_id FROM task_companies WHERE task_id=$1`, *f.TaskID)
		if err == nil {
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					add(id)
				}
			}
			rows.Close()
		}
	}
	// 资产企业：逐资产查（findings 数量有限，500 内）。
	for _, aid := range f.AssetIDs {
		var cid int64
		if err := d.QueryRow(`SELECT COALESCE(company_id,0) FROM assets WHERE id=$1`, aid).Scan(&cid); err == nil {
			add(cid)
		}
	}
	return out
}
