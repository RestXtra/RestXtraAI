package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// utf8Clean makes a string safe for a PostgreSQL text column. It (1) replaces
// invalid UTF-8 byte sequences with U+FFFD and (2) strips NUL (0x00) bytes.
// Tool output (nmap/curl/… stdout) can carry raw/truncated bytes that PostgreSQL's
// UTF8 encoding rejects ("invalid byte sequence for encoding UTF8"); without this
// the INSERT fails and the activity record is silently lost. NUL is *valid* UTF-8
// (U+0000) so ToValidUTF8 leaves it in place, yet PostgreSQL text still rejects it
// (SQLSTATE 22021) — so it must be removed separately. JSONB payloads are fine
// (json.Marshal already sanitizes), so only the plain text columns need it.
// utf8Clean sanitizes a string for storage: removes NUL bytes, fixes invalid
// UTF-8, and replaces lone surrogates (U+D800–DFFF) with U+FFFD. ToValidUTF8
// fixes malformed byte sequences but NOT lone surrogates — Go's json.Marshal
// emits them as \ud800 and Postgres jsonb rejects that ("unsupported Unicode
// escape sequence", SQLSTATE 22P05). Tools returning raw bytes (curl/pty/
// terminal output) can carry such code points, so every field that lands in a
// JSONB column or text column must pass through here.
func utf8Clean(s string) string {
	if strings.IndexByte(s, 0) >= 0 {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	s = strings.ToValidUTF8(s, "\uFFFD")
	// fast path: surrogates (U+D800–DFFF) encode to bytes starting 0xED; absent
	// → nothing to do.
	if strings.IndexByte(s, 0xED) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0xD800 && r <= 0xDFFF {
			b.WriteRune('\uFFFD')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Node is a typed reasoning node (= old task_nodes). kind ∈ goal|intent|finding|hint.
type Node struct {
	ID             int64           `json:"id"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	Priority       int             `json:"priority"`
	State          string          `json:"state"`
	Origin         string          `json:"origin,omitempty"`
	Owner          string          `json:"owner,omitempty"`
	LeaseExpiresAt *time.Time      `json:"lease_expires_at,omitempty"`
	AttemptCount   int             `json:"attempt_count"`
	LastLeaseAt    *time.Time      `json:"last_lease_at,omitempty"`
	Anchors        []int64         `json:"anchors,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Activity is one worker execution step (= old task_activity).
type Activity struct {
	ID        int64     `json:"id"`
	NodeID    *int64    `json:"node_id,omitempty"`
	Worker    string    `json:"worker,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	Tool      string    `json:"tool,omitempty"`
	ToolUseID string    `json:"tool_use_id,omitempty"`
	IsError   bool      `json:"is_error"`
	Summary   string    `json:"summary,omitempty"`
	Detail    string    `json:"-"` // full blob; lazy-loaded, omitted from list payloads
	CreatedAt time.Time `json:"created_at"`
	// Token usage — set only on the terminal kind='result' record; nil elsewhere.
	InputTokens      *int `json:"input_tokens,omitempty"`
	OutputTokens     *int `json:"output_tokens,omitempty"`
	CacheReadTokens  *int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// Canonical event metadata. EventOnly records lifecycle facts without adding
	// a compatibility activity row (and therefore without changing the current UI).
	EventType string              `json:"-"`
	EventKey  string              `json:"-"`
	TurnID    string              `json:"-"`
	Payload   json.RawMessage     `json:"-"`
	EventOnly bool                `json:"-"`
	Artifacts []ArtifactCandidate `json:"-"`
}

// TokenUsage is a per-worker token aggregate (TokenStatsByWorker).
type TokenUsage struct {
	Worker           string `json:"worker"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
}

// AgentRoundCost is the auditable, token-based execution cost of one agent in
// an exploration. Monetary values are intentionally absent: profiles do not
// yet carry a versioned pricing schedule, so exposing currency would be false
// precision. A round is one terminal kind='result' activity.
type AgentRoundCost struct {
	Worker           string `json:"worker"`
	Rounds           int    `json:"rounds"`
	ToolCalls        int    `json:"tool_calls"`
	ToolErrors       int    `json:"tool_errors"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
}

// DailyTokenBucket is one day's global token aggregate across all tasks.
type DailyTokenBucket struct {
	Day             string `json:"day"` // "YYYY-MM-DD"
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	CacheReadTokens int    `json:"cache_read_tokens"`
}

// TokenDailyAll aggregates token consumption by calendar day (UTC) for the
// past `days` days across all explorations. Only kind='result' rows carry token
// counts (the terminal per-run summary), so there is no double-counting.
func (d *DB) TokenDailyAll(days int) ([]DailyTokenBucket, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := d.Query(`
		SELECT TO_CHAR(DATE_TRUNC('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD') AS day,
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cache_read_tokens), 0)
		FROM activity
		WHERE kind = 'result'
		  AND created_at >= NOW() - ($1 * INTERVAL '1 day')
		GROUP BY day
		ORDER BY day`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyTokenBucket
	for rows.Next() {
		var b DailyTokenBucket
		if err := rows.Scan(&b.Day, &b.InputTokens, &b.OutputTokens, &b.CacheReadTokens); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ExplorationStore is a handle onto one exploration's reasoning graph + activity.
type ExplorationStore struct {
	db    *DB
	expID int64
}

// CreateExploration creates a new exploration root and returns its id.
func (d *DB) CreateExploration(description, goal string) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO explorations(description, goal) VALUES ($1, $2) RETURNING id`, description, goal).Scan(&id)
	return id, err
}

// Exploration returns a handle bound to an exploration id.
func (d *DB) Exploration(id int64) *ExplorationStore { return &ExplorationStore{db: d, expID: id} }

func (s *ExplorationStore) ID() int64 { return s.expID }

// BumpVersion marks this exploration's graph as changed (P2.6). Called by every
// write that graph_overview reflects; invalidates the cached overview snapshot.
func (s *ExplorationStore) BumpVersion() {
	l := s.db.ovLock(s.expID)
	l.Lock()
	defer l.Unlock()
	if s.db.ovVer == nil {
		s.db.ovVer = map[int64]int64{}
	}
	s.db.ovVer[s.expID]++
}

// CachedOverview returns the cached overview JSON when the graph version is
// unchanged since it was computed. (b, ok); ok=false → caller recomputes.
func (s *ExplorationStore) CachedOverview() ([]byte, bool) {
	l := s.db.ovLock(s.expID)
	l.Lock()
	defer l.Unlock()
	c, ok := s.db.ovCache[s.expID]
	if !ok {
		return nil, false
	}
	if c.ver != s.db.ovVer[s.expID] {
		return nil, false
	}
	return c.data, true
}

// CacheOverview stores a fresh overview snapshot at the current graph version.
func (s *ExplorationStore) CacheOverview(data map[string]any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	l := s.db.ovLock(s.expID)
	l.Lock()
	defer l.Unlock()
	if s.db.ovCache == nil {
		s.db.ovCache = map[int64]*overviewCache{}
	}
	s.db.ovCache[s.expID] = &overviewCache{ver: s.db.ovVer[s.expID], data: b}
}

// Root returns the exploration's description and goal.
func (s *ExplorationStore) Root() (description, goal string, err error) {
	var d sql.NullString
	err = s.db.QueryRow(`SELECT description, goal FROM explorations WHERE id=$1`, s.expID).Scan(&d, &goal)
	return d.String, goal, err
}

// AddNode writes a typed reasoning node with optional anchors to asset ids.
func (s *ExplorationStore) AddNode(kind string, payload map[string]any, priority int, state, origin string, anchors []int64) (int64, error) {
	raw, _ := json.Marshal(payload)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`
INSERT INTO exploration_nodes(exploration_id, kind, payload, priority, state, origin)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		s.expID, kind, string(raw), priority, state, origin).Scan(&id); err != nil {
		return 0, err
	}
	insertedAnchors := make([]int64, 0, len(anchors))
	for _, a := range anchors {
		res, err := tx.Exec(`INSERT INTO exploration_anchors(node_id, asset_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, a)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			insertedAnchors = append(insertedAnchors, a)
		}
	}
	if err := appendNodeCreatedEvent(tx, s.expID, id, kind, raw, priority, state, origin); err != nil {
		return 0, err
	}
	for _, a := range insertedAnchors {
		if err := appendAnchorCreatedEvent(tx, s.expID, id, a); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.BumpVersion()
	return id, nil
}

// AddIntent is a convenience: an open intent.
func (s *ExplorationStore) AddIntent(payload map[string]any, priority int, anchors []int64, origin string) (int64, error) {
	if origin == "" {
		origin = "planner"
	}
	stored := make(map[string]any, len(payload)+2)
	for key, value := range payload {
		stored[key] = value
	}
	stored["concurrency_class"] = intentConcurrencyClass(stored)
	stored["resource_claims"] = normalizeResourceClaims(stored, anchors)
	return s.AddNode("intent", stored, priority, "open", origin, anchors)
}

// AddGoal writes a goal node (state open).
func (s *ExplorationStore) AddGoal(payload map[string]any, origin string) (int64, error) {
	return s.AddNode("goal", payload, 0, "open", origin, nil)
}

// OriginFactID returns this exploration's root fact id — the KindFact node with
// state='origin' seeded at task creation (the task root). Every intent traces
// back to it. Returns 0 (no error) when none exists (legacy explorations created
// under the old 'begin' root, or none yet).
func (s *ExplorationStore) OriginFactID() (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM exploration_nodes WHERE exploration_id=$1 AND kind='fact' AND state='origin' ORDER BY id LIMIT 1`, s.expID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// Anchor records a node→asset reference edge (many-to-many): it captures which
// global assets an exploration node touched, as lineage/provenance. The asset
// graph itself is global and shared — anchoring no longer gates visibility (any
// task may read any asset via search); it only preserves the reasoning trail.
// Idempotent.
func (s *ExplorationStore) Anchor(nodeID, assetID int64) error {
	if nodeID <= 0 || assetID <= 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO exploration_anchors(node_id, asset_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, nodeID, assetID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if err := appendAnchorCreatedEvent(tx, s.expID, nodeID, assetID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// Link adds a typed exploration edge (idempotent).
func (s *ExplorationStore) Link(from int64, rel string, to int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`
INSERT INTO exploration_edges(exploration_id, src_id, rel, dst_id) VALUES ($1,$2,$3,$4)
ON CONFLICT (exploration_id, src_id, rel, dst_id) DO NOTHING`, s.expID, from, rel, to)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		if err := appendEdgeCreatedEvent(tx, s.expID, from, rel, to); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// SetNodeState updates any node's state (never deletes).
func (s *ExplorationStore) SetNodeState(id int64, state string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow(`SELECT state FROM exploration_nodes WHERE id=$1 AND exploration_id=$2 FOR UPDATE`, id, s.expID).Scan(&previous); err != nil {
		return err
	}
	if previous == state {
		return tx.Commit()
	}
	if _, err := tx.Exec(`UPDATE exploration_nodes SET state=$1 WHERE id=$2 AND exploration_id=$3`, state, id, s.expID); err != nil {
		return err
	}
	if err := appendNodeStateEvent(tx, s.expID, id, previous, state, "", "explicit"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.BumpVersion()
	return nil
}

// RequeueExpiredIntentLeases makes crash recovery explicit without disturbing
// work still owned by another live server instance.
func (s *ExplorationStore) RequeueExpiredIntentLeases() (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM agent_resource_leases WHERE exploration_id=$1 AND lease_expires_at<now()`, s.expID); err != nil {
		return 0, err
	}
	rows, err := tx.Query(`UPDATE exploration_nodes
SET state='open', owner=NULL, lease_expires_at=NULL, completed_at=NULL
WHERE exploration_id=$1 AND kind='intent' AND state='running'
	  AND (lease_expires_at IS NULL OR lease_expires_at < now()) RETURNING id,COALESCE(owner,'')`, s.expID)
	if err != nil {
		return 0, err
	}
	var changed []struct {
		id    int64
		owner string
	}
	for rows.Next() {
		var v struct {
			id    int64
			owner string
		}
		if err := rows.Scan(&v.id, &v.owner); err != nil {
			rows.Close()
			return 0, err
		}
		changed = append(changed, v)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, v := range changed {
		if err := appendNodeStateEvent(tx, s.expID, v.id, "running", "open", v.owner, "lease_expired"); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n := int64(len(changed))
	if n > 0 {
		s.BumpVersion() // P2.6
	}
	return n, nil
}

const nodeCols = `id, kind, payload, priority, state, COALESCE(origin,''), COALESCE(owner,''), lease_expires_at, attempt_count, last_lease_at, created_at`

func scanNode(sc interface{ Scan(...any) error }) (*Node, error) {
	var n Node
	var payload []byte
	if err := sc.Scan(&n.ID, &n.Kind, &payload, &n.Priority, &n.State, &n.Origin, &n.Owner, &n.LeaseExpiresAt, &n.AttemptCount, &n.LastLeaseAt, &n.CreatedAt); err != nil {
		return nil, err
	}
	n.Payload = json.RawMessage(payload)
	return &n, nil
}

func scanNodes(rows *sql.Rows) ([]*Node, error) {
	var out []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListByKind lists nodes of a kind (newest first).
func (s *ExplorationStore) ListByKind(kind string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT `+nodeCols+` FROM exploration_nodes
WHERE exploration_id=$1 AND kind=$2 ORDER BY id DESC LIMIT $3`, s.expID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// CountByKind returns the total number of nodes of a kind in this exploration.
func (s *ExplorationStore) CountByKind(kind string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind=$2`, s.expID, kind).Scan(&n)
	return n, err
}

// CountByKindState returns the number of nodes of a kind with a specific state.
func (s *ExplorationStore) CountByKindState(kind, state string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM exploration_nodes WHERE exploration_id=$1 AND kind=$2 AND state=$3`, s.expID, kind, state).Scan(&n)
	return n, err
}

// ListByKindState lists nodes of a kind filtered by state (newest first). Used
// to fetch only the small recent window needed for display instead of loading
// the full history.
func (s *ExplorationStore) ListByKindState(kind, state string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+nodeCols+` FROM exploration_nodes
WHERE exploration_id=$1 AND kind=$2 AND state=$3 ORDER BY id DESC LIMIT $4`, s.expID, kind, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// IntentCounts is an aggregate snapshot of intent states, computed with indexed
// count(*) queries instead of materializing the full intent history.
type IntentCounts struct {
	Total   int `json:"total"`
	Open    int `json:"open"`
	Running int `json:"running"`
	Blocked int `json:"blocked"`
}

// IntentCounts returns aggregate intent state counts plus a bounded list of the
// most recent running intents. This avoids loading up to tens of thousands of
// intent nodes just to count states on the hot overview polling endpoint.
func (s *ExplorationStore) IntentCounts(runningLimit int) (IntentCounts, []*Node, error) {
	var c IntentCounts
	var err error
	if c.Total, err = s.CountByKind("intent"); err != nil {
		return c, nil, err
	}
	if c.Open, err = s.CountByKindState("intent", "open"); err != nil {
		return c, nil, err
	}
	if c.Running, err = s.CountByKindState("intent", "running"); err != nil {
		return c, nil, err
	}
	if c.Blocked, err = s.CountByKindState("intent", "blocked"); err != nil {
		return c, nil, err
	}
	running, err := s.ListByKindState("intent", "running", runningLimit)
	if err != nil {
		return c, nil, err
	}
	return c, running, nil
}

// GetNode returns one node of this exploration by id (nil, nil if not found).
func (s *ExplorationStore) GetNode(id int64) (*Node, error) {
	n, err := scanNode(s.db.QueryRow(`SELECT `+nodeCols+` FROM exploration_nodes WHERE id=$1 AND exploration_id=$2`, id, s.expID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

// Nodes lists all nodes (for graph viz).
func (s *ExplorationStore) Nodes(limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 2000
	}
	rows, err := s.db.Query(`SELECT `+nodeCols+` FROM exploration_nodes WHERE exploration_id=$1 ORDER BY id LIMIT $2`, s.expID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// Edge is one row from exploration_edges.
type Edge struct {
	From int64
	Rel  string
	To   int64
}

// Edges lists exploration edges (for graph viz).
func (s *ExplorationStore) Edges(limit int) ([]Edge, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.db.Query(`SELECT src_id, rel, dst_id FROM exploration_edges WHERE exploration_id=$1 LIMIT $2`, s.expID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.Rel, &e.To); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// FindingLineage returns the finding node and every exploration node that can
// reach it. The result is the smallest task subgraph needed by the finding
// detail page; unrelated branches are deliberately excluded.
func (s *ExplorationStore) FindingLineage(nodeID int64) ([]*Node, []Edge, error) {
	nodeRows, err := s.db.Query(`
WITH RECURSIVE ancestors(id) AS (
    SELECT $2::bigint
  UNION
    SELECT e.src_id
    FROM exploration_edges e
    JOIN ancestors a ON e.dst_id = a.id
    WHERE e.exploration_id = $1
)
SELECT `+nodeCols+`
FROM exploration_nodes
WHERE exploration_id = $1 AND id IN (SELECT id FROM ancestors)
ORDER BY id`, s.expID, nodeID)
	if err != nil {
		return nil, nil, err
	}
	nodes, err := scanNodes(nodeRows)
	if err != nil || len(nodes) == 0 {
		return nodes, nil, err
	}

	edgeRows, err := s.db.Query(`
WITH RECURSIVE ancestors(id) AS (
    SELECT $2::bigint
  UNION
    SELECT e.src_id
    FROM exploration_edges e
    JOIN ancestors a ON e.dst_id = a.id
    WHERE e.exploration_id = $1
)
SELECT src_id, rel, dst_id
FROM exploration_edges
WHERE exploration_id = $1
  AND src_id IN (SELECT id FROM ancestors)
  AND dst_id IN (SELECT id FROM ancestors)`, s.expID, nodeID)
	if err != nil {
		return nil, nil, err
	}
	defer edgeRows.Close()
	edges := make([]Edge, 0)
	for edgeRows.Next() {
		var edge Edge
		if err := edgeRows.Scan(&edge.From, &edge.Rel, &edge.To); err != nil {
			return nil, nil, err
		}
		edges = append(edges, edge)
	}
	return nodes, edges, edgeRows.Err()
}

// FindingIntents maps each finding id to the intent that produced it (the
// intent --yields--> finding edge; report_finding links it). Findings with no
// producing intent are absent. Precise (JOIN, no edge-scan limit).
func (s *ExplorationStore) FindingIntents() (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT e.dst_id, e.src_id
FROM exploration_edges e
JOIN exploration_nodes n ON n.id = e.dst_id AND n.exploration_id = e.exploration_id
WHERE e.exploration_id=$1 AND e.rel=$2 AND n.kind='finding'`, s.expID, RelYields)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var findingID, intentID int64
		if err := rows.Scan(&findingID, &intentID); err != nil {
			return nil, err
		}
		out[findingID] = intentID
	}
	return out, rows.Err()
}

