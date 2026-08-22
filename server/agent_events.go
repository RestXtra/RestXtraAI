package server

import (
	"encoding/base64"
	"net/http"
	"strconv"
)

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
