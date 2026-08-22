package db

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	EventTurnStarted       = "turn_started"
	EventPromptAssembled   = "prompt_assembled"
	EventToolCalled        = "tool_called"
	EventToolResult        = "tool_result"
	EventArtifactCreated   = "artifact_created"
	EventSummaryCreated    = "summary_created"
	EventIntentClaimed     = "intent_claimed"
	EventIntentRejected    = "intent_rejected"
	EventBudgetChanged     = "budget_changed"
	EventTurnFinished      = "turn_finished"
	EventAssistantText     = "assistant_text"
	EventAssistantThinking = "assistant_thinking"
	EventUserMessage       = "user_message"
)

// ArtifactCandidate is a file made model-visible by a tool result. Metadata is
// verified from disk before the database transaction starts.
type ArtifactCandidate struct {
	Path            string
	MIMEType        string
	ByteSize        int64
	LineCount       int64
	Summary         string
	PermissionLabel string
}

type preparedArtifact struct {
	ArtifactCandidate
	ContentHash string
}

// AgentEvent is the canonical append-only record used to rebuild projections.
type AgentEvent struct {
	ID               int64           `json:"id"`
	ExplorationID    *int64          `json:"exploration_id,omitempty"`
	ConversationID   *int64          `json:"conversation_id,omitempty"`
	TurnID           string          `json:"turn_id,omitempty"`
	NodeID           *int64          `json:"node_id,omitempty"`
	Agent            string          `json:"agent,omitempty"`
	EventType        string          `json:"event_type"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	ArtifactID       *int64          `json:"artifact_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	InputTokens      *int            `json:"input_tokens,omitempty"`
	OutputTokens     *int            `json:"output_tokens,omitempty"`
	CacheReadTokens  *int            `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int            `json:"cache_write_tokens,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

// Artifact is searchable metadata for a file referenced by an agent event.
type Artifact struct {
	ID              int64     `json:"id"`
	ExplorationID   *int64    `json:"exploration_id,omitempty"`
	ConversationID  *int64    `json:"conversation_id,omitempty"`
	NodeID          *int64    `json:"node_id,omitempty"`
	Agent           string    `json:"agent,omitempty"`
	SourceTool      string    `json:"source_tool,omitempty"`
	CorrelationID   string    `json:"correlation_id,omitempty"`
	StoragePath     string    `json:"storage_path"`
	ContentHash     string    `json:"content_hash"`
	MIMEType        string    `json:"mime_type"`
	ByteSize        int64     `json:"byte_size"`
	LineCount       int64     `json:"line_count"`
	Summary         string    `json:"summary,omitempty"`
	PermissionLabel string    `json:"permission_label"`
	CreatedAt       time.Time `json:"created_at"`
}

type eventScope struct {
	explorationID  *int64
	conversationID *int64
}

func eventTypeForActivity(a Activity) string {
	if a.EventType != "" {
		return a.EventType
	}
	switch a.Kind {
	case "tool_use":
		return EventToolCalled
	case "tool_result":
		return EventToolResult
	case "usage":
		return EventBudgetChanged
	case "result":
		return EventTurnFinished
	case "text":
		return EventAssistantText
	case "thinking":
		return EventAssistantThinking
	case "user":
		return EventUserMessage
	default:
		return "activity_recorded"
	}
}

func activityPayload(a Activity) []byte {
	p := map[string]any{}
	if len(a.Payload) > 0 && json.Valid(a.Payload) {
		_ = json.Unmarshal(a.Payload, &p)
	}
	if a.Kind != "" {
		p["kind"] = a.Kind
	}
	p["is_error"] = a.IsError
	if a.Tool != "" {
		p["tool"] = a.Tool
	}
	if a.Summary != "" {
		p["summary"] = utf8Clean(a.Summary)
	}
	if a.Detail != "" {
		p["detail"] = utf8Clean(a.Detail)
	}
	b, _ := json.Marshal(p)
	return b
}

func appendCanonicalEvent(tx *sql.Tx, scope eventScope, a Activity, artifacts []preparedArtifact) (int64, error) {
	var eventID int64
	err := tx.QueryRow(`
INSERT INTO agent_events(exploration_id, conversation_id, turn_id, node_id, agent, event_type, correlation_id, payload,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, source_key)
VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,NULLIF($7,''),$8,$9,$10,$11,$12,NULLIF($13,''))
ON CONFLICT (source_key) DO UPDATE SET source_key=EXCLUDED.source_key
RETURNING id`, scope.explorationID, scope.conversationID, utf8Clean(a.TurnID), a.NodeID, utf8Clean(a.Worker),
		eventTypeForActivity(a), utf8Clean(a.ToolUseID), string(activityPayload(a)), a.InputTokens, a.OutputTokens,
		a.CacheReadTokens, a.CacheWriteTokens, utf8Clean(a.EventKey)).Scan(&eventID)
	if err != nil {
		return 0, err
	}
	for _, artifact := range artifacts {
		artifactID, err := insertArtifact(tx, scope, a, artifact)
		if err != nil {
			return 0, err
		}
		payload, _ := json.Marshal(map[string]any{
			"artifact_id": artifactID, "path": artifact.Path, "content_hash": artifact.ContentHash,
			"mime_type": artifact.MIMEType, "byte_size": artifact.ByteSize, "line_count": artifact.LineCount,
			"summary": artifact.Summary, "source_event_id": eventID,
		})
		if _, err := tx.Exec(`
INSERT INTO agent_events(exploration_id, conversation_id, turn_id, node_id, agent, event_type, correlation_id, artifact_id, payload, source_key)
VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,NULLIF($7,''),$8,$9,NULLIF($10,''))
ON CONFLICT (source_key) DO NOTHING`,
			scope.explorationID, scope.conversationID, utf8Clean(a.TurnID), a.NodeID, utf8Clean(a.Worker),
			EventArtifactCreated, utf8Clean(a.ToolUseID), artifactID, string(payload), artifactEventKey(a.EventKey, artifact)); err != nil {
			return 0, err
		}
	}
	if eventTypeForActivity(a) == EventSummaryCreated {
		if _, err := projectWorkingSet(tx, scope, eventID, a.Detail); err != nil {
			return 0, err
		}
	}
	return eventID, nil
}

func artifactEventKey(parent string, artifact preparedArtifact) string {
	if parent == "" {
		return ""
	}
	return parent + ":artifact:" + artifact.ContentHash + ":" + artifact.Path
}

func insertArtifact(tx *sql.Tx, scope eventScope, a Activity, artifact preparedArtifact) (int64, error) {
	label := artifact.PermissionLabel
	if label == "" {
		label = "task"
		if scope.conversationID != nil {
			label = "conversation"
		}
	}
	var id int64
	err := tx.QueryRow(`
INSERT INTO artifacts(exploration_id, conversation_id, node_id, agent, source_tool, correlation_id,
    storage_path, content_hash, mime_type, byte_size, line_count, summary, permission_label)
VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,NULLIF($12,''),$13)
ON CONFLICT DO NOTHING RETURNING id`, scope.explorationID, scope.conversationID, a.NodeID, utf8Clean(a.Worker),
		utf8Clean(a.Tool), utf8Clean(a.ToolUseID), artifact.Path, artifact.ContentHash, artifact.MIMEType,
		artifact.ByteSize, artifact.LineCount, utf8Clean(artifact.Summary), label).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	err = tx.QueryRow(`
SELECT id FROM artifacts
WHERE COALESCE(exploration_id,0)=COALESCE($1,0)
  AND COALESCE(conversation_id,0)=COALESCE($2,0)
  AND content_hash=$3 AND storage_path=$4`, scope.explorationID, scope.conversationID,
		artifact.ContentHash, artifact.Path).Scan(&id)
	return id, err
}

func prepareArtifacts(candidates []ArtifactCandidate) []preparedArtifact {
	out := make([]preparedArtifact, 0, len(candidates))
	for _, candidate := range candidates {
		f, err := os.Open(candidate.Path)
		if err != nil {
			continue
		}
		h := sha256.New()
		counter := &artifactLineCounter{}
		if _, err := io.Copy(io.MultiWriter(h, counter), f); err != nil {
			_ = f.Close()
			continue
		}
		info, err := f.Stat()
		_ = f.Close()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		candidate.Path = filepath.Clean(candidate.Path)
		candidate.ByteSize = info.Size()
		candidate.LineCount = counter.lines()
		if candidate.MIMEType == "" {
			candidate.MIMEType = mime.TypeByExtension(strings.ToLower(filepath.Ext(candidate.Path)))
			if candidate.MIMEType == "" {
				candidate.MIMEType = "application/octet-stream"
			}
		}
		out = append(out, preparedArtifact{ArtifactCandidate: candidate, ContentHash: "sha256:" + hex.EncodeToString(h.Sum(nil))})
	}
	return out
}

type artifactLineCounter struct {
	bytes    int64
	newlines int64
}

func (c *artifactLineCounter) Write(p []byte) (int, error) {
	c.bytes += int64(len(p))
	c.newlines += int64(bytes.Count(p, []byte{'\n'}))
	return len(p), nil
}

func (c *artifactLineCounter) lines() int64 {
	if c.bytes == 0 {
		return 0
	}
	return c.newlines + 1
}

// AgentEvents returns the canonical exploration stream after a global cursor.
func (s *ExplorationStore) AgentEvents(sinceID int64, limit int) ([]AgentEvent, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`
SELECT id, exploration_id, conversation_id, COALESCE(turn_id,''), node_id, COALESCE(agent,''),
       event_type, COALESCE(correlation_id,''), artifact_id, payload,
       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, created_at
FROM agent_events WHERE exploration_id=$1 AND id>$2 ORDER BY id LIMIT $3`, s.expID, sinceID, limit)
	if err != nil {
		return nil, sinceID, err
	}
	defer rows.Close()
	out := []AgentEvent{}
	cursor := sinceID
	for rows.Next() {
		var event AgentEvent
		if err := rows.Scan(&event.ID, &event.ExplorationID, &event.ConversationID, &event.TurnID, &event.NodeID,
			&event.Agent, &event.EventType, &event.CorrelationID, &event.ArtifactID, &event.Payload,
			&event.InputTokens, &event.OutputTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CreatedAt); err != nil {
			return nil, sinceID, err
		}
		out = append(out, event)
		cursor = event.ID
	}
	return out, cursor, rows.Err()
}

