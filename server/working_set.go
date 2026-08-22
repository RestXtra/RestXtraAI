package server

import (
	"net/http"
	"strconv"
)

func (s *Server) taskWorkingSet(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	item, err := t.Store.WorkingSet()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "working set not created yet")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) taskWorkingSets(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	before, _ := strconv.Atoi(r.URL.Query().Get("before"))
	items, err := t.Store.WorkingSets(before, atoiDefault(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
