package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// Batch task queues (ported from Pentest-RestXtra). A queue holds tasks whose
// payload drives the executor; the built-in executor spawns an ARTEX exploration
// task per task (payload {"action":"task","description":...,"goal":...}). A
// background ticker drains enabled queues so pending tasks run without a manual
// trigger, and POST /run forces an immediate drain.

// startBatchScheduler launches the background queue-drain loop.
func (s *Server) startBatchScheduler() {
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-tick.C:
				s.drainBatchQueues(4) // at most 4 tasks per tick (best-effort)
			}
		}
	}()
}

// drainBatchQueues processes up to limit pending tasks across all enabled queues.
func (s *Server) drainBatchQueues(limit int) {
	if s.m.pg == nil {
		return
	}
	queues, err := s.m.pg.ListBatchQueues()
	if err != nil {
		return
	}
	done := 0
	for _, q := range queues {
		if !q.Enabled {
			continue
		}
		for done < limit {
			task, err := s.m.pg.ClaimBatchTask(q.ID)
			if err != nil || task == nil {
				break
			}
			s.executeBatchTask(task)
			done++
		}
		if done >= limit {
			return
		}
	}
}

// executeBatchTask runs one claimed task based on its payload.
func (s *Server) executeBatchTask(t *db.BatchTask) {
	var payload struct {
		Action      string `json:"action"`
		Description string `json:"description"`
		Goal        string `json:"goal"`
	}
	_ = json.Unmarshal(t.Payload, &payload)
	if s.m.pg == nil {
		return
	}
	switch payload.Action {
	case "task":
		desc := payload.Description
		if desc == "" {
			desc = t.Title
		}
		if _, err := s.m.CreateTask(desc, payload.Goal, nil, 0); err != nil {
			_ = s.m.pg.FinishBatchTask(t.ID, "failed", err.Error())
			return
		}
		_ = s.m.pg.FinishBatchTask(t.ID, "completed", "")
	default:
		// no-op executor: mark completed (scaffold)
		_ = s.m.pg.FinishBatchTask(t.ID, "completed", "")
	}
}

// GET /api/batch/queues — list queues (with counts).
func (s *Server) batchListQueues(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	queues, err := pg.ListBatchQueues()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if queues == nil {
		queues = []*db.BatchQueue{}
	}
	writeJSON(w, 200, map[string]any{"queues": queues})
}

// POST /api/batch/queues — create a queue.
func (s *Server) batchCreateQueue(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Cron        string `json:"cron"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeErr(w, 400, "名称不能为空")
		return
	}
	id, err := pg.CreateBatchQueue(req.Name, req.Description, req.Cron)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "batch", "create_queue", "success", "创建批量队列 "+req.Name)
	writeJSON(w, 201, map[string]any{"id": id})
}

// PATCH /api/batch/queues/{id} — update a queue.
func (s *Server) batchUpdateQueue(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Cron        string `json:"cron"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if err := pg.UpdateBatchQueue(id, req.Name, req.Description, req.Cron, req.Enabled); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "batch", "update_queue", "success", "更新批量队列")
	writeJSON(w, 200, map[string]any{"ok": true})
}

// DELETE /api/batch/queues/{id} — delete a queue (tasks cascade).
func (s *Server) batchDeleteQueue(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	if err := pg.DeleteBatchQueue(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.recordAudit(r, "batch", "delete_queue", "success", "删除批量队列")
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// POST /api/batch/queues/{id}/run — drain an enabled queue immediately.
func (s *Server) batchRunQueue(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	q, err := pg.GetBatchQueue(id)
	if err != nil || q == nil {
		writeErr(w, 404, "队列不存在")
		return
	}
	ran := 0
	for {
		task, err := pg.ClaimBatchTask(id)
		if err != nil || task == nil {
			break
		}
		s.executeBatchTask(task)
		ran++
	}
	s.recordAudit(r, "batch", "run_queue", "success", "手动执行批量队列 "+q.Name)
	writeJSON(w, 200, map[string]any{"ran": ran})
}

// GET /api/batch/queues/{id}/tasks — list a queue's tasks.
func (s *Server) batchListTasks(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	tasks, err := pg.ListBatchTasks(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if tasks == nil {
		tasks = []*db.BatchTask{}
	}
	writeJSON(w, 200, map[string]any{"tasks": tasks})
}

// POST /api/batch/queues/{id}/tasks — add a task to a queue.
func (s *Server) batchAddTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	var req struct {
		Title   string          `json:"title"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if len(req.Payload) == 0 {
		req.Payload = json.RawMessage(`{"action":"task","goal":""}`)
	}
	taskID, err := pg.AddBatchTask(id, req.Title, req.Payload)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": taskID})
}

// DELETE /api/batch/tasks/{id} — remove a task.
func (s *Server) batchDeleteTask(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id := pathID(r)
	if _, err := pg.Exec(`DELETE FROM batch_tasks WHERE id=$1`, id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}
