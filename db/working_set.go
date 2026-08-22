package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const workingSetSchemaVersion = 1

// WorkingSet is the bounded, replayable context generated at a compaction
// boundary. Full evidence and tool output remain addressable by event/artifact ID.
type WorkingSet struct {
	ID             int64                   `json:"id"`
	ExplorationID  *int64                  `json:"exploration_id,omitempty"`
	ConversationID *int64                  `json:"conversation_id,omitempty"`
	SourceEventID  int64                   `json:"source_event_id"`
	Version        int                     `json:"version"`
	SchemaVersion  int                     `json:"schema_version"`
	ContentHash    string                  `json:"content_hash"`
	Fixed          WorkingSetFixedLayer    `json:"fixed"`
	Sliding        WorkingSetSlidingLayer  `json:"sliding"`
	External       WorkingSetExternalLayer `json:"external"`
	ModelSummary   string                  `json:"model_summary,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
}

type WorkingSetFixedLayer struct {
	Objective          string              `json:"objective"`
	AuthorizationScope []string            `json:"authorization_scope"`
	Constraints        []string            `json:"constraints"`
	ConfirmedFacts     []WorkingSetNodeRef `json:"confirmed_facts"`
	RiskPolicy         []string            `json:"risk_policy"`
}

type WorkingSetSlidingLayer struct {
	RecentIntents       []WorkingSetNodeRef  `json:"recent_intents"`
	PendingDependencies []WorkingSetNodeRef  `json:"pending_dependencies"`
	RecentToolErrors    []WorkingSetEventRef `json:"recent_tool_errors"`
	PendingEvidence     []WorkingSetNodeRef  `json:"pending_evidence"`
	ArtifactRefs        []WorkingSetArtifact `json:"artifact_refs"`
}

type WorkingSetExternalLayer struct {
	EventCursor  int64   `json:"event_cursor"`
	ArtifactIDs  []int64 `json:"artifact_ids"`
	EventsAPI    string  `json:"events_api"`
	ArtifactsAPI string  `json:"artifacts_api"`
}

type WorkingSetNodeRef struct {
	ID       int64   `json:"id"`
	State    string  `json:"state"`
	Summary  string  `json:"summary"`
	Evidence string  `json:"evidence,omitempty"`
	AssetIDs []int64 `json:"asset_ids,omitempty"`
}

type WorkingSetEventRef struct {
	ID            int64  `json:"id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Tool          string `json:"tool,omitempty"`
	Summary       string `json:"summary"`
}

type WorkingSetArtifact struct {
	ID          int64  `json:"id"`
	ContentHash string `json:"content_hash"`
	MIMEType    string `json:"mime_type"`
	ByteSize    int64  `json:"byte_size"`
	Summary     string `json:"summary,omitempty"`
}

