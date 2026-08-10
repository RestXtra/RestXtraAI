package db

import (
	"database/sql"
	"encoding/json"
	"time"
)

// BatchQueue is a named batch-task queue (ported from Pentest-RestXtra).
type BatchQueue struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Cron        string    `json:"cron"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	TaskCount   int       `json:"task_count,omitempty"`
}

// BatchTask is one unit of work inside a queue. Payload drives the executor
// (e.g. {"action":"task","description":...,"goal":...} spawns an exploration task).
type BatchTask struct {
	ID         int64           `json:"id"`
	QueueID    int64           `json:"queue_id"`
	Title      string          `json:"title"`
	Payload    json.RawMessage `json:"payload"`
	Status     string          `json:"status"` // pending|running|completed|failed|cancelled
	Attempts   int             `json:"attempts"`
	Error      string          `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}

// CreateBatchQueue inserts a queue and returns its id.
func (d *DB) CreateBatchQueue(name, description, cron string) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO batch_queues(name, description, cron) VALUES ($1,$2,$3) RETURNING id`,
		name, description, cron).Scan(&id)
	return id, err
}

// ListBatchQueues returns all queues with their pending/total task counts.
func (d *DB) ListBatchQueues() ([]*BatchQueue, error) {
	rows, err := d.Query(`
SELECT q.id, q.name, q.description, q.cron, q.enabled, q.created_at, q.updated_at,
       (SELECT count(*) FROM batch_tasks t WHERE t.queue_id = q.id) AS task_count
FROM batch_queues q ORDER BY q.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BatchQueue
	for rows.Next() {
		var q BatchQueue
		if err := rows.Scan(&q.ID, &q.Name, &q.Description, &q.Cron, &q.Enabled, &q.CreatedAt, &q.UpdatedAt, &q.TaskCount); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

// GetBatchQueue returns one queue.
func (d *DB) GetBatchQueue(id int64) (*BatchQueue, error) {
	q := &BatchQueue{}
	err := d.QueryRow(`SELECT id, name, description, cron, enabled, created_at, updated_at FROM batch_queues WHERE id=$1`, id).
		Scan(&q.ID, &q.Name, &q.Description, &q.Cron, &q.Enabled, &q.CreatedAt, &q.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

// UpdateBatchQueue patches name/description/cron/enabled.
func (d *DB) UpdateBatchQueue(id int64, name, description, cron string, enabled *bool) error {
	q, err := d.GetBatchQueue(id)
	if err != nil || q == nil {
		if err == nil {
			err = sql.ErrNoRows
		}
		return err
	}
	if name != "" {
		q.Name = name
	}
	if description != "" {
		q.Description = description
	}
	if cron != "" {
		q.Cron = cron
	}
	if enabled != nil {
		q.Enabled = *enabled
	}
	_, err = d.Exec(`UPDATE batch_queues SET name=$1, description=$2, cron=$3, enabled=$4 WHERE id=$5`,
		q.Name, q.Description, q.Cron, q.Enabled, id)
	return err
}

// DeleteBatchQueue removes a queue (tasks cascade).
func (d *DB) DeleteBatchQueue(id int64) error {
	_, err := d.Exec(`DELETE FROM batch_queues WHERE id=$1`, id)
	return err
}

// AddBatchTask appends a task to a queue.
func (d *DB) AddBatchTask(queueID int64, title string, payload json.RawMessage) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO batch_tasks(queue_id, title, payload) VALUES ($1,$2,$3) RETURNING id`,
		queueID, title, payload).Scan(&id)
	return id, err
}

// ListBatchTasks returns a queue's tasks ordered by id.
func (d *DB) ListBatchTasks(queueID int64) ([]*BatchTask, error) {
	rows, err := d.Query(`
SELECT id, queue_id, title, payload, status, attempts, error, created_at, started_at, finished_at
FROM batch_tasks WHERE queue_id=$1 ORDER BY id`, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BatchTask
	for rows.Next() {
		t := &BatchTask{}
		if err := rows.Scan(&t.ID, &t.QueueID, &t.Title, &t.Payload, &t.Status, &t.Attempts, &t.Error, &t.CreatedAt, &t.StartedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimBatchTask atomically claims the oldest pending task from a queue.
func (d *DB) ClaimBatchTask(queueID int64) (*BatchTask, error) {
	t := &BatchTask{}
	err := d.QueryRow(`
UPDATE batch_tasks SET status='running', attempts=attempts+1, started_at=now(), error=''
WHERE id = (SELECT id FROM batch_tasks WHERE queue_id=$1 AND status='pending' ORDER BY id LIMIT 1)
RETURNING id, queue_id, title, payload, status, attempts, error, created_at, started_at, finished_at`, queueID).
		Scan(&t.ID, &t.QueueID, &t.Title, &t.Payload, &t.Status, &t.Attempts, &t.Error, &t.CreatedAt, &t.StartedAt, &t.FinishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// FinishBatchTask marks a task completed/failed and records the error.
func (d *DB) FinishBatchTask(id int64, status, errMsg string) error {
	_, err := d.Exec(`UPDATE batch_tasks SET status=$1, error=$2, finished_at=now() WHERE id=$3`, status, errMsg, id)
	return err
}

// ResetBatchTask puts a task back to pending (for retry).
func (d *DB) ResetBatchTask(id int64) error {
	_, err := d.Exec(`UPDATE batch_tasks SET status='pending', started_at=NULL, finished_at=NULL, error='' WHERE id=$1`, id)
	return err
}
