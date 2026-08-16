package db

import "time"

// SandboxHost is a registered Docker daemon host that sandbox containers run on.
type SandboxHost struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Addr        string    `json:"addr"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

func (d *DB) ListSandboxHosts() ([]*SandboxHost, error) {
	rows, err := d.Query(`SELECT id,name,addr,COALESCE(description,''),created_at FROM sandbox_hosts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SandboxHost
	for rows.Next() {
		var h SandboxHost
		if err := rows.Scan(&h.ID, &h.Name, &h.Addr, &h.Description, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &h)
	}
	return out, rows.Err()
}

func (d *DB) GetSandboxHost(id int64) (*SandboxHost, error) {
	var h SandboxHost
	err := d.QueryRow(`SELECT id,name,addr,COALESCE(description,''),created_at FROM sandbox_hosts WHERE id=$1`, id).
		Scan(&h.ID, &h.Name, &h.Addr, &h.Description, &h.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// UpsertSandboxHost inserts (id==0) or updates a Docker host.
func (d *DB) UpsertSandboxHost(h *SandboxHost) (int64, error) {
	if h.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO sandbox_hosts(name,addr,description) VALUES ($1,$2,$3) RETURNING id`,
			h.Name, h.Addr, h.Description).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE sandbox_hosts SET name=$1,addr=$2,description=$3 WHERE id=$4`,
		h.Name, h.Addr, h.Description, h.ID)
	return h.ID, err
}

func (d *DB) DeleteSandboxHost(id int64) error {
	_, err := d.Exec(`DELETE FROM sandbox_hosts WHERE id=$1`, id)
	return err
}

// SandboxEgress is one authorized/denied egress scope rule (CIDR or domain).
type SandboxEgress struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // cidr | domain
	Value     string    `json:"value"`
	Action    string    `json:"action"` // allow | deny
	Note      string    `json:"note"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *DB) ListSandboxEgress() ([]*SandboxEgress, error) {
	rows, err := d.Query(`SELECT id,kind,value,action,COALESCE(note,''),enabled,created_at FROM sandbox_egress ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SandboxEgress
	for rows.Next() {
		var e SandboxEgress
		if err := rows.Scan(&e.ID, &e.Kind, &e.Value, &e.Action, &e.Note, &e.Enabled, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (d *DB) UpsertSandboxEgress(e *SandboxEgress) (int64, error) {
	if e.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO sandbox_egress(kind,value,action,note,enabled) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			e.Kind, e.Value, e.Action, e.Note, e.Enabled).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE sandbox_egress SET kind=$1,value=$2,action=$3,note=$4,enabled=$5 WHERE id=$6`,
		e.Kind, e.Value, e.Action, e.Note, e.Enabled, e.ID)
	return e.ID, err
}

func (d *DB) DeleteSandboxEgress(id int64) error {
	_, err := d.Exec(`DELETE FROM sandbox_egress WHERE id=$1`, id)
	return err
}