// Frontier returns open intents ordered by priority desc then id (FIFO tiebreak).
func (s *ExplorationStore) Frontier(limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+nodeCols+` FROM exploration_nodes
WHERE exploration_id=$1 AND kind='intent' AND state='open'
ORDER BY priority DESC, id ASC LIMIT $2`, s.expID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodes(rows)
}

// IntentReady 返回意图的前置依赖是否已满足（P3.1 依赖门控认领）。
// 语义：意图的 derived_from / spawns 父节点——
//   - fact/finding/goal/hint → 已满足（事实在图上即算数）；
//   - intent → 仅当该父意图已 done 且产出了至少一个事实/发现（有 yields 边）才算满足；
//     open/running/blocked/exhausted 的父意图 → 不满足（下游串行链不能抢跑）。
//
// 无父节点 → 满足（顶层意图可直接认领）。
func (s *ExplorationStore) IntentReady(id int64) (bool, error) {
	rows, err := s.db.Query(`
SELECT e.src_id, COALESCE(n.kind,''), COALESCE(n.state,'')
FROM exploration_edges e
LEFT JOIN exploration_nodes n ON n.id = e.src_id AND n.exploration_id = e.exploration_id
WHERE e.dst_id=$1 AND e.exploration_id=$2 AND e.rel IN ('derived_from','spawns')`, id, s.expID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var parents []struct {
		id    int64
		kind  string
		state string
	}
	for rows.Next() {
		var p struct {
			id    int64
			kind  string
			state string
		}
		if err := rows.Scan(&p.id, &p.kind, &p.state); err != nil {
			return false, err
		}
		parents = append(parents, p)
	}
	if rows.Err() != nil {
		return false, rows.Err()
	}
	if len(parents) == 0 {
		return true, nil
	}
	for _, p := range parents {
		switch p.kind {
		case "intent":
			if p.state != "done" {
				return false, nil // 父意图还没完成
			}
			// 父意图 done 还不够：必须有产出（yields 到 fact/finding）才算真正给出前置结果。
			var n int
			if err := s.db.QueryRow(`
SELECT count(*) FROM exploration_edges
WHERE src_id=$1 AND exploration_id=$2 AND rel='yields'`, p.id, s.expID).Scan(&n); err != nil {
				return false, err
			}
			if n == 0 {
				return false, nil
			}
		default:
			// fact/finding/goal/hint 父节点 → 已满足
		}
	}
	return true, nil
}

// ClaimNextIntentLease atomically selects and leases the highest-priority ready
// intent. Expired work can be reclaimed; dependency checks and row locking live
// in the same statement so concurrent workers cannot race a stale frontier.
func (s *ExplorationStore) ClaimNextIntentLease(owner, agent string, lease time.Duration) (*Node, error) {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	if agent == "" {
		agent = owner
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Resource compatibility must be checked and leased atomically. Serializing
	// only this short claim transaction prevents two processes from both seeing
	// an empty conflicting resource set.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, s.expID); err != nil {
		return nil, err
	}
	var n Node
	var raw []byte
	var reclaimed bool
	err = tx.QueryRow(`
WITH candidate AS (
    SELECT n.id, n.state AS previous_state
    FROM exploration_nodes n
    WHERE n.exploration_id=$1 AND n.kind='intent'
      AND (n.state='open' OR (n.state='running' AND n.lease_expires_at < now()))
	  AND NOT EXISTS (
	      SELECT 1 FROM jsonb_to_recordset(COALESCE(n.payload->'resource_claims','[]'::jsonb)) c(key text,mode text)
	      JOIN agent_resource_leases l ON l.exploration_id=n.exploration_id AND l.resource_key=c.key AND l.lease_expires_at>=now()
	      WHERE l.owner<>$2 AND (l.mode='exclusive' OR c.mode='exclusive')
	  )
      AND NOT EXISTS (
          SELECT 1
          FROM exploration_edges dep
          JOIN exploration_nodes parent ON parent.id=dep.src_id AND parent.exploration_id=dep.exploration_id
          WHERE dep.exploration_id=n.exploration_id AND dep.dst_id=n.id
            AND dep.rel IN ('derived_from','spawns') AND parent.kind='intent'
            AND (parent.state <> 'done' OR NOT EXISTS (
                SELECT 1 FROM exploration_edges produced
                WHERE produced.exploration_id=n.exploration_id
                  AND produced.src_id=parent.id AND produced.rel='yields'
            ))
      )
    ORDER BY n.priority DESC, n.id ASC
    FOR UPDATE OF n SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE exploration_nodes n
    SET state='running', owner=$2, attempt_count=n.attempt_count+1,
        last_lease_at=now(), lease_expires_at=now()+($3 * interval '1 second'),
        completed_at=NULL
    FROM candidate c WHERE n.id=c.id
    RETURNING n.id, n.kind, n.payload, n.priority, n.state,
              COALESCE(n.origin,''), COALESCE(n.owner,''), n.lease_expires_at,
              n.attempt_count, n.last_lease_at, n.created_at,
              (c.previous_state='running') AS reclaimed
)
SELECT * FROM claimed`, s.expID, owner, lease.Seconds()).Scan(
		&n.ID, &n.Kind, &raw, &n.Priority, &n.State, &n.Origin, &n.Owner,
		&n.LeaseExpiresAt, &n.AttemptCount, &n.LastLeaseAt, &n.CreatedAt, &reclaimed)
	if err == sql.ErrNoRows {
		if err := appendFirstResourceConflict(tx, s.expID, agent); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	n.Payload = json.RawMessage(raw)
	if err := acquireIntentResources(tx, s.expID, &n, owner, agent); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"intent_id": n.ID, "owner": owner, "attempt": n.AttemptCount,
		"lease_expires_at": n.LeaseExpiresAt, "reclaimed": reclaimed,
	})
	previous := "open"
	if reclaimed {
		previous = "running"
	}
	if err := appendNodeStateEvent(tx, s.expID, n.ID, previous, "running", owner, "intent_claimed"); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if _, err := appendCanonicalEvent(tx, eventScope{explorationID: &s.expID}, Activity{
		NodeID: &n.ID, Worker: agent, EventType: EventIntentClaimed, EventOnly: true, Payload: payload,
	}, nil); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.BumpVersion()
	return &n, nil
}

