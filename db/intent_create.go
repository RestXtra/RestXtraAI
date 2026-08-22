package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrIntentScopeFused = errors.New("同类资产范围已连续两次没有产出")

func canonicalIntentIDs(ids []int64) []int64 {
	out := dedupeIDs(ids)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func intentDedupeKey(summary string, assetIDs, parentIDs []int64) string {
	canonical := struct {
		Summary   string  `json:"summary"`
		AssetIDs  []int64 `json:"asset_ids"`
		ParentIDs []int64 `json:"parent_ids"`
	}{
		Summary:  strings.ToLower(strings.Join(strings.Fields(summary), " ")),
		AssetIDs: canonicalIntentIDs(assetIDs), ParentIDs: canonicalIntentIDs(parentIDs),
	}
	raw, _ := json.Marshal(canonical)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func normalizedIntentClass(value any) string {
	class, _ := value.(string)
	return strings.ToLower(strings.Join(strings.Fields(class), "-"))
}

func intentScopeKey(class string, assetIDs []int64) string {
	if class == "" {
		return ""
	}
	raw, _ := json.Marshal(struct {
		Class    string  `json:"intent_class"`
		AssetIDs []int64 `json:"asset_ids"`
	}{class, canonicalIntentIDs(assetIDs)})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func appendIntentRejected(tx *sql.Tx, explorationID int64, payload map[string]any, nodeID *int64) error {
	raw, _ := json.Marshal(payload)
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{
		NodeID: nodeID, Worker: "planner", EventType: EventIntentRejected, EventOnly: true, Payload: raw,
	}, nil)
	return err
}

func recentZeroYieldIntents(tx *sql.Tx, explorationID int64, scopeKey string) ([]int64, error) {
	rows, err := tx.Query(`SELECT n.id, NOT EXISTS (SELECT 1 FROM exploration_edges e
      WHERE e.exploration_id=$1 AND e.src_id=n.id AND e.rel='yields') AS zero_yield
FROM exploration_nodes n
WHERE n.exploration_id=$1 AND n.kind='intent' AND n.payload->>'intent_scope_key'=$2
  AND n.state IN ('done','blocked','exhausted','stopped')
ORDER BY COALESCE(n.completed_at,n.updated_at,n.created_at) DESC,n.id DESC LIMIT 2`, explorationID, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var zeroYield bool
		if err := rows.Scan(&id, &zeroYield); err != nil {
			return nil, err
		}
		if !zeroYield {
			return nil, nil
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func hasNovelMaterialParent(tx *sql.Tx, explorationID int64, parentIDs []int64, scopeKey string) (bool, error) {
	for _, parentID := range parentIDs {
		var novel bool
		err := tx.QueryRow(`SELECT n.state <> 'origin' AND NOT EXISTS (
    SELECT 1 FROM exploration_edges e
    JOIN exploration_nodes prior ON prior.id=e.dst_id AND prior.exploration_id=e.exploration_id
    WHERE e.exploration_id=$1 AND e.src_id=$2 AND e.rel='derived_from'
      AND prior.kind='intent' AND prior.payload->>'intent_scope_key'=$3
) FROM exploration_nodes n WHERE n.id=$2 AND n.exploration_id=$1`, explorationID, parentID, scopeKey).Scan(&novel)
		if err != nil {
			return false, err
		}
		if novel {
			return true, nil
		}
	}
	return false, nil
}

func validateIntentParent(tx *sql.Tx, explorationID, parentID int64) error {
	var kind, state string
	err := tx.QueryRow(`SELECT kind,state FROM exploration_nodes WHERE id=$1 AND exploration_id=$2`, parentID, explorationID).Scan(&kind, &state)
	if err == sql.ErrNoRows {
		return fmt.Errorf("parent_id %d 不存在", parentID)
	}
	if err != nil {
		return err
	}
	valid := (kind == KindFact && (state == "confirmed" || state == StateOrigin)) ||
		(kind == KindFinding && state == "confirmed")
	if !valid {
		return fmt.Errorf("parent_id %d 是 %s/%s，意图只能由已确认 fact/finding 派生", parentID, kind, state)
	}
	return nil
}

// AddIntentWithLineage atomically validates parents, inserts one intent, anchors
// assets and writes derived_from edges. The normalized dedupe key includes both
// asset and parent sets: an exact retry is reused, while genuinely new evidence
// (a different parent fact/finding) can justify exploring the direction again.
func (s *ExplorationStore) AddIntentWithLineage(payload map[string]any, priority int, assetIDs, parentIDs []int64, origin string) (id int64, created bool, err error) {
	if priority == 0 {
		priority = 5
	}
	assets := canonicalIntentIDs(assetIDs)
	parents := canonicalIntentIDs(parentIDs)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	// Serialize the small number of planner writes per exploration. Besides making
	// legacy origin creation race-free, this gives deterministic parent/key input
	// before the unique index handles cross-process retries.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, s.expID); err != nil {
		return 0, false, err
	}
	if len(parents) == 0 {
		var originID int64
		err := tx.QueryRow(`SELECT id FROM exploration_nodes
WHERE exploration_id=$1 AND kind='fact' AND state='origin' ORDER BY id LIMIT 1`, s.expID).Scan(&originID)
		if err != nil && err != sql.ErrNoRows {
			return 0, false, err
		}
		if err == sql.ErrNoRows {
			var description, goal string
			if err := tx.QueryRow(`SELECT COALESCE(description,''),goal FROM explorations WHERE id=$1`, s.expID).Scan(&description, &goal); err != nil {
				return 0, false, err
			}
			originPayload, _ := json.Marshal(map[string]any{
				"summary":     "任务起点：" + description + "；目标：" + goal,
				"description": description, "goal": goal,
			})
			if err := tx.QueryRow(`INSERT INTO exploration_nodes(exploration_id,kind,payload,priority,state,origin)
VALUES ($1,'fact',$2,0,'origin','system') RETURNING id`, s.expID, string(originPayload)).Scan(&originID); err != nil {
				return 0, false, err
			}
			if err := appendNodeCreatedEvent(tx, s.expID, originID, KindFact, originPayload, 0, StateOrigin, "system"); err != nil {
				return 0, false, err
			}
		}
		parents = []int64{originID}
	}
	for _, parentID := range parents {
		if err := validateIntentParent(tx, s.expID, parentID); err != nil {
			return 0, false, err
		}
	}

	summary, _ := payload["summary"].(string)
	key := intentDedupeKey(summary, assets, parents)
	class := normalizedIntentClass(payload["intent_class"])
	scopeKey := intentScopeKey(class, assets)
	stored := make(map[string]any, len(payload)+2)
	for name, value := range payload {
		stored[name] = value
	}
	stored["dedupe_key"] = key
	if class != "" {
		stored["intent_class"] = class
		stored["intent_scope_key"] = scopeKey
	}
	if len(assets) > 0 {
		stored["asset_ids"] = assets
	}
	stored["concurrency_class"] = intentConcurrencyClass(stored)
	stored["resource_claims"] = normalizeResourceClaims(stored, assets)
	raw, err := json.Marshal(stored)
	if err != nil {
		return 0, false, err
	}

	err = tx.QueryRow(`INSERT INTO exploration_nodes(exploration_id,kind,payload,priority,state,origin)
VALUES ($1,'intent',$2,$3,'open',$4)
ON CONFLICT DO NOTHING RETURNING id`, s.expID, string(raw), priority, origin).Scan(&id)
	if err == sql.ErrNoRows {
		err = tx.QueryRow(`SELECT id FROM exploration_nodes
WHERE exploration_id=$1 AND kind='intent' AND payload->>'dedupe_key'=$2`, s.expID, key).Scan(&id)
		if err != nil {
			return 0, false, err
		}
		rejection := map[string]any{"reason": "duplicate_intent", "asset_ids": assets, "parent_ids": parents, "existing_intent_id": id}
		if class != "" {
			rejection["intent_class"], rejection["intent_scope_key"] = class, scopeKey
		}
		if err := appendIntentRejected(tx, s.expID, rejection, &id); err != nil {
			return 0, false, err
		}
		if err := tx.Commit(); err != nil {
			return 0, false, err
		}
		return id, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	for _, assetID := range assets {
		if _, err := tx.Exec(`INSERT INTO exploration_anchors(node_id,asset_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, assetID); err != nil {
			return 0, false, err
		}
	}
	if scopeKey != "" {
		recent, err := recentZeroYieldIntents(tx, s.expID, scopeKey)
		if err != nil {
			return 0, false, err
		}
		novel, err := hasNovelMaterialParent(tx, s.expID, parents, scopeKey)
		if err != nil {
			return 0, false, err
		}
		if len(recent) >= 2 && !novel {
			if _, err := tx.Exec(`DELETE FROM exploration_nodes WHERE id=$1`, id); err != nil {
				return 0, false, err
			}
			if err := appendIntentRejected(tx, s.expID, map[string]any{
				"reason": "zero_yield_scope_fuse", "intent_class": class, "intent_scope_key": scopeKey,
				"asset_ids": assets, "parent_ids": parents, "recent_zero_yield_intent_ids": recent,
			}, nil); err != nil {
				return 0, false, err
			}
			if err := tx.Commit(); err != nil {
				return 0, false, err
			}
			return 0, false, fmt.Errorf("%w（intent_class=%s，recent_intent_ids=%v）", ErrIntentScopeFused, class, recent)
		}
	}
	for _, parentID := range parents {
		if _, err := tx.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id)
VALUES ($1,$2,'derived_from',$3) ON CONFLICT DO NOTHING`, s.expID, parentID, id); err != nil {
			return 0, false, err
		}
	}
	if err := appendNodeCreatedEvent(tx, s.expID, id, KindIntent, raw, priority, "open", origin); err != nil {
		return 0, false, err
	}
	for _, assetID := range assets {
		if err := appendAnchorCreatedEvent(tx, s.expID, id, assetID); err != nil {
			return 0, false, err
		}
	}
	for _, parentID := range parents {
		if err := appendEdgeCreatedEvent(tx, s.expID, parentID, RelDerivedFrom, id); err != nil {
			return 0, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	s.BumpVersion()
	return id, true, nil
}