func projectWorkingSet(tx *sql.Tx, scope eventScope, sourceEventID int64, modelSummary string) (*WorkingSet, error) {
	if scope.explorationID == nil {
		return nil, nil // task projections are implemented first; conversation events remain canonical.
	}
	var existing int64
	if err := tx.QueryRow(`SELECT id FROM agent_working_sets WHERE source_event_id=$1`, sourceEventID).Scan(&existing); err == nil {
		return nil, nil
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	explorationID := *scope.explorationID
	// Serializes version allocation for concurrent planner/worker compactions.
	var lockedID int64
	if err := tx.QueryRow(`SELECT id FROM explorations WHERE id=$1 FOR UPDATE`, explorationID).Scan(&lockedID); err != nil {
		return nil, err
	}
	ws := &WorkingSet{
		ExplorationID: scope.explorationID, SourceEventID: sourceEventID,
		SchemaVersion: workingSetSchemaVersion, ModelSummary: utf8Clean(modelSummary),
		Fixed: WorkingSetFixedLayer{AuthorizationScope: []string{}, Constraints: []string{},
			ConfirmedFacts: []WorkingSetNodeRef{}, RiskPolicy: []string{}},
		Sliding: WorkingSetSlidingLayer{RecentIntents: []WorkingSetNodeRef{}, PendingDependencies: []WorkingSetNodeRef{},
			RecentToolErrors: []WorkingSetEventRef{}, PendingEvidence: []WorkingSetNodeRef{}, ArtifactRefs: []WorkingSetArtifact{}},
		External: WorkingSetExternalLayer{EventCursor: sourceEventID, ArtifactIDs: []int64{}},
	}
	var description string
	var taskID int64
	if err := tx.QueryRow(`SELECT COALESCE(t.id,0), COALESCE(e.description,''), e.goal FROM explorations e
LEFT JOIN tasks t ON t.exploration_id=e.id WHERE e.id=$1`, explorationID).
		Scan(&taskID, &description, &ws.Fixed.Objective); err != nil {
		return nil, err
	}
	if taskID > 0 {
		ws.External.EventsAPI = fmt.Sprintf("/api/tasks/%d/events", taskID)
		ws.External.ArtifactsAPI = fmt.Sprintf("/api/tasks/%d/artifacts", taskID)
	}
	ws.Fixed.Constraints = explicitConstraints(description)
	ws.Fixed.RiskPolicy = explicitRiskPolicy(ws.Fixed.Constraints)
	if err := loadAuthorizationScope(tx, explorationID, &ws.Fixed.AuthorizationScope); err != nil {
		return nil, err
	}
	var err error
	if ws.Fixed.ConfirmedFacts, err = loadWorkingSetNodes(tx, explorationID,
		`kind='fact' AND state='confirmed'`, 50); err != nil {
		return nil, err
	}
	if ws.Sliding.RecentIntents, err = loadWorkingSetNodes(tx, explorationID, `kind='intent'`, 8); err != nil {
		return nil, err
	}
	if ws.Sliding.PendingDependencies, err = loadWorkingSetNodes(tx, explorationID,
		`kind='intent' AND state IN ('open','running','blocked')`, 8); err != nil {
		return nil, err
	}
	if ws.Sliding.PendingEvidence, err = loadPendingEvidence(tx, explorationID); err != nil {
		return nil, err
	}
	if ws.Sliding.RecentToolErrors, err = loadRecentToolErrors(tx, explorationID); err != nil {
		return nil, err
	}
	if ws.Sliding.ArtifactRefs, err = loadWorkingSetArtifacts(tx, explorationID); err != nil {
		return nil, err
	}
	for _, artifact := range ws.Sliding.ArtifactRefs {
		ws.External.ArtifactIDs = append(ws.External.ArtifactIDs, artifact.ID)
	}
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM agent_working_sets WHERE exploration_id=$1`, explorationID).Scan(&ws.Version); err != nil {
		return nil, err
	}
	hashInput, _ := json.Marshal(struct {
		SchemaVersion int                     `json:"schema_version"`
		Fixed         WorkingSetFixedLayer    `json:"fixed"`
		Sliding       WorkingSetSlidingLayer  `json:"sliding"`
		External      WorkingSetExternalLayer `json:"external"`
	}{ws.SchemaVersion, ws.Fixed, ws.Sliding, ws.External})
	digest := sha256.Sum256(hashInput)
	ws.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	fixed, _ := json.Marshal(ws.Fixed)
	sliding, _ := json.Marshal(ws.Sliding)
	external, _ := json.Marshal(ws.External)
	if err := tx.QueryRow(`
INSERT INTO agent_working_sets(exploration_id, source_event_id, version, schema_version, content_hash,
    fixed_layer, sliding_layer, external_layer, model_summary)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`, explorationID, sourceEventID, ws.Version,
		ws.SchemaVersion, ws.ContentHash, string(fixed), string(sliding), string(external), ws.ModelSummary).
		Scan(&ws.ID, &ws.CreatedAt); err != nil {
		return nil, err
	}
	// The event already carries model_summary as payload.detail; avoid storing the
	// same potentially large text twice while keeping the event fully replayable.
	eventWorkingSet := *ws
	eventWorkingSet.ModelSummary = ""
	encoded, _ := json.Marshal(&eventWorkingSet)
	if _, err := tx.Exec(`UPDATE agent_events SET payload=payload || jsonb_build_object('working_set',$2::jsonb) WHERE id=$1`, sourceEventID, string(encoded)); err != nil {
		return nil, err
	}
	return ws, nil
}

func loadAuthorizationScope(tx *sql.Tx, explorationID int64, dst *[]string) error {
	rows, err := tx.Query(`
SELECT ts.kind, COALESCE(c.name,''), COALESCE(ts.domain,''), COALESCE(ts.net::text,''), ts.source
FROM task_scope ts JOIN tasks t ON t.id=ts.task_id
LEFT JOIN companies c ON c.id=ts.company_id
WHERE t.exploration_id=$1 ORDER BY ts.id`, explorationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, company, domain, network, source string
		if err := rows.Scan(&kind, &company, &domain, &network, &source); err != nil {
			return err
		}
		value := domain
		if value == "" {
			value = network
		}
		if value == "" {
			value = company
		}
		*dst = append(*dst, fmt.Sprintf("%s:%s (%s)", kind, value, source))
	}
	return rows.Err()
}

func loadWorkingSetNodes(tx *sql.Tx, explorationID int64, predicate string, limit int) ([]WorkingSetNodeRef, error) {
	rows, err := tx.Query(`SELECT id, state, payload, ARRAY(SELECT asset_id FROM exploration_anchors WHERE node_id=exploration_nodes.id ORDER BY asset_id)
FROM exploration_nodes WHERE exploration_id=$1 AND `+predicate+` ORDER BY id DESC LIMIT $2`, explorationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkingSetNodes(rows)
}

func scanWorkingSetNodes(rows *sql.Rows) ([]WorkingSetNodeRef, error) {
	out := []WorkingSetNodeRef{}
	for rows.Next() {
		var ref WorkingSetNodeRef
		var raw []byte
		if err := rows.Scan(&ref.ID, &ref.State, &raw, &ref.AssetIDs); err != nil {
			return nil, err
		}
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		ref.Summary, _ = payload["summary"].(string)
		if ref.Summary == "" {
			ref.Summary, _ = payload["text"].(string)
		}
		ref.Evidence, _ = payload["evidence"].(string)
		out = append(out, ref)
	}
	return out, rows.Err()
}

func loadPendingEvidence(tx *sql.Tx, explorationID int64) ([]WorkingSetNodeRef, error) {
	rows, err := tx.Query(`SELECT n.id, n.state, n.payload,
ARRAY(SELECT asset_id FROM exploration_anchors WHERE node_id=n.id ORDER BY asset_id)
FROM exploration_nodes n WHERE n.exploration_id=$1 AND n.kind='intent'
AND n.state IN ('open','running','done','blocked')
AND NOT EXISTS (SELECT 1 FROM exploration_edges e JOIN exploration_nodes r ON r.id=e.dst_id
    WHERE e.exploration_id=$1 AND e.src_id=n.id AND e.rel='yields' AND r.kind IN ('fact','finding'))
ORDER BY n.id DESC LIMIT 8`, explorationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkingSetNodes(rows)
}

func loadRecentToolErrors(tx *sql.Tx, explorationID int64) ([]WorkingSetEventRef, error) {
	rows, err := tx.Query(`SELECT id, COALESCE(correlation_id,''), COALESCE(payload->>'tool',''), COALESCE(payload->>'summary','')
FROM agent_events WHERE exploration_id=$1 AND event_type='tool_result' AND COALESCE((payload->>'is_error')::boolean,false)
ORDER BY id DESC LIMIT 5`, explorationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkingSetEventRef{}
	for rows.Next() {
		var ref WorkingSetEventRef
		if err := rows.Scan(&ref.ID, &ref.CorrelationID, &ref.Tool, &ref.Summary); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func loadWorkingSetArtifacts(tx *sql.Tx, explorationID int64) ([]WorkingSetArtifact, error) {
	rows, err := tx.Query(`SELECT id, content_hash, mime_type, byte_size, COALESCE(summary,'')
FROM artifacts WHERE exploration_id=$1 ORDER BY id DESC LIMIT 8`, explorationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkingSetArtifact{}
	for rows.Next() {
		var ref WorkingSetArtifact
		if err := rows.Scan(&ref.ID, &ref.ContentHash, &ref.MIMEType, &ref.ByteSize, &ref.Summary); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func explicitConstraints(description string) []string {
	parts := strings.FieldsFunc(description, func(r rune) bool { return r == '\n' || r == '\r' || r == '。' || r == ';' || r == '；' })
	keywords := []string{"禁止", "不得", "仅限", "只允许", "约束", "范围", "must not", "do not", "only", "scope"}
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		lower := strings.ToLower(part)
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				out = append(out, part)
				break
			}
		}
		if len(out) == 8 {
			break
		}
	}
	return out
}

func explicitRiskPolicy(constraints []string) []string {
	keywords := []string{"禁止", "不得", "破坏", "must not", "do not"}
	out := []string{}
	for _, constraint := range constraints {
		lower := strings.ToLower(constraint)
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				out = append(out, constraint)
				break
			}
		}
	}
	return out
}

func scanWorkingSet(sc interface{ Scan(...any) error }) (*WorkingSet, error) {
	var ws WorkingSet
	var fixed, sliding, external []byte
	if err := sc.Scan(&ws.ID, &ws.ExplorationID, &ws.ConversationID, &ws.SourceEventID, &ws.Version,
		&ws.SchemaVersion, &ws.ContentHash, &fixed, &sliding, &external, &ws.ModelSummary, &ws.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(fixed, &ws.Fixed); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(sliding, &ws.Sliding); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(external, &ws.External); err != nil {
		return nil, err
	}
	return &ws, nil
}

const workingSetColumns = `id, exploration_id, conversation_id, source_event_id, version, schema_version,
content_hash, fixed_layer, sliding_layer, external_layer, model_summary, created_at`

func (s *ExplorationStore) WorkingSet() (*WorkingSet, error) {
	ws, err := scanWorkingSet(s.db.QueryRow(`SELECT `+workingSetColumns+` FROM agent_working_sets
WHERE exploration_id=$1 ORDER BY version DESC LIMIT 1`, s.expID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return ws, err
}

func (s *ExplorationStore) WorkingSets(beforeVersion, limit int) ([]*WorkingSet, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if beforeVersion <= 0 {
		beforeVersion = int(^uint(0) >> 1)
	}
	rows, err := s.db.Query(`SELECT `+workingSetColumns+` FROM agent_working_sets
WHERE exploration_id=$1 AND version<$2 ORDER BY version DESC LIMIT $3`, s.expID, beforeVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkingSet{}
	for rows.Next() {
		ws, err := scanWorkingSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}
