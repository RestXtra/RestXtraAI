package db

import "time"

// WorkflowGraph 是一个持久化的工作流图（graph_json 为 workflow.Graph 序列化）。
type WorkflowGraph struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	GraphJSON   string    `json:"graph_json"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkflowRun 是一次工作流运行记录。
type WorkflowRun struct {
	ID        int64     `json:"id"`
	GraphID   int64     `json:"graph_id"`
	Status    string    `json:"status"` // running|completed|failed|paused
	Inputs    string    `json:"inputs,omitempty"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) CreateWorkflowGraph(name, description, graphJSON string) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO workflow_graphs(name,description,graph_json) VALUES ($1,$2,$3) RETURNING id`,
		name, description, graphJSON).Scan(&id)
	return id, err
}

func (d *DB) ListWorkflowGraphs() ([]*WorkflowGraph, error) {
	rows, err := d.Query(`SELECT id,name,COALESCE(description,''),graph_json,enabled,created_at,updated_at FROM workflow_graphs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkflowGraph
	for rows.Next() {
		var g WorkflowGraph
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.GraphJSON, &g.Enabled, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func (d *DB) GetWorkflowGraph(id int64) (*WorkflowGraph, error) {
	var g WorkflowGraph
	err := d.QueryRow(`SELECT id,name,COALESCE(description,''),graph_json,enabled,created_at,updated_at FROM workflow_graphs WHERE id=$1`, id).
		Scan(&g.ID, &g.Name, &g.Description, &g.GraphJSON, &g.Enabled, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (d *DB) UpdateWorkflowGraph(id int64, name, description, graphJSON string, enabled bool) error {
	_, err := d.Exec(`UPDATE workflow_graphs SET name=$1,description=$2,graph_json=$3,enabled=$4 WHERE id=$5`,
		name, description, graphJSON, enabled, id)
	return err
}

func (d *DB) DeleteWorkflowGraph(id int64) error {
	_, err := d.Exec(`DELETE FROM workflow_graphs WHERE id=$1`, id)
	return err
}

func (d *DB) CreateWorkflowRun(graphID int64, inputs string) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO workflow_runs(graph_id,status,inputs) VALUES ($1,'running',NULLIF($2,'')) RETURNING id`,
		graphID, inputs).Scan(&id)
	return id, err
}

func (d *DB) FinishWorkflowRun(id int64, status, result, errMsg string) error {
	_, err := d.Exec(`UPDATE workflow_runs SET status=$1,result=NULLIF($2,''),error=NULLIF($3,'') WHERE id=$4`,
		status, result, errMsg, id)
	return err
}

func (d *DB) GetWorkflowRun(id int64) (*WorkflowRun, error) {
	var r WorkflowRun
	err := d.QueryRow(`SELECT id,graph_id,status,COALESCE(inputs,''),COALESCE(result,''),COALESCE(error,''),created_at FROM workflow_runs WHERE id=$1`, id).
		Scan(&r.ID, &r.GraphID, &r.Status, &r.Inputs, &r.Result, &r.Error, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (d *DB) ListWorkflowRuns(graphID int64, limit int) ([]*WorkflowRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.Query(`SELECT id,graph_id,status,COALESCE(inputs,''),COALESCE(result,''),COALESCE(error,''),created_at FROM workflow_runs WHERE graph_id=$1 ORDER BY id DESC LIMIT $2`, graphID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkflowRun
	for rows.Next() {
		var r WorkflowRun
		if err := rows.Scan(&r.ID, &r.GraphID, &r.Status, &r.Inputs, &r.Result, &r.Error, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}
