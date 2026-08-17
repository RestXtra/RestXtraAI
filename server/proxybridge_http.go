package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
	"github.com/RestXtra/RestXtraAI/proxybridge"
)

// bridgeStatus returns the current bridge status.
func (s *Server) bridgeStatus(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	st, err := s.proxyBridge.status(pg)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

// bridgeSaveConfig updates bridge config (port / enabled / node / rules).
func (s *Server) bridgeSaveConfig(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Port    int                `json:"port"`
		Enabled bool               `json:"enabled"`
		NodeID  int64              `json:"node_id"`
		Rules   []proxybridge.Rule `json:"rules"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Port <= 0 {
		req.Port = defaultBridgePort
	}
	if req.Port > 65535 {
		writeErr(w, 400, "端口非法")
		return
	}
	if req.NodeID < 0 {
		writeErr(w, 400, "node_id 非法")
		return
	}
	if err := s.proxyBridge.setConfig(pg, req.Port, req.Enabled, req.NodeID, req.Rules); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st, _ := s.proxyBridge.status(pg)
	writeJSON(w, 200, st)
}

// bridgeStart starts the bridge (uses last saved config).
func (s *Server) bridgeStart(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	port := defaultBridgePort
	if v, ok, _ := pg.GetSetting(settingBridgePort); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			port = n
		}
	}
	rules := parseBridgeRules(mustSetting(pg, settingBridgeRules))
	nodeID := int64(0)
	if v, ok, _ := pg.GetSetting(settingBridgeNodeID); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			nodeID = n
		}
	}
	if err := s.proxyBridge.setConfig(pg, port, true, nodeID, rules); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st, _ := s.proxyBridge.status(pg)
	writeJSON(w, 200, st)
}

// bridgeStop stops the bridge.
func (s *Server) bridgeStop(w http.ResponseWriter, r *http.Request) {
	if err := s.proxyBridge.stop(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "running": false})
}

// bridgeImportRules converts a Clash rules list into bridge rules and returns
// them (no persistence — the caller saves via bridgeSaveConfig).
func (s *Server) bridgeImportRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []string `json:"rules"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"rules": parseClashRules(req.Rules)})
}

// ---------- helpers ----------

func mustSetting(pg *db.DB, key string) string {
	v, _, _ := pg.GetSetting(key)
	return v
}