// RenewIntentLease extends a live lease only for its current owner.
func (s *ExplorationStore) RenewIntentLease(id int64, owner string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE exploration_nodes
SET lease_expires_at=now()+($4 * interval '1 second'), last_lease_at=now()
WHERE id=$1 AND exploration_id=$2 AND kind='intent' AND state='running'
  AND owner=$3 AND lease_expires_at >= now()`, id, s.expID, owner, lease.Seconds())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, tx.Commit()
	}
	if _, err := tx.Exec(`UPDATE agent_resource_leases SET lease_expires_at=now()+($4 * interval '1 second') WHERE exploration_id=$1 AND intent_id=$2 AND owner=$3`, s.expID, id, owner, lease.Seconds()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// FinishIntentLease changes state only while owner still holds an unexpired
// lease. It prevents a delayed worker from overwriting a reclaimed attempt.
func (s *ExplorationStore) FinishIntentLease(id int64, owner, state string) (bool, error) {
	terminal := state == "done" || state == "blocked" || state == "exhausted" || state == "stopped"
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE exploration_nodes
SET state=$4, owner=CASE WHEN $4='open' THEN NULL ELSE owner END,
    lease_expires_at=NULL, completed_at=CASE WHEN $5 THEN now() ELSE NULL END
WHERE id=$1 AND exploration_id=$2 AND kind='intent' AND state='running'
  AND owner=$3 AND lease_expires_at >= now()`, id, s.expID, owner, state, terminal)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		if err := appendNodeStateEvent(tx, s.expID, id, "running", state, owner, "intent_finished"); err != nil {
			return false, err
		}
		var claims json.RawMessage
		_ = tx.QueryRow(`SELECT COALESCE(jsonb_agg(jsonb_build_object('key',resource_key,'mode',mode)),'[]'::jsonb) FROM agent_resource_leases WHERE exploration_id=$1 AND intent_id=$2 AND owner=$3`, s.expID, id, owner).Scan(&claims)
		if len(claims) > 0 && string(claims) != "[]" {
			if err := appendResourceEvent(tx, s.expID, &id, owner, EventResourceLeaseReleased, "", map[string]any{"intent_id": id, "owner": owner, "resource_claims": claims}); err != nil {
				return false, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM agent_resource_leases WHERE exploration_id=$1 AND intent_id=$2 AND owner=$3`, s.expID, id, owner); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		s.BumpVersion()
		return true, nil
	}
	return false, tx.Commit()
}

// Stats returns node counts grouped by kind (for dashboard).
func (s *ExplorationStore) Stats() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT kind, count(*) FROM exploration_nodes WHERE exploration_id=$1 GROUP BY kind`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var c int
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

// --- activity (poll by global id cursor) ---

// AppendActivity records one worker step and returns its id.
func (s *ExplorationStore) AppendActivity(a Activity) (int64, error) {
	prepared := prepareArtifacts(a.Artifacts)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	eventID, err := appendCanonicalEvent(tx, eventScope{explorationID: &s.expID}, a, prepared)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if a.EventOnly {
		return 0, tx.Commit()
	}
	var id int64
	err = tx.QueryRow(`
INSERT INTO activity(exploration_id, node_id, worker, kind, tool, tool_use_id, is_error, summary, detail, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, event_id)
VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,$14)
ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO UPDATE SET event_id=EXCLUDED.event_id
RETURNING id`, s.expID, a.NodeID, utf8Clean(a.Worker), utf8Clean(a.Kind), utf8Clean(a.Tool), utf8Clean(a.ToolUseID), a.IsError, utf8Clean(a.Summary), utf8Clean(a.Detail),
		a.InputTokens, a.OutputTokens, a.CacheReadTokens, a.CacheWriteTokens, eventID).Scan(&id)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	return id, tx.Commit()
}

// TokenTotal sums token usage across ALL workers for this exploration (whole-task
// total). Same source as TokenStatsByWorker (kind='result' rows), just ungrouped.
func (s *ExplorationStore) TokenTotal() (TokenUsage, error) {
	var u TokenUsage
	err := s.db.QueryRow(`SELECT
		COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0)
	FROM activity WHERE exploration_id=$1 AND kind='result'`, s.expID).
		Scan(&u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheWriteTokens)
	return u, err
}

// LastActivity returns the persisted timestamp of the latest activity for this
// exploration. It complements the engine's in-memory heartbeat after a restart.
func (s *ExplorationStore) LastActivity() (int64, error) {
	var unix int64
	err := s.db.QueryRow(`SELECT COALESCE(EXTRACT(EPOCH FROM MAX(created_at))::bigint,0)
		FROM activity WHERE exploration_id=$1`, s.expID).Scan(&unix)
	return unix, err
}

// GoalCounts returns the task goal progress without loading every goal node.
func (s *ExplorationStore) GoalCounts() (GoalCounts, error) {
	var counts GoalCounts
	err := s.db.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE state='met')
		FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal'`, s.expID).
		Scan(&counts.Total, &counts.Met)
	return counts, err
}

// TokenTotalsAll returns the whole-task token total for every exploration in one
// query (exploration_id → total), so the task list can show per-task consumption
// without a query per row.
func (d *DB) TokenTotalsAll() (map[int64]TokenUsage, error) {
	rows, err := d.Query(`SELECT exploration_id,
		COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0)
	FROM activity WHERE kind='result' GROUP BY exploration_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TokenUsage{}
	for rows.Next() {
		var eid int64
		var u TokenUsage
		if err := rows.Scan(&eid, &u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheWriteTokens); err != nil {
			return nil, err
		}
		out[eid] = u
	}
	return out, rows.Err()
}

// LastActivityAll returns the unix time of the most recent activity per
// exploration (exploration_id → max created_at epoch), one query for all tasks.
// Persisted (unlike Engine.LastActivity's in-memory map), so it survives restarts
// and gives终态任务 a stable "ran until" time for computing run duration.
func (d *DB) LastActivityAll() (map[int64]int64, error) {
	rows, err := d.Query(`SELECT exploration_id, EXTRACT(EPOCH FROM MAX(created_at))::bigint FROM activity GROUP BY exploration_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var eid, ts int64
		if err := rows.Scan(&eid, &ts); err != nil {
			return nil, err
		}
		out[eid] = ts
	}
	return out, rows.Err()
}

// GoalCounts is the goal summary for one exploration.
type GoalCounts struct{ Total, Met int }

// GoalCountsAll returns goal totals (total / met) per exploration id, one query
// for all tasks — used by listTasks so the task list shows progress for every task.
func (d *DB) GoalCountsAll() (map[int64]GoalCounts, error) {
	rows, err := d.Query(`
		SELECT exploration_id,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE state = 'met') AS met
		FROM exploration_nodes
		WHERE kind = 'goal'
		GROUP BY exploration_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]GoalCounts{}
	for rows.Next() {
		var eid int64
		var gc GoalCounts
		if err := rows.Scan(&eid, &gc.Total, &gc.Met); err != nil {
			return nil, err
		}
		out[eid] = gc
	}
	return out, rows.Err()
}

// TokenStatsByWorker sums token usage per worker for this exploration (from the
// kind='result' records that carry usage). Used by the per-agent token display.
func (s *ExplorationStore) TokenStatsByWorker() ([]TokenUsage, error) {
	rows, err := s.db.Query(`SELECT COALESCE(worker,''),
		COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0)
	FROM activity WHERE exploration_id=$1 AND kind='result'
	GROUP BY worker ORDER BY worker`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TokenUsage{}
	for rows.Next() {
		var u TokenUsage
		if err := rows.Scan(&u.Worker, &u.InputTokens, &u.OutputTokens, &u.CacheReadTokens, &u.CacheWriteTokens); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// RoundCostsByWorker returns one token-cost row per active worker in a single
// aggregate query. It deliberately uses terminal result rows for token data:
// interim usage events are cumulative and would double-count a model turn.
func (s *ExplorationStore) RoundCostsByWorker() ([]AgentRoundCost, error) {
	rows, err := s.db.Query(`SELECT COALESCE(worker,''),
		COUNT(*) FILTER (WHERE kind='result'),
		COUNT(*) FILTER (WHERE kind='tool_use'),
		COUNT(*) FILTER (WHERE kind='tool_result' AND is_error),
		COALESCE(SUM(input_tokens) FILTER (WHERE kind='result'),0),
		COALESCE(SUM(output_tokens) FILTER (WHERE kind='result'),0),
		COALESCE(SUM(cache_read_tokens) FILTER (WHERE kind='result'),0),
		COALESCE(SUM(cache_write_tokens) FILTER (WHERE kind='result'),0)
	FROM activity
	WHERE exploration_id=$1
	GROUP BY worker
	HAVING COUNT(*) FILTER (WHERE kind IN ('result','tool_use','tool_result')) > 0
	ORDER BY worker`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRoundCost{}
	for rows.Next() {
		var c AgentRoundCost
		if err := rows.Scan(&c.Worker, &c.Rounds, &c.ToolCalls, &c.ToolErrors,
			&c.InputTokens, &c.OutputTokens, &c.CacheReadTokens, &c.CacheWriteTokens); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ActivityList returns steps after sinceID (exclusive), optionally filtered by node.
// Returns items and the new cursor (max id seen).
func (s *ExplorationStore) ActivityList(nodeID *int64, sinceID int64, limit int) ([]Activity, int64, error) {
	if limit <= 0 {
		limit = 300
	}
	var rows *sql.Rows
	var err error
	const cols = `id, node_id, COALESCE(worker,''), COALESCE(kind,''), COALESCE(tool,''), COALESCE(tool_use_id,''), is_error, COALESCE(summary,''), created_at, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens`
	if nodeID != nil {
		rows, err = s.db.Query(`SELECT `+cols+`
FROM activity WHERE exploration_id=$1 AND node_id=$2 AND id>$3 ORDER BY id LIMIT $4`, s.expID, *nodeID, sinceID, limit)
	} else {
		rows, err = s.db.Query(`SELECT `+cols+`
FROM activity WHERE exploration_id=$1 AND id>$2 ORDER BY id LIMIT $3`, s.expID, sinceID, limit)
	}
	if err != nil {
		return nil, sinceID, err
	}
	defer rows.Close()
	out := []Activity{}
	cursor := sinceID
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.NodeID, &a.Worker, &a.Kind, &a.Tool, &a.ToolUseID, &a.IsError, &a.Summary, &a.CreatedAt,
			&a.InputTokens, &a.OutputTokens, &a.CacheReadTokens, &a.CacheWriteTokens); err != nil {
			return nil, sinceID, err
		}
		if a.ID > cursor {
			cursor = a.ID
		}
		out = append(out, a)
	}
	return out, cursor, rows.Err()
}

// ActivityAllByCompany returns recent activity rows (newest first, capped at
// limit) from tasks associated with the given company (0 = all tasks), joined
// with the task id so the UI can group/steer. The activity seq column is the
// global activity.id; task_id is the owning exploration's task id.
func (d *DB) ActivityAllByCompany(companyID int64, limit int) ([]ActivityTaskRow, error) {
	if limit <= 0 {
		limit = 300
	}
	q := `
SELECT a.id, a.exploration_id, COALESCE(a.node_id,0), COALESCE(a.worker,''), COALESCE(a.kind,''),
       COALESCE(a.tool,''), COALESCE(a.tool_use_id,''), a.is_error, COALESCE(a.summary,''), a.created_at,
       COALESCE(a.input_tokens,0), COALESCE(a.output_tokens,0), COALESCE(a.cache_read_tokens,0), COALESCE(a.cache_write_tokens,0)
FROM activity a
`
	if companyID > 0 {
		q += ` JOIN tasks t ON t.exploration_id = a.exploration_id
       JOIN task_companies tc ON tc.task_id = t.id AND tc.company_id = $1
`
	}
	q += ` ORDER BY a.id DESC LIMIT $` + fmt.Sprintf("%d", 1+boolArg(companyID > 0))
	args := []any{limit}
	if companyID > 0 {
		args = append([]any{companyID}, args...)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityTaskRow{}
	for rows.Next() {
		var r ActivityTaskRow
		if err := rows.Scan(&r.ID, &r.ExplorationID, &r.NodeID, &r.Worker, &r.Kind, &r.Tool, &r.ToolUseID, &r.IsError, &r.Summary, &r.CreatedAt,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// boolArg converts a bool to 1/0 for dynamic SQL arg-position math.
func boolArg(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ActivityTaskRow is one activity row with its owning exploration/task id.
type ActivityTaskRow struct {
	Activity
	ExplorationID int64 `json:"exploration_id"`
}

// ActivityDetail lazily returns the full detail blob for one step.
func (s *ExplorationStore) ActivityDetail(id int64) (string, error) {
	var d sql.NullString
	err := s.db.QueryRow(`SELECT detail FROM activity WHERE id=$1 AND exploration_id=$2`, id, s.expID).Scan(&d)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return d.String, err
}

// scanTrace reads the summary-only column set shared by the worker-trace tools
// (ActivityTrace / ActivityTraceSearch). Detail is never selected here — it is
// pulled separately, on demand, via ActivityByIDs.
func scanTrace(rows *sql.Rows) ([]Activity, error) {
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.NodeID, &a.Worker, &a.Kind, &a.Tool, &a.IsError, &a.Summary); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const traceCols = `id, node_id, COALESCE(worker,''), COALESCE(kind,''), COALESCE(tool,''), is_error, COALESCE(summary,'')`

// ActivityTrace lists one work's steps (by intent/node id) for the trace tools:
// summaries only (detail is lazy — fetch via ActivityByIDs). thinking/usage rows
// are dropped so the caller sees the work's actions + observations, not the
// model's internal reasoning or token-accounting noise.
func (s *ExplorationStore) ActivityTrace(nodeID int64, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`SELECT `+traceCols+`
FROM activity WHERE exploration_id=$1 AND node_id=$2 AND kind NOT IN ('thinking','usage')
ORDER BY id LIMIT $3`, s.expID, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTrace(rows)
}

// ActivityTraceSearch finds steps whose summary OR detail matches q
// (case-insensitive), returning summaries only. nodeID != nil scopes to one work;
// nil searches across ALL works (node_id IS NOT NULL — planner/main steps have no
// node_id, so this cleanly means "worker traces"). thinking/usage rows dropped.
func (s *ExplorationStore) ActivityTraceSearch(nodeID *int64, q string, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 100
	}
	like := "%" + q + "%"
	var rows *sql.Rows
	var err error
	if nodeID != nil {
		rows, err = s.db.Query(`SELECT `+traceCols+`
FROM activity WHERE exploration_id=$1 AND node_id=$2 AND kind NOT IN ('thinking','usage')
AND (summary ILIKE $3 OR detail ILIKE $3) ORDER BY id LIMIT $4`, s.expID, *nodeID, like, limit)
	} else {
		rows, err = s.db.Query(`SELECT `+traceCols+`
FROM activity WHERE exploration_id=$1 AND node_id IS NOT NULL AND kind NOT IN ('thinking','usage')
AND (summary ILIKE $2 OR detail ILIKE $2) ORDER BY id LIMIT $3`, s.expID, like, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTrace(rows)
}

// ActivityByIDs loads full detail for specific step ids (the trace drill-down),
// scoped to this exploration. thinking rows are skipped (never returned, even if
// their id is asked for). Order is ascending id, not the input order.
func (s *ExplorationStore) ActivityByIDs(ids []int64) ([]Activity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids)+1)
	args[0] = s.expID
	for i, id := range ids {
		ph[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}
	rows, err := s.db.Query(`SELECT id, node_id, COALESCE(kind,''), COALESCE(tool,''), is_error, COALESCE(detail,'')
FROM activity WHERE exploration_id=$1 AND kind<>'thinking' AND id IN (`+strings.Join(ph, ",")+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.NodeID, &a.Kind, &a.Tool, &a.IsError, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AddStandaloneFinding writes a finding to the standalone findings table, which
// persists across task deletion (task_id / node_id become NULL when the task or
// exploration node is deleted). taskID and nodeID may be 0 (stored as NULL).
func (s *ExplorationStore) AddStandaloneFinding(taskID, nodeID int64, vulnclass, severity, summary, evidence, worker string, assetIDs []int64) (int64, error) {
	id, err := s.db.AddFinding(taskID, nodeID, vulnclass, severity, summary, evidence, worker, assetIDs)
	if err == nil {
		s.BumpVersion() // P2.6
	}
	return id, err
}

// SetFindingReport stores the structured 8-block vuln report on a finding row
// (task-context delegation to the underlying DB).
func (s *ExplorationStore) SetFindingReport(id int64, report string) (int64, error) {
	return s.db.SetFindingReport(id, report)
}

// SetFindingPOC stores the raw request/response packets on a finding row.
func (s *ExplorationStore) SetFindingPOC(id int64, requestRaw, responseRaw string) error {
	return s.db.SetFindingPOC(id, requestRaw, responseRaw)
}
