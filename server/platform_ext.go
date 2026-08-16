package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// 平台扩展能力：工作空间（文件浏览）、知识库、WebShell、C2。
// WebShell / C2 / 知识库 模块，落地为可用 v1。

// ---------- 工作空间（共享工作目录浏览） ----------

// workspaceRoot 是 agent 写入中间产物的共享目录（dataDir）。
func (s *Server) workspaceRoot() string { return s.m.dir }

// safeWorkspacePath 把相对路径限制在工作空间根内，防目录穿越。
func (s *Server) safeWorkspacePath(rel string) (string, error) {
	root := s.workspaceRoot()
	p := filepath.Join(root, filepath.Clean("/"+rel))
	if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界")
	}
	return p, nil
}

func (s *Server) workspaceList(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	p, err := s.safeWorkspacePath(rel)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, map[string]any{
			"name": e.Name(), "dir": e.IsDir(), "size": size,
			"mod": func() int64 { if info != nil { return info.ModTime().Unix() }; return 0 }(),
		})
	}
	writeJSON(w, 200, map[string]any{"path": filepath.Clean("/" + rel), "entries": out})
}

func (s *Server) workspaceRead(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	p, err := s.safeWorkspacePath(rel)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		writeErr(w, 404, "not a file")
		return
	}
	if fi.Size() > 1<<20 {
		writeErr(w, 413, "文件过大（>1MB）")
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": filepath.Clean("/" + rel), "content": string(b)})
}

// ---------- 知识库 ----------

func (s *Server) knowledgeList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListKnowledge(300)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) knowledgeSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Q     string `json:"q"`
		Limit int    `json:"limit"`
	}
	_ = decode(r, &req)
	items, err := s.m.pg.SearchKnowledge(req.Q, req.Limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) knowledgeSave(w http.ResponseWriter, r *http.Request) {
	var k db.KnowledgeItem
	if err := decode(r, &k); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(k.Title) == "" {
		writeErr(w, 400, "标题必填")
		return
	}
	id, err := s.m.pg.SaveKnowledge(&k)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) knowledgeDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteKnowledge(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// ---------- WebShell ----------

func (s *Server) webshellList(w http.ResponseWriter, r *http.Request) {
	conns, err := s.m.pg.ListWebshells()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"connections": conns})
}

func (s *Server) webshellSave(w http.ResponseWriter, r *http.Request) {
	var c db.WebshellConn
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.URL) == "" {
		writeErr(w, 400, "名称与 URL 必填")
		return
	}
	id, err := s.m.pg.SaveWebshell(&c)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) webshellDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteWebshell(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// webshellTest 通过 HTTP 向 webshell 发一条 ping 命令验证连通性。
func (s *Server) webshellTest(w http.ResponseWriter, r *http.Request) {
	var c db.WebshellConn
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(c.URL) == "" {
		writeErr(w, 400, "URL 必填")
		return
	}
	marker := "RESTXTRA_WS_OK"
	client := &http.Client{Timeout: 15 * time.Second}
	var body io.Reader
	var req *http.Request
	var err error
	u := c.URL
	switch c.Type {
	case "php", "jsp", "aspx", "asp":
		q := url.Values{}
		q.Set("cmd", "echo "+marker)
		if c.Password != "" {
			q.Set("pwd", c.Password)
		}
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		req, err = http.NewRequest(http.MethodGet, u+sep+q.Encode(), nil)
	default: // generic：JSON POST
		payload, _ := json.Marshal(map[string]string{"cmd": "echo " + marker})
		body = strings.NewReader(string(payload))
		req, err = http.NewRequest(http.MethodPost, u, body)
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// 自定义头
	var headers map[string]string
	_ = json.Unmarshal([]byte(c.Headers), &headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	ok := strings.Contains(string(b), marker)
	writeJSON(w, 200, map[string]any{"ok": ok, "status": resp.StatusCode, "snippet": firstLine(string(b), 200)})
}

// ---------- C2 ----------

func (s *Server) c2List(w http.ResponseWriter, r *http.Request) {
	listeners, err := s.m.pg.ListC2Listeners()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	sessions, err := s.m.pg.ListC2Sessions(200)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"listeners": listeners, "sessions": sessions})
}

func (s *Server) c2SaveListener(w http.ResponseWriter, r *http.Request) {
	var l db.C2Listener
	if err := decode(r, &l); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(l.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	id, err := s.m.pg.SaveC2Listener(&l)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) c2DeleteListener(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Listener(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// c2Ingest 是 beacon 心跳入口：upsert 会话。
func (s *Server) c2Ingest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListenerID *int64 `json:"listener_id"`
		SessionID  string `json:"session_id"`
		Host       string `json:"host"`
		Meta       string `json:"meta"`
		Status     string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		writeErr(w, 400, "session_id 必填")
		return
	}
	if err := s.m.pg.UpsertC2Session(&db.C2Session{
		ListenerID: req.ListenerID, SessionID: req.SessionID, Host: req.Host, Meta: req.Meta, Status: req.Status,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) c2SetStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.m.pg.SetC2SessionStatus(req.SessionID, req.Status); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