// Artifacts searches artifact metadata for one exploration. File content is
// fetched separately in bounded pages through ArtifactPage.
func (s *ExplorationStore) Artifacts(query string, afterID int64, limit int) ([]Artifact, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`
SELECT id, exploration_id, conversation_id, node_id, COALESCE(agent,''), COALESCE(source_tool,''),
       COALESCE(correlation_id,''), storage_path, content_hash, mime_type, byte_size, line_count,
       COALESCE(summary,''), permission_label, created_at
FROM artifacts
WHERE exploration_id=$1 AND id>$2
  AND ($3 OR storage_path ILIKE $4 OR content_hash ILIKE $4 OR source_tool ILIKE $4 OR summary ILIKE $4)
ORDER BY id LIMIT $5`, s.expID, afterID, query == "", pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.ID, &artifact.ExplorationID, &artifact.ConversationID, &artifact.NodeID,
			&artifact.Agent, &artifact.SourceTool, &artifact.CorrelationID, &artifact.StoragePath,
			&artifact.ContentHash, &artifact.MIMEType, &artifact.ByteSize, &artifact.LineCount,
			&artifact.Summary, &artifact.PermissionLabel, &artifact.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	return out, rows.Err()
}

// ArtifactPage reads a bounded byte range only after verifying that the file is
// registered to this exploration. It returns metadata, bytes, and the next offset.
func (s *ExplorationStore) ArtifactPage(id, offset int64, limit int) (*Artifact, []byte, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 1<<20 {
		limit = 64 << 10
	}
	var artifact Artifact
	err := s.db.QueryRow(`
SELECT id, exploration_id, conversation_id, node_id, COALESCE(agent,''), COALESCE(source_tool,''),
       COALESCE(correlation_id,''), storage_path, content_hash, mime_type, byte_size, line_count,
       COALESCE(summary,''), permission_label, created_at
FROM artifacts WHERE id=$1 AND exploration_id=$2`, id, s.expID).Scan(
		&artifact.ID, &artifact.ExplorationID, &artifact.ConversationID, &artifact.NodeID,
		&artifact.Agent, &artifact.SourceTool, &artifact.CorrelationID, &artifact.StoragePath,
		&artifact.ContentHash, &artifact.MIMEType, &artifact.ByteSize, &artifact.LineCount,
		&artifact.Summary, &artifact.PermissionLabel, &artifact.CreatedAt)
	if err != nil {
		return nil, nil, offset, err
	}
	f, err := os.Open(artifact.StoragePath)
	if err != nil {
		return &artifact, nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return &artifact, nil, offset, err
	}
	b := make([]byte, limit)
	n, err := f.Read(b)
	if err != nil && err != io.EOF {
		return &artifact, nil, offset, err
	}
	return &artifact, b[:n], offset + int64(n), nil
}
