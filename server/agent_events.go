package server

import (
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/RestXtra/RestXtraAI/db"
)

func (s *Server) taskOperationsDashboard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid task id")
		return
	}
	task, err := s.m.pg.GetTask(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load task failed")
		return
	}
	if task == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	store := s.m.pg.Exploration(task.ExplorationID)
	baseline, err := s.m.pg.TaskPerformanceBaseline(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load performance baseline failed")
		return
	}
	costs, err := store.RoundCostsByWorker()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load agent costs failed")
		return
	}
	events, err := store.LatestAgentEvents(30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load events failed")
		return
	}
	counts, err := store.AgentEventTypeCounts()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load event counts failed")
		return
	}
	workingSet, err := store.WorkingSet()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load working set failed")
		return
	}
	leases, err := store.ActiveResourceLeases()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load resource leases failed")
		return
	}
	verification, err := store.VerifyGraphProjection()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "verify event projection failed")
		return
	}
	total := db.AgentRoundCost{}
	for _, row := range costs {
		total.Rounds += row.Rounds
		total.ToolCalls += row.ToolCalls
		total.ToolErrors += row.ToolErrors
		total.InputTokens += row.InputTokens
		total.OutputTokens += row.OutputTokens
		total.CacheReadTokens += row.CacheReadTokens
		total.CacheWriteTokens += row.CacheWriteTokens
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": id, "baseline": baseline, "costs": map[string]any{"unit": "tokens", "workers": costs, "total": total},
		"events": events, "event_counts": counts, "working_set": workingSet, "resource_leases": leases, "projection": verification,
	})
}

func (s *Server) taskAgentEvents(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	items, cursor, err := t.Store.AgentEvents(since, atoiDefault(r.URL.Query().Get("limit"), 50))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "cursor": cursor})
}

func (s *Server) taskArtifacts(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	items, err := t.Store.Artifacts(r.URL.Query().Get("q"), after, atoiDefault(r.URL.Query().Get("limit"), 50))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) taskArtifactPage(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	artifactID, err := strconv.ParseInt(r.PathValue("artifactID"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid artifact id")
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	artifact, content, next, err := t.Store.ArtifactPage(artifactID, offset, atoiDefault(r.URL.Query().Get("limit"), 64<<10))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact": artifact, "offset": offset, "next_offset": next,
		"eof": next >= artifact.ByteSize, "data_base64": base64.StdEncoding.EncodeToString(content),
	})
}
