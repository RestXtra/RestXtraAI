package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
)

const (
	EventNodeCreated      = "node_created"
	EventNodeStateChanged = "node_state_changed"
	EventEdgeCreated      = "edge_created"
	EventAnchorCreated    = "anchor_created"
)

func appendNodeCreatedEvent(tx *sql.Tx, explorationID int64, nodeID int64, kind string, payload json.RawMessage, priority int, state, origin string) error {
	raw, _ := json.Marshal(map[string]any{"node_id": nodeID, "kind": kind, "payload": payload, "priority": priority, "state": state, "origin": origin})
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{NodeID: &nodeID, Worker: origin, EventType: EventNodeCreated, EventOnly: true, Payload: raw}, nil)
	return err
}

func appendNodeStateEvent(tx *sql.Tx, explorationID, nodeID int64, previous, state, owner, reason string) error {
	raw, _ := json.Marshal(map[string]any{"node_id": nodeID, "previous_state": previous, "state": state, "owner": owner, "reason": reason})
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{NodeID: &nodeID, Worker: owner, EventType: EventNodeStateChanged, EventOnly: true, Payload: raw}, nil)
	return err
}

func appendEdgeCreatedEvent(tx *sql.Tx, explorationID, from int64, rel string, to int64) error {
	raw, _ := json.Marshal(map[string]any{"from": from, "rel": rel, "to": to})
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{EventType: EventEdgeCreated, EventOnly: true, Payload: raw}, nil)
	return err
}

func appendAnchorCreatedEvent(tx *sql.Tx, explorationID, nodeID, assetID int64) error {
	raw, _ := json.Marshal(map[string]any{"node_id": nodeID, "asset_id": assetID})
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{NodeID: &nodeID, EventType: EventAnchorCreated, EventOnly: true, Payload: raw}, nil)
	return err
}

type graphProjection struct {
	Nodes   map[int64]graphProjectionNode `json:"nodes"`
	Edges   []graphProjectionEdge         `json:"edges"`
	Anchors []graphProjectionAnchor       `json:"anchors"`
}

