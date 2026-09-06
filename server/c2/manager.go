// Package c2 implements the RestXtraAI teamserver runtime: beacon HTTP
// listeners (register/poll/result), decoy disguise, basic-auth firewall and
// SOCKS tunnels relayed through live beacons.
package c2

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

func coreLogf(format string, a ...any) {
	log.Printf("[c2] "+format, a...)
}

// TriggerWorkflow is wired by the server package so workflow-targeted auto-tasks
// can hand off to the platform's workflow engine on new-session registration.
// When nil, workflow auto-tasks are logged and skipped.
var TriggerWorkflow func(workflowID int64, listenerID int64, host string) error

// AutoPostex is wired by the server package: fired once when a brand-new beacon
// session registers, so the platform's AI agent can automatically run the
// post-exploitation flow (info → privilege → network → process → escalate/persist
// recon → analysis) against the new host. When nil, auto post-exploitation is
// skipped.
var AutoPostex func(listenerID int64, sessionID, host string)

// OnTaskCompleted is wired by the server package: fired after a beacon reports a
// task result so the platform can auto-record findings ("边渗透边记录").
var OnTaskCompleted func(t *db.C2Task)

// Manager owns the running listeners and tunnels. It is tied to a *server.Server
// lifetime and talks to the shared PostgreSQL via *db.DB.
type Manager struct {
	db *db.DB

	mu        sync.Mutex
	listeners map[int64]*runningListener
	tunnels   map[int64]*runningTunnel
}

type runningListener struct {
	mgr     *Manager
	l       *db.C2Listener
	srv     *http.Server
	ln      net.Listener
	profile *db.C2Profile
	regURI  string
	pollURI string
	resURI  string
}

func New(db *db.DB) *Manager {
	m := &Manager{
		db:        db,
		listeners: map[int64]*runningListener{},
		tunnels:   map[int64]*runningTunnel{},
	}
	if db != nil {
		go m.autoReap()
	}
	return m
}

// autoReap periodically marks beacon sessions with stale heartbeats as lost so
// the client list reflects reality without manual intervention.
func (m *Manager) autoReap() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for range tick.C {
		if n, err := m.db.MarkStaleC2SessionsLost(); err == nil && n > 0 {
			coreLogf("auto-reap: %d stale c2 sessions marked lost", n)
		}
	}
}

// ---------- listeners ----------

func (m *Manager) StartListener(l *db.C2Listener) error {
	if l == nil || l.ID == 0 {
		return fmt.Errorf("invalid listener")
	}
	m.mu.Lock()
	if _, ok := m.listeners[l.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("listener %d already running", l.ID)
	}
	m.mu.Unlock()

	var profile *db.C2Profile
	if l.ProfileID != nil {
		profiles, err := m.db.ListC2Profiles()
		if err == nil {
			for _, p := range profiles {
				if p.ID == *l.ProfileID {
					profile = p
					break
				}
			}
		}
	}

	regURI, pollURI, resURI := defaultURIs()
	if profile != nil {
		if u := profileURIs(profile, "register"); u != "" {
			regURI = u
		}
		if u := profileURIs(profile, "poll"); u != "" {
			pollURI = u
		}
		if u := profileURIs(profile, "result"); u != "" {
			resURI = u
		}
	}

	addr := net.JoinHostPort(l.Host, fmt.Sprintf("%d", l.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	rl := &runningListener{
		mgr:     m,
		l:       l,
		ln:      ln,
		profile: profile,
		regURI:  regURI,
		pollURI: pollURI,
		resURI:  resURI,
	}
	srv := &http.Server{Handler: http.HandlerFunc(rl.ServeHTTP)}
	rl.srv = srv

	m.mu.Lock()
	m.listeners[l.ID] = rl
	m.mu.Unlock()

	go func() {
		if l.Protocol == "https" {
			tc, err := tlsConfigFrom(l.Options)
			if err != nil {
				_ = m.db.SetC2ListenerStatus(l.ID, "error", err.Error())
				return
			}
			tlsLn := tls.NewListener(ln, tc)
			_ = srv.Serve(tlsLn)
			return
		}
		_ = srv.Serve(ln)
	}()
	_ = m.db.SetC2ListenerStatus(l.ID, "running", "")
	return nil
}

func (m *Manager) StopListener(id int64) error {
	m.mu.Lock()
	rl, ok := m.listeners[id]
	if ok {
		delete(m.listeners, id)
	}
	m.mu.Unlock()
	if !ok {
		_ = m.db.SetC2ListenerStatus(id, "stopped", "")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2e9)
	defer cancel()
	_ = rl.srv.Shutdown(ctx)
	_ = rl.ln.Close()
	_ = m.db.SetC2ListenerStatus(id, "stopped", "")
	return nil
}

func (m *Manager) RunningListener(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.listeners[id]
	return ok
}

func defaultURIs() (reg, poll, res string) {
	return "/register", "/poll", "/result"
}

func profileURIs(p *db.C2Profile, block string) string {
	var cfg map[string]any
	if err := json.Unmarshal(p.Config, &cfg); err != nil {
		return ""
	}
	uriObj, ok := cfg[block]
	if !ok {
		return ""
	}
	uris, _ := uriObj.([]any)
	for _, u := range uris {
		if s, ok := u.(string); ok && strings.HasPrefix(s, "/") {
			return s
		}
	}
	return ""
}
