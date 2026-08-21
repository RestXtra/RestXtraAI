package db

import (
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

// ---------- C2 ----------

type C2Listener struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Protocol  string    `json:"protocol"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Enabled   bool      `json:"enabled"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type C2Session struct {
	ID         int64     `json:"id"`
	ListenerID *int64    `json:"listener_id"`
	SessionID  string    `json:"session_id"`
	Host       string    `json:"host"`
	Meta       string    `json:"meta"`
	Status     string    `json:"status"`
	LastSeen   time.Time `json:"last_seen"`
	CreatedAt  time.Time `json:"created_at"`
}

func (d *DB) ListC2Listeners() ([]*C2Listener, error) {
	rows, err := d.Query(`SELECT id,name,protocol,host,port,enabled,COALESCE(note,''),created_at FROM c2_listeners ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Listener
	for rows.Next() {
		var l C2Listener
		if err := rows.Scan(&l.ID, &l.Name, &l.Protocol, &l.Host, &l.Port, &l.Enabled, &l.Note, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (d *DB) SaveC2Listener(l *C2Listener) (int64, error) {
	if l.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO c2_listeners(name,protocol,host,port,enabled,note) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
			l.Name, l.Protocol, l.Host, l.Port, l.Enabled, l.Note).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE c2_listeners SET name=$1,protocol=$2,host=$3,port=$4,enabled=$5,note=$6 WHERE id=$7`,
		l.Name, l.Protocol, l.Host, l.Port, l.Enabled, l.Note, l.ID)
	return l.ID, err
}

func (d *DB) DeleteC2Listener(id int64) error {
	_, err := d.Exec(`DELETE FROM c2_listeners WHERE id=$1`, id)
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
	rows, err := d.Query(`SELECT id,listener_id,COALESCE(session_id,''),COALESCE(host,''),COALESCE(meta,''),COALESCE(status,'active'),last_seen,created_at FROM c2_sessions ORDER BY last_seen DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*C2Session
	for rows.Next() {
		var s C2Session
		if err := rows.Scan(&s.ID, &s.ListenerID, &s.SessionID, &s.Host, &s.Meta, &s.Status, &s.LastSeen, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// UpsertC2Session 以 session_id 为键 upsert（beacon 心跳）。
func (d *DB) UpsertC2Session(s *C2Session) error {
	if s.Status == "" {
		s.Status = "active"
	}
	_, err := d.Exec(`INSERT INTO c2_sessions(listener_id,session_id,host,meta,status,last_seen)
VALUES ($1,$2,$3,$4,$5,now())
ON CONFLICT (session_id) DO UPDATE SET host=$3,meta=$4,status=$5,last_seen=now()`,
		s.ListenerID, s.SessionID, s.Host, s.Meta, s.Status)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

func (d *DB) SetC2SessionStatus(sessionID, status string) error {
	_, err := d.Exec(`UPDATE c2_sessions SET status=$2 WHERE session_id=$1`, sessionID, status)
	return err
}