type graphProjectionNode struct {
	ID       int64           `json:"id"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
	Priority int             `json:"priority"`
	State    string          `json:"state"`
	Origin   string          `json:"origin"`
}

type graphProjectionEdge struct {
	From int64  `json:"from"`
	Rel  string `json:"rel"`
	To   int64  `json:"to"`
}
type graphProjectionAnchor struct {
	NodeID  int64 `json:"node_id"`
	AssetID int64 `json:"asset_id"`
}

// GraphProjectionVerification is a non-destructive shadow replay result. It is
// deliberately explicit about legacy/incomplete streams instead of claiming a
// match from a partial event history.
type GraphProjectionVerification struct {
	Complete         bool   `json:"complete"`
	Matches          bool   `json:"matches"`
	EventHash        string `json:"event_hash"`
	ProjectionHash   string `json:"projection_hash"`
	EventNodes       int    `json:"event_nodes"`
	ProjectedNodes   int    `json:"projected_nodes"`
	EventEdges       int    `json:"event_edges"`
	ProjectedEdges   int    `json:"projected_edges"`
	EventAnchors     int    `json:"event_anchors"`
	ProjectedAnchors int    `json:"projected_anchors"`
}

// VerifyGraphProjection returns a cached shadow-replay result when the graph
// version is unchanged since it was last computed, avoiding a full graph replay
// + full projected graph load on every dashboard poll. It recomputes only when
// the graph version bumps.
func (s *ExplorationStore) VerifyGraphProjection() (*GraphProjectionVerification, error) {
	l := s.db.ovLock(s.expID)
	l.Lock()
	cached, ok := s.db.projCache[s.expID]
	ver := s.db.ovVer[s.expID]
	if ok && cached != nil && s.db.projVer[s.expID] == ver {
		l.Unlock()
		return cached, nil
	}
	l.Unlock()

	verification, err := s.computeGraphProjection()
	if err != nil {
		return nil, err
	}
	l.Lock()
	if s.db.projVer == nil {
		s.db.projVer = map[int64]int64{}
	}
	if s.db.projCache == nil {
		s.db.projCache = map[int64]*GraphProjectionVerification{}
	}
	s.db.projVer[s.expID] = s.db.ovVer[s.expID]
	s.db.projCache[s.expID] = verification
	l.Unlock()
	return verification, nil
}

func (s *ExplorationStore) computeGraphProjection() (*GraphProjectionVerification, error) {
	projected, err := s.loadProjectedGraph()
	if err != nil {
		return nil, err
	}
	replayed, err := s.replayGraphEvents()
	if err != nil {
		return nil, err
	}
	eventHash := hashGraphProjection(replayed)
	projectionHash := hashGraphProjection(projected)
	complete := len(replayed.Nodes) == len(projected.Nodes) && len(replayed.Edges) == len(projected.Edges) && len(replayed.Anchors) == len(projected.Anchors)
	return &GraphProjectionVerification{
		Complete: complete, Matches: complete && eventHash == projectionHash, EventHash: eventHash, ProjectionHash: projectionHash,
		EventNodes: len(replayed.Nodes), ProjectedNodes: len(projected.Nodes), EventEdges: len(replayed.Edges), ProjectedEdges: len(projected.Edges), EventAnchors: len(replayed.Anchors), ProjectedAnchors: len(projected.Anchors),
	}, nil
}

func newGraphProjection() *graphProjection {
	return &graphProjection{Nodes: map[int64]graphProjectionNode{}, Edges: []graphProjectionEdge{}, Anchors: []graphProjectionAnchor{}}
}

func (s *ExplorationStore) replayGraphEvents() (*graphProjection, error) {
	p := newGraphProjection()
	rows, err := s.db.Query(`SELECT event_type,payload FROM agent_events WHERE exploration_id=$1 AND event_type IN ($2,$3,$4,$5) ORDER BY id`, s.expID, EventNodeCreated, EventNodeStateChanged, EventEdgeCreated, EventAnchorCreated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var raw []byte
		if err := rows.Scan(&typ, &raw); err != nil {
			return nil, err
		}
		switch typ {
		case EventNodeCreated:
			var v graphProjectionNode
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			p.Nodes[v.ID] = v
		case EventNodeStateChanged:
			var v struct {
				NodeID int64  `json:"node_id"`
				State  string `json:"state"`
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			n, ok := p.Nodes[v.NodeID]
			if ok {
				n.State = v.State
				p.Nodes[v.NodeID] = n
			}
		case EventEdgeCreated:
			var v graphProjectionEdge
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			p.Edges = append(p.Edges, v)
		case EventAnchorCreated:
			var v graphProjectionAnchor
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, err
			}
			p.Anchors = append(p.Anchors, v)
		}
	}
	return p, rows.Err()
}

func (s *ExplorationStore) loadProjectedGraph() (*graphProjection, error) {
	p := newGraphProjection()
	rows, err := s.db.Query(`SELECT id,kind,payload,priority,state,COALESCE(origin,'') FROM exploration_nodes WHERE exploration_id=$1 ORDER BY id`, s.expID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n graphProjectionNode
		var raw []byte
		if err := rows.Scan(&n.ID, &n.Kind, &raw, &n.Priority, &n.State, &n.Origin); err != nil {
			rows.Close()
			return nil, err
		}
		n.Payload = raw
		p.Nodes[n.ID] = n
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	edges, err := s.db.Query(`SELECT src_id,rel,dst_id FROM exploration_edges WHERE exploration_id=$1 ORDER BY src_id,rel,dst_id`, s.expID)
	if err != nil {
		return nil, err
	}
	for edges.Next() {
		var e graphProjectionEdge
		if err := edges.Scan(&e.From, &e.Rel, &e.To); err != nil {
			edges.Close()
			return nil, err
		}
		p.Edges = append(p.Edges, e)
	}
	if err := edges.Close(); err != nil {
		return nil, err
	}
	anchors, err := s.db.Query(`SELECT a.node_id,a.asset_id FROM exploration_anchors a JOIN exploration_nodes n ON n.id=a.node_id WHERE n.exploration_id=$1 ORDER BY a.node_id,a.asset_id`, s.expID)
	if err != nil {
		return nil, err
	}
	defer anchors.Close()
	for anchors.Next() {
		var a graphProjectionAnchor
		if err := anchors.Scan(&a.NodeID, &a.AssetID); err != nil {
			return nil, err
		}
		p.Anchors = append(p.Anchors, a)
	}
	return p, anchors.Err()
}

func hashGraphProjection(p *graphProjection) string {
	nodes := make([]graphProjectionNode, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(p.Edges, func(i, j int) bool {
		if p.Edges[i].From != p.Edges[j].From {
			return p.Edges[i].From < p.Edges[j].From
		}
		if p.Edges[i].Rel != p.Edges[j].Rel {
			return p.Edges[i].Rel < p.Edges[j].Rel
		}
		return p.Edges[i].To < p.Edges[j].To
	})
	sort.Slice(p.Anchors, func(i, j int) bool {
		if p.Anchors[i].NodeID != p.Anchors[j].NodeID {
			return p.Anchors[i].NodeID < p.Anchors[j].NodeID
		}
		return p.Anchors[i].AssetID < p.Anchors[j].AssetID
	})
	raw, _ := json.Marshal(struct {
		Nodes   []graphProjectionNode   `json:"nodes"`
		Edges   []graphProjectionEdge   `json:"edges"`
		Anchors []graphProjectionAnchor `json:"anchors"`
	}{nodes, p.Edges, p.Anchors})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
