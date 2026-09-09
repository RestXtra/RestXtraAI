package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------- 知识库 ----------

type KnowledgeItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      string    `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (d *DB) ListKnowledge(limit int) ([]*KnowledgeItem, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id,title,content,COALESCE(tags,''),created_at,updated_at FROM knowledge_items ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeItem
	for rows.Next() {
		var k KnowledgeItem
		if err := rows.Scan(&k.ID, &k.Title, &k.Content, &k.Tags, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (d *DB) SearchKnowledge(q string, limit int) ([]*KnowledgeItem, error) {
	if limit <= 0 {
		limit = 10
	}
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	like := "%" + q + "%"
	rows, err := d.Query(`SELECT id,title,content,COALESCE(tags,''),created_at,updated_at FROM knowledge_items
WHERE title ILIKE $1 OR content ILIKE $1 OR tags ILIKE $1 ORDER BY id DESC LIMIT $2`, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeItem
	for rows.Next() {
		var k KnowledgeItem
		if err := rows.Scan(&k.ID, &k.Title, &k.Content, &k.Tags, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (d *DB) SaveKnowledge(k *KnowledgeItem) (int64, error) {
	if k.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO knowledge_items(title,content,tags) VALUES ($1,$2,$3) RETURNING id`,
			k.Title, k.Content, k.Tags).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE knowledge_items SET title=$1,content=$2,tags=$3 WHERE id=$4`,
		k.Title, k.Content, k.Tags, k.ID)
	return k.ID, err
}

func (d *DB) DeleteKnowledge(id int64) error {
	_, err := d.Exec(`DELETE FROM knowledge_items WHERE id=$1`, id)
	return err
}

// ---------- WebShell ----------

type WebshellConn struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Type        string    `json:"type"`
	Password    string    `json:"-"`
	PasswordSet bool      `json:"password_set"`
	Headers     string    `json:"headers"`
	Note        string    `json:"note"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func (d *DB) ListWebshells() ([]*WebshellConn, error) {
	rows, err := d.Query(`SELECT id,name,url,type,COALESCE(password,''),COALESCE(headers,'{}'),COALESCE(note,''),enabled,created_at FROM webshell_conns ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WebshellConn
	for rows.Next() {
		var w WebshellConn
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Type, &w.Password, &w.Headers, &w.Note, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		w.Password, err = d.RevealSecret(w.Password)
		if err != nil {
			return nil, err
		}
		w.PasswordSet = w.Password != ""
		out = append(out, &w)
	}
	return out, rows.Err()
}

func (d *DB) SaveWebshell(w *WebshellConn) (int64, error) {
	password, err := d.ProtectSecret(w.Password)
	if err != nil {
		return 0, err
	}
	if w.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO webshell_conns(name,url,type,password,headers,note,enabled) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			w.Name, w.URL, w.Type, password, w.Headers, w.Note, w.Enabled).Scan(&id)
		return id, err
	}
	_, err = d.Exec(`UPDATE webshell_conns SET name=$1,url=$2,type=$3,password=$4,headers=$5,note=$6,enabled=$7 WHERE id=$8`,
		w.Name, w.URL, w.Type, password, w.Headers, w.Note, w.Enabled, w.ID)
	return w.ID, err
}

func (d *DB) DeleteWebshell(id int64) error {
	_, err := d.Exec(`DELETE FROM webshell_conns WHERE id=$1`, id)
	return err
}

// DeleteWebshells removes a set of webshell connections by id. Returns rows removed.
func (d *DB) DeleteWebshells(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.Exec(`DELETE FROM webshell_conns WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearWebshells removes all webshell connections. Returns rows removed.
func (d *DB) ClearWebshells() (int64, error) {
	res, err := d.Exec(`DELETE FROM webshell_conns`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- Connection（统一连接管理）----------

type Connection struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"` // webshell|ssh|rdp|telnet|agent
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	Username  string          `json:"username"`
	Config    json.RawMessage `json:"config"`
	Secret    string          `json:"-"`
	SecretSet bool            `json:"secret_set"`
	Note      string          `json:"note"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (d *DB) ListConnections(kind string) ([]*Connection, error) {
	q := `SELECT id,name,COALESCE(kind,'webshell'),COALESCE(host,''),COALESCE(port,0),COALESCE(username,''),
		COALESCE(config,'{}'),COALESCE(secret,''),COALESCE(note,''),enabled,created_at,updated_at
		FROM connections`
	args := []interface{}{}
	if kind != "" {
		q += ` WHERE kind=$1`
		args = append(args, kind)
	}
	q += ` ORDER BY id`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Connection
	for rows.Next() {
		var c Connection
		var cfg []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Host, &c.Port, &c.Username, &cfg, &c.Secret, &c.Note, &c.Enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if len(cfg) == 0 {
			cfg = []byte("{}")
		}
		c.Config = cfg
		c.Secret, err = d.RevealSecret(c.Secret)
		if err != nil {
			return nil, err
		}
		c.SecretSet = c.Secret != ""
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (d *DB) GetConnection(id int64) (*Connection, error) {
	var c Connection
	var cfg []byte
	err := d.QueryRow(`SELECT id,name,COALESCE(kind,'webshell'),COALESCE(host,''),COALESCE(port,0),COALESCE(username,''),
		COALESCE(config,'{}'),COALESCE(secret,''),COALESCE(note,''),enabled,created_at,updated_at
		FROM connections WHERE id=$1`, id).
		Scan(&c.ID, &c.Name, &c.Kind, &c.Host, &c.Port, &c.Username, &cfg, &c.Secret, &c.Note, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	c.Config = cfg
	c.Secret, err = d.RevealSecret(c.Secret)
	if err != nil {
		return nil, err
	}
	c.SecretSet = c.Secret != ""
	return &c, nil
}

func (d *DB) SaveConnection(c *Connection) (int64, error) {
	secret, err := d.ProtectSecret(c.Secret)
	if err != nil {
		return 0, err
	}
	cfg := c.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	if c.Kind == "" {
		c.Kind = "webshell"
	}
	if c.ID == 0 {
		var id int64
		err = d.QueryRow(`INSERT INTO connections(name,kind,host,port,username,config,secret,note,enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
			c.Name, c.Kind, c.Host, c.Port, c.Username, cfg, secret, c.Note, c.Enabled).Scan(&id)
		return id, err
	}
	_, err = d.Exec(`UPDATE connections SET name=$1,kind=$2,host=$3,port=$4,username=$5,config=$6,secret=$7,note=$8,enabled=$9,updated_at=now() WHERE id=$10`,
		c.Name, c.Kind, c.Host, c.Port, c.Username, cfg, secret, c.Note, c.Enabled, c.ID)
	return c.ID, err
}

func (d *DB) DeleteConnection(id int64) error {
	_, err := d.Exec(`DELETE FROM connections WHERE id=$1`, id)
	return err
}

func (d *DB) DeleteConnections(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.Exec(`DELETE FROM connections WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) ClearConnections() (int64, error) {
	res, err := d.Exec(`DELETE FROM connections`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- C2 ----------

type C2Listener struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Protocol  string          `json:"protocol"`
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	Enabled   bool            `json:"enabled"`
	Note      string          `json:"note"`
	Status    string          `json:"status"`
	ProfileID *int64          `json:"profile_id"`
	Options   json.RawMessage `json:"options"`
	Disguise  json.RawMessage `json:"disguise"`
	Firewall  json.RawMessage `json:"firewall"`
	Error     string          `json:"error"`
	CreatedAt time.Time       `json:"created_at"`
}

type C2Session struct {
	ID          int64     `json:"id"`
	ListenerID  *int64    `json:"listener_id"`
	SessionID   string    `json:"session_id"`
	Host        string    `json:"host"`
	RemoteIP    string    `json:"remote_ip"`
	Location    string    `json:"location"`
	Hostname    string    `json:"hostname"`
	Username    string    `json:"username"`
	UID         string    `json:"uid"`
	GID         string    `json:"gid"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	PID         int32     `json:"pid"`
	ProcessName string    `json:"process_name"`
	Connection  string    `json:"connection"`
	Note        string    `json:"note"`
	Meta        string    `json:"meta"`
	Status      string    `json:"status"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	CreatedAt   time.Time `json:"created_at"`
}

func (d *DB) ListC2Listeners() ([]*C2Listener, error) {
	rows, err := d.Query(`SELECT id,name,protocol,host,port,enabled,COALESCE(note,''),COALESCE(status,'stopped'),
		profile_id,COALESCE(options,'{}'),COALESCE(disguise,'{}'),COALESCE(firewall,'{}'),COALESCE(error,''),created_at
		FROM c2_listeners ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Listener
	for rows.Next() {
		var l C2Listener
		if err := rows.Scan(&l.ID, &l.Name, &l.Protocol, &l.Host, &l.Port, &l.Enabled, &l.Note, &l.Status,
			&l.ProfileID, &l.Options, &l.Disguise, &l.Firewall, &l.Error, &l.CreatedAt); err != nil {
			return nil, err
		}
		if len(l.Options) == 0 {
			l.Options = []byte("{}")
		}
		if len(l.Disguise) == 0 {
			l.Disguise = []byte("{}")
		}
		if len(l.Firewall) == 0 {
			l.Firewall = []byte("{}")
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Listener(l *C2Listener) (int64, error) {
	opts := l.Options
	if len(opts) == 0 {
		opts = []byte("{}")
	}
	disg := l.Disguise
	if len(disg) == 0 {
		disg = []byte("{}")
	}
	fw := l.Firewall
	if len(fw) == 0 {
		fw = []byte("{}")
	}
	if l.Status == "" {
		l.Status = "stopped"
	}
	if l.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO c2_listeners(name,protocol,host,port,enabled,note,status,profile_id,options,disguise,firewall)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
			l.Name, l.Protocol, l.Host, l.Port, l.Enabled, l.Note, l.Status, l.ProfileID, opts, disg, fw).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE c2_listeners SET name=$1,protocol=$2,host=$3,port=$4,enabled=$5,note=$6,status=$7,profile_id=$8,options=$9,disguise=$10,firewall=$11 WHERE id=$12`,
		l.Name, l.Protocol, l.Host, l.Port, l.Enabled, l.Note, l.Status, l.ProfileID, opts, disg, fw, l.ID)
	return l.ID, err
}

func (d *DB) DeleteC2Listener(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_listeners WHERE id=$1`, id)
	return err
}

func (d *DB) SetC2ListenerStatus(id int64, status, errMsg string) error {
	_, err := d.Exec(`UPDATE c2_listeners SET status=$2,error=$3 WHERE id=$1`, id, status, errMsg)
	return err
}

// DeleteC2Listeners removes a set of C2 listeners by id. Returns rows removed.
func (d *DB) DeleteC2Listeners(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.Exec(`DELETE FROM c2_listeners WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearC2Listeners removes all C2 listeners. Returns rows removed.
func (d *DB) ClearC2Listeners() (int64, error) {
	res, err := d.Exec(`DELETE FROM c2_listeners`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteC2Sessions removes a set of C2 sessions by id. Returns rows removed.
func (d *DB) DeleteC2Sessions(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := d.Exec(`DELETE FROM c2_sessions WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearC2Sessions removes all C2 beacon sessions. Returns rows removed.
func (d *DB) ClearC2Sessions() (int64, error) {
	res, err := d.Exec(`DELETE FROM c2_sessions`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) ListC2Sessions(limit int) ([]*C2Session, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id,listener_id,COALESCE(session_id,''),
		COALESCE(host,''),COALESCE(remote_ip,''),COALESCE(location,''),COALESCE(hostname,''),
		COALESCE(username,''),COALESCE(uid,''),COALESCE(gid,''),COALESCE(os,''),COALESCE(arch,''),
		COALESCE(pid,0),COALESCE(process_name,''),COALESCE(connection,''),COALESCE(note,''),
		COALESCE(meta,''),COALESCE(status,'active'),first_seen,last_seen,created_at
		FROM c2_sessions ORDER BY last_seen DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Session
	for rows.Next() {
		var s C2Session
		if err := rows.Scan(&s.ID, &s.ListenerID, &s.SessionID, &s.Host, &s.RemoteIP, &s.Location, &s.Hostname,
			&s.Username, &s.UID, &s.GID, &s.OS, &s.Arch, &s.PID, &s.ProcessName, &s.Connection, &s.Note,
			&s.Meta, &s.Status, &s.FirstSeen, &s.LastSeen, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UpsertC2Session 以 session_id 为键 upsert（beacon 心跳）。空字段保持原值。
func (d *DB) UpsertC2Session(s *C2Session) error {
	if s.Status == "" {
		s.Status = "active"
	}
	_, err := d.Exec(`INSERT INTO c2_sessions(listener_id,session_id,host,remote_ip,location,hostname,username,uid,gid,os,arch,pid,process_name,connection,note,meta,status,first_seen,last_seen)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now(),now())
ON CONFLICT (session_id) DO UPDATE SET
  listener_id = COALESCE(EXCLUDED.listener_id, c2_sessions.listener_id),
  host        = CASE WHEN EXCLUDED.host='' THEN c2_sessions.host ELSE EXCLUDED.host END,
  remote_ip   = CASE WHEN EXCLUDED.remote_ip='' THEN c2_sessions.remote_ip ELSE EXCLUDED.remote_ip END,
  location    = CASE WHEN EXCLUDED.location='' THEN c2_sessions.location ELSE EXCLUDED.location END,
  hostname    = CASE WHEN EXCLUDED.hostname='' THEN c2_sessions.hostname ELSE EXCLUDED.hostname END,
  username    = CASE WHEN EXCLUDED.username='' THEN c2_sessions.username ELSE EXCLUDED.username END,
  uid         = CASE WHEN EXCLUDED.uid='' THEN c2_sessions.uid ELSE EXCLUDED.uid END,
  gid         = CASE WHEN EXCLUDED.gid='' THEN c2_sessions.gid ELSE EXCLUDED.gid END,
  os          = CASE WHEN EXCLUDED.os='' THEN c2_sessions.os ELSE EXCLUDED.os END,
  arch        = CASE WHEN EXCLUDED.arch='' THEN c2_sessions.arch ELSE EXCLUDED.arch END,
  pid         = CASE WHEN EXCLUDED.pid=0 THEN c2_sessions.pid ELSE EXCLUDED.pid END,
  process_name= CASE WHEN EXCLUDED.process_name='' THEN c2_sessions.process_name ELSE EXCLUDED.process_name END,
  connection  = CASE WHEN EXCLUDED.connection='' THEN c2_sessions.connection ELSE EXCLUDED.connection END,
  note        = CASE WHEN EXCLUDED.note='' THEN c2_sessions.note ELSE EXCLUDED.note END,
  meta        = CASE WHEN EXCLUDED.meta='' THEN c2_sessions.meta ELSE EXCLUDED.meta END,
  status      = CASE WHEN EXCLUDED.status='' THEN c2_sessions.status ELSE EXCLUDED.status END,
  last_seen   = now()`,
		s.ListenerID, s.SessionID, s.Host, s.RemoteIP, s.Location, s.Hostname, s.Username, s.UID, s.GID,
		s.OS, s.Arch, s.PID, s.ProcessName, s.Connection, s.Note, s.Meta, s.Status)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

func (d *DB) SetC2SessionStatus(sessionID, status string) error {
	_, err := d.Exec(`UPDATE c2_sessions SET status=$2 WHERE session_id=$1`, sessionID, status)
	return err
}

// TouchC2Session refreshes last_seen on beacon poll/result and re-activates a
// session that had been auto-marked lost.
func (d *DB) TouchC2Session(sessionID string) error {
	_, err := d.Exec(`UPDATE c2_sessions SET last_seen=now(),status='active' WHERE session_id=$1`, sessionID)
	return err
}

// C2SessionExists reports whether a beacon session is already known.
func (d *DB) C2SessionExists(sessionID string) (bool, error) {
	var ok bool
	err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM c2_sessions WHERE session_id=$1)`, sessionID).Scan(&ok)
	return ok, err
}

// MarkStaleC2SessionsLost marks active sessions whose heartbeat is older than
// the threshold as lost. Returns the number of sessions transitioned.
func (d *DB) MarkStaleC2SessionsLost() (int64, error) {
	res, err := d.Exec(`UPDATE c2_sessions SET status='lost' WHERE status='active' AND last_seen < now() - interval '2 minutes'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) UpdateC2SessionNote(sessionID, note string) error {
	_, err := d.Exec(`UPDATE c2_sessions SET note=$2 WHERE session_id=$1`, sessionID, note)
	return err
}

// ---------- C2 Profiles ----------

type C2Profile struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"created_at"`
}

func (d *DB) ListC2Profiles() ([]*C2Profile, error) {
	rows, err := d.Query(`SELECT id,name,kind,COALESCE(config,'{}'),created_at FROM c2_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Profile
	for rows.Next() {
		var p C2Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.Config, &p.CreatedAt); err != nil {
			return nil, err
		}
		if len(p.Config) == 0 {
			p.Config = []byte("{}")
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Profile(p *C2Profile) (int64, error) {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	if p.Kind == "" {
		p.Kind = "http"
	}
	if p.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO c2_profiles(name,kind,config) VALUES ($1,$2,$3) RETURNING id`,
			p.Name, p.Kind, cfg).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE c2_profiles SET name=$1,kind=$2,config=$3 WHERE id=$4`,
		p.Name, p.Kind, cfg, p.ID)
	return p.ID, err
}

func (d *DB) DeleteC2Profile(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_profiles WHERE id=$1`, id)
	return err
}

// ---------- C2 Tasks ----------

type C2Task struct {
	ID          int64           `json:"id"`
	SessionID   string          `json:"session_id"`
	Command     string          `json:"command"`
	Request     json.RawMessage `json:"request"`
	State       string          `json:"state"`
	Approval    string          `json:"approval"`
	Description string          `json:"description"`
	Response    json.RawMessage `json:"response"`
	CreatedAt   time.Time       `json:"created_at"`
	SentAt      *time.Time      `json:"sent_at"`
	CompletedAt *time.Time      `json:"completed_at"`
}

func (d *DB) ListC2Tasks(sessionID string, limit int) ([]*C2Task, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.Query(`SELECT id,session_id,COALESCE(command,''),COALESCE(request,'{}'),COALESCE(state,'queued'),
		COALESCE(approval,'approved'),COALESCE(description,''),COALESCE(response,'{}'),created_at,sent_at,completed_at
		FROM c2_tasks WHERE session_id=$1 ORDER BY id DESC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Task
	for rows.Next() {
		var t C2Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Command, &t.Request, &t.State, &t.Approval, &t.Description,
			&t.Response, &t.CreatedAt, &t.SentAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		if len(t.Request) == 0 {
			t.Request = []byte("{}")
		}
		if len(t.Response) == 0 {
			t.Response = []byte("{}")
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (d *DB) CreateC2Task(sessionID, command, description string, request json.RawMessage) (int64, error) {
	return d.CreateC2TaskApproval(sessionID, command, description, request, "approved")
}

// CreateC2TaskApproval enqueues a task with an explicit approval state. Tasks
// created with approval='pending' are held until an operator approves/rejects.
func (d *DB) CreateC2TaskApproval(sessionID, command, description string, request json.RawMessage, approval string) (int64, error) {
	req := request
	if len(req) == 0 {
		req = []byte("{}")
	}
	if approval == "" {
		approval = "approved"
	}
	var id int64
	err := d.QueryRow(`INSERT INTO c2_tasks(session_id,command,description,request,state,approval) VALUES ($1,$2,$3,$4,'queued',$5) RETURNING id`,
		sessionID, command, description, req, approval).Scan(&id)
	return id, err
}

func (d *DB) SetC2TaskSent(id int64) error {
	_, err := d.Exec(`UPDATE c2_tasks SET state='sent',sent_at=now() WHERE id=$1`, id)
	return err
}

// ClaimC2Tasks atomically claims the oldest approved+queued tasks for a beacon
// session (state -> sent) and returns them, so the listener can hand them out on
// poll. Pending/rejected tasks are never handed to the beacon.
func (d *DB) ClaimC2Tasks(sessionID string, limit int) ([]*C2Task, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.Query(`UPDATE c2_tasks SET state='sent',sent_at=now() WHERE id IN (
		SELECT id FROM c2_tasks WHERE session_id=$1 AND state='queued' AND approval='approved' ORDER BY id LIMIT $2
	) RETURNING id,session_id,COALESCE(command,''),COALESCE(request,'{}'),COALESCE(state,'sent'),COALESCE(approval,'approved'),COALESCE(description,''),COALESCE(response,'{}'),created_at,sent_at,completed_at`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Task
	for rows.Next() {
		var t C2Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Command, &t.Request, &t.State, &t.Approval, &t.Description, &t.Response, &t.CreatedAt, &t.SentAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		if len(t.Request) == 0 {
			t.Request = []byte("{}")
		}
		if len(t.Response) == 0 {
			t.Response = []byte("{}")
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (d *DB) UpdateC2TaskResult(id int64, state string, response json.RawMessage) error {
	resp := response
	if len(resp) == 0 {
		resp = []byte("{}")
	}
	_, err := d.Exec(`UPDATE c2_tasks SET state=$2,response=$3,completed_at=now() WHERE id=$1`, id, state, resp)
	return err
}

// ListC2PendingApprovals returns tasks awaiting human approval (approval='pending').
func (d *DB) ListC2PendingApprovals() ([]*C2Task, error) {
	rows, err := d.Query(`SELECT id,session_id,COALESCE(command,''),COALESCE(request,'{}'),COALESCE(state,'queued'),
		COALESCE(approval,'pending'),COALESCE(description,''),COALESCE(response,'{}'),created_at,sent_at,completed_at
		FROM c2_tasks WHERE approval='pending' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Task
	for rows.Next() {
		var t C2Task
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Command, &t.Request, &t.State, &t.Approval, &t.Description,
			&t.Response, &t.CreatedAt, &t.SentAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		if len(t.Request) == 0 {
			t.Request = []byte("{}")
		}
		if len(t.Response) == 0 {
			t.Response = []byte("{}")
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

// SetC2TaskApproval transitions a pending task to approved/rejected. Rejecting
// also cancels the task so it is never claimed.
func (d *DB) SetC2TaskApproval(id int64, approval string) error {
	if approval == "rejected" {
		_, err := d.Exec(`UPDATE c2_tasks SET approval=$2,state='failed' WHERE id=$1`, id, approval)
		return err
	}
	_, err := d.Exec(`UPDATE c2_tasks SET approval=$2 WHERE id=$1`, id, approval)
	return err
}

// GetC2TaskByID returns a single task by id.
func (d *DB) GetC2TaskByID(id int64) (*C2Task, error) {
	var t C2Task
	err := d.QueryRow(`SELECT id,session_id,COALESCE(command,''),COALESCE(request,'{}'),COALESCE(state,'queued'),
		COALESCE(approval,'approved'),COALESCE(description,''),COALESCE(response,'{}'),created_at,sent_at,completed_at
		FROM c2_tasks WHERE id=$1`, id).
		Scan(&t.ID, &t.SessionID, &t.Command, &t.Request, &t.State, &t.Approval, &t.Description,
			&t.Response, &t.CreatedAt, &t.SentAt, &t.CompletedAt)
	if err != nil {
		return nil, err
	}
	if len(t.Request) == 0 {
		t.Request = []byte("{}")
	}
	if len(t.Response) == 0 {
		t.Response = []byte("{}")
	}
	return &t, nil
}

func (d *DB) DeleteC2Task(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_tasks WHERE id=$1`, id)
	return err
}

func (d *DB) ClearC2Tasks() (int64, error) {
	res, err := d.Exec(`DELETE FROM c2_tasks`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- C2 Auto Tasks ----------

type C2AutoTask struct {
	ID         int64           `json:"id"`
	ListenerID *int64          `json:"listener_id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	OrderIdx   int             `json:"order_idx"`
	Target     string          `json:"target"`
	WorkflowID *int64          `json:"workflow_id"`
	Commands   json.RawMessage `json:"commands"`
	Conditions json.RawMessage `json:"conditions"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (d *DB) ListC2AutoTasks() ([]*C2AutoTask, error) {
	rows, err := d.Query(`SELECT id,listener_id,COALESCE(name,''),COALESCE(enabled,true),COALESCE(order_idx,0),
		COALESCE(target,'commands'),workflow_id,COALESCE(commands,'[]'),COALESCE(conditions,'{}'),created_at
		FROM c2_auto_tasks ORDER BY order_idx,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2AutoTask
	for rows.Next() {
		var a C2AutoTask
		if err := rows.Scan(&a.ID, &a.ListenerID, &a.Name, &a.Enabled, &a.OrderIdx, &a.Target, &a.WorkflowID,
			&a.Commands, &a.Conditions, &a.CreatedAt); err != nil {
			return nil, err
		}
		if len(a.Commands) == 0 {
			a.Commands = []byte("[]")
		}
		if len(a.Conditions) == 0 {
			a.Conditions = []byte("{}")
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2AutoTask(a *C2AutoTask) (int64, error) {
	cmds := a.Commands
	if len(cmds) == 0 {
		cmds = []byte("[]")
	}
	conds := a.Conditions
	if len(conds) == 0 {
		conds = []byte("{}")
	}
	if a.Target == "" {
		a.Target = "commands"
	}
	if a.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO c2_auto_tasks(listener_id,name,enabled,order_idx,target,workflow_id,commands,conditions)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			a.ListenerID, a.Name, a.Enabled, a.OrderIdx, a.Target, a.WorkflowID, cmds, conds).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE c2_auto_tasks SET listener_id=$1,name=$2,enabled=$3,order_idx=$4,target=$5,workflow_id=$6,commands=$7,conditions=$8 WHERE id=$9`,
		a.ListenerID, a.Name, a.Enabled, a.OrderIdx, a.Target, a.WorkflowID, cmds, conds, a.ID)
	return a.ID, err
}

func (d *DB) DeleteC2AutoTask(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_auto_tasks WHERE id=$1`, id)
	return err
}

// ---------- C2 Plugins ----------

type C2Plugin struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Commands    json.RawMessage `json:"commands"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (d *DB) ListC2Plugins() ([]*C2Plugin, error) {
	rows, err := d.Query(`SELECT id,COALESCE(name,''),COALESCE(description,''),COALESCE(commands,'[]'),created_at FROM c2_plugins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Plugin
	for rows.Next() {
		var p C2Plugin
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Commands, &p.CreatedAt); err != nil {
			return nil, err
		}
		if len(p.Commands) == 0 {
			p.Commands = []byte("[]")
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Plugin(p *C2Plugin) (int64, error) {
	cmds := p.Commands
	if len(cmds) == 0 {
		cmds = []byte("[]")
	}
	if p.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO c2_plugins(name,description,commands) VALUES ($1,$2,$3) RETURNING id`,
			p.Name, p.Description, cmds).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE c2_plugins SET name=$1,description=$2,commands=$3 WHERE id=$4`,
		p.Name, p.Description, cmds, p.ID)
	return p.ID, err
}

func (d *DB) DeleteC2Plugin(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_plugins WHERE id=$1`, id)
	return err
}

// ---------- C2 Generated ----------

type C2Generated struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	ListenerID *int64          `json:"listener_id"`
	OS         string          `json:"os"`
	Arch       string          `json:"arch"`
	Format     string          `json:"format"`
	Config     json.RawMessage `json:"config"`
	Artifact   string          `json:"artifact"`
	Size       int64           `json:"size"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (d *DB) ListC2Generated() ([]*C2Generated, error) {
	rows, err := d.Query(`SELECT id,COALESCE(name,''),listener_id,COALESCE(os,'linux'),COALESCE(arch,'amd64'),
		COALESCE(format,'stageless'),COALESCE(config,'{}'),COALESCE(artifact,''),COALESCE(size,0),created_at FROM c2_generated ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Generated
	for rows.Next() {
		var g C2Generated
		if err := rows.Scan(&g.ID, &g.Name, &g.ListenerID, &g.OS, &g.Arch, &g.Format, &g.Config, &g.Artifact, &g.Size, &g.CreatedAt); err != nil {
			return nil, err
		}
		if len(g.Config) == 0 {
			g.Config = []byte("{}")
		}
		out = append(out, &g)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Generated(g *C2Generated) (int64, error) {
	cfg := g.Config
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	if g.Format == "" {
		g.Format = "stageless"
	}
	var id int64
	err := d.QueryRow(`INSERT INTO c2_generated(name,listener_id,os,arch,format,config,artifact,size) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		g.Name, g.ListenerID, g.OS, g.Arch, g.Format, cfg, g.Artifact, g.Size).Scan(&id)
	return id, err
}

func (d *DB) UpdateC2GeneratedArtifact(id int64, artifact string, size int64) error {
	_, err := d.Exec(`UPDATE c2_generated SET artifact=$2,size=$3 WHERE id=$1`, id, artifact, size)
	return err
}

func (d *DB) GetC2Generated(id int64) (*C2Generated, error) {
	var g C2Generated
	err := d.QueryRow(`SELECT id,COALESCE(name,''),listener_id,COALESCE(os,'linux'),COALESCE(arch,'amd64'),
		COALESCE(format,'stageless'),COALESCE(config,'{}'),COALESCE(artifact,''),COALESCE(size,0),created_at FROM c2_generated WHERE id=$1`, id).
		Scan(&g.ID, &g.Name, &g.ListenerID, &g.OS, &g.Arch, &g.Format, &g.Config, &g.Artifact, &g.Size, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (d *DB) DeleteC2Generated(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_generated WHERE id=$1`, id)
	return err
}

// ---------- C2 Tunnels ----------

type C2Tunnel struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	BindHost  string    `json:"bind_host"`
	BindPort  int       `json:"bind_port"`
	Target    string    `json:"target"`
	State     string    `json:"state"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) ListC2Tunnels() ([]*C2Tunnel, error) {
	rows, err := d.Query(`SELECT id,COALESCE(session_id,''),COALESCE(kind,'socks5'),COALESCE(bind_host,'127.0.0.1'),COALESCE(bind_port,1080),COALESCE(target,''),COALESCE(state,'stopped'),COALESCE(error,''),created_at FROM c2_tunnels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Tunnel
	for rows.Next() {
		var t C2Tunnel
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Kind, &t.BindHost, &t.BindPort, &t.Target, &t.State, &t.Error, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Tunnel(t *C2Tunnel) (int64, error) {
	if t.Kind == "" {
		t.Kind = "socks5"
	}
	if t.BindHost == "" {
		t.BindHost = "127.0.0.1"
	}
	if t.BindPort == 0 {
		t.BindPort = 1080
	}
	if t.State == "" {
		t.State = "stopped"
	}
	var id int64
	err := d.QueryRow(`INSERT INTO c2_tunnels(session_id,kind,bind_host,bind_port,target,state,error) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		t.SessionID, t.Kind, t.BindHost, t.BindPort, t.Target, t.State, t.Error).Scan(&id)
	return id, err
}

func (d *DB) DeleteC2Tunnel(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_tunnels WHERE id=$1`, id)
	return err
}

func (d *DB) SetC2TunnelState(id int64, state, errMsg string) error {
	_, err := d.Exec(`UPDATE c2_tunnels SET state=$2,error=$3 WHERE id=$1`, id, state, errMsg)
	return err
}
