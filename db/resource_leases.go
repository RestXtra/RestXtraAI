package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	EventResourceLeaseAcquired = "resource_lease_acquired"
	EventResourceLeaseReleased = "resource_lease_released"
	EventResourceConflictWait  = "resource_conflict_wait"
)

type ResourceClaim struct {
	Key  string `json:"key"`
	Mode string `json:"mode"`
}

type ActiveResourceLease struct {
	IntentID       int64     `json:"intent_id"`
	Owner          string    `json:"owner"`
	ResourceKey    string    `json:"resource_key"`
	Mode           string    `json:"mode"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

func (s *ExplorationStore) ActiveResourceLeases() ([]ActiveResourceLease, error) {
	rows, err := s.db.Query(`SELECT intent_id,owner,resource_key,mode,lease_expires_at FROM agent_resource_leases WHERE exploration_id=$1 AND lease_expires_at>=now() ORDER BY resource_key,owner`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActiveResourceLease{}
	for rows.Next() {
		var lease ActiveResourceLease
		if err := rows.Scan(&lease.IntentID, &lease.Owner, &lease.ResourceKey, &lease.Mode, &lease.LeaseExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func intentConcurrencyClass(payload map[string]any) string {
	if mode, _ := payload["concurrency_class"].(string); mode == "shared" || mode == "exclusive" {
		return mode
	}
	class := normalizedIntentClass(payload["intent_class"])
	switch class {
	case "recon", "endpoint-enum", "fingerprint", "passive":
		return "shared"
	}
	return "exclusive"
}

func normalizeResourceClaims(payload map[string]any, assetIDs []int64) []ResourceClaim {
	mode := intentConcurrencyClass(payload)
	byKey := map[string]string{}
	add := func(prefix, value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		key := prefix + value
		if byKey[key] == "exclusive" {
			return
		}
		byKey[key] = mode
	}
	for _, id := range canonicalIntentIDs(assetIDs) {
		add("asset:", fmt.Sprint(id))
	}
	for _, field := range []struct{ name, prefix string }{{"account_scopes", "account:"}, {"rate_limit_domains", "rate-limit:"}} {
		if values, ok := payload[field.name].([]string); ok {
			for _, value := range values {
				add(field.prefix, value)
			}
		} else if values, ok := payload[field.name].([]any); ok {
			for _, value := range values {
				if s, ok := value.(string); ok {
					add(field.prefix, s)
				}
			}
		}
	}
	claims := make([]ResourceClaim, 0, len(byKey))
	for key, claimMode := range byKey {
		claims = append(claims, ResourceClaim{Key: key, Mode: claimMode})
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].Key < claims[j].Key })
	return claims
}

func appendResourceEvent(tx *sql.Tx, explorationID int64, intentID *int64, worker, eventType, eventKey string, payload map[string]any) error {
	raw, _ := json.Marshal(payload)
	_, err := appendCanonicalEvent(tx, eventScope{explorationID: &explorationID}, Activity{NodeID: intentID, Worker: worker, EventType: eventType, EventOnly: true, EventKey: eventKey, Payload: raw}, nil)
	return err
}

func acquireIntentResources(tx *sql.Tx, explorationID int64, n *Node, owner, agent string) error {
	res, err := tx.Exec(`INSERT INTO agent_resource_leases(exploration_id,intent_id,owner,resource_key,mode,lease_expires_at)
SELECT $1,$2,$3,c.key,c.mode,$4 FROM jsonb_to_recordset(COALESCE($5::jsonb->'resource_claims','[]'::jsonb)) AS c(key text,mode text)
ON CONFLICT (exploration_id,resource_key,owner) DO UPDATE SET lease_expires_at=EXCLUDED.lease_expires_at`, explorationID, n.ID, owner, n.LeaseExpiresAt, string(n.Payload))
	if err != nil {
		return err
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return nil
	}
	var intentPayload map[string]any
	_ = json.Unmarshal(n.Payload, &intentPayload)
	return appendResourceEvent(tx, explorationID, &n.ID, agent, EventResourceLeaseAcquired, "", map[string]any{"intent_id": n.ID, "owner": owner, "resource_claims": intentPayload["resource_claims"], "lease_expires_at": n.LeaseExpiresAt})
}

func appendFirstResourceConflict(tx *sql.Tx, explorationID int64, agent string) error {
	var intentID int64
	var owner, key, mode string
	var expiry int64
	err := tx.QueryRow(`SELECT n.id,l.owner,l.resource_key,l.mode,EXTRACT(EPOCH FROM l.lease_expires_at)::bigint
FROM exploration_nodes n CROSS JOIN LATERAL jsonb_to_recordset(COALESCE(n.payload->'resource_claims','[]'::jsonb)) c(key text,mode text)
JOIN agent_resource_leases l ON l.exploration_id=n.exploration_id AND l.resource_key=c.key AND l.lease_expires_at>=now()
WHERE n.exploration_id=$1 AND n.kind='intent' AND n.state='open' AND (l.mode='exclusive' OR c.mode='exclusive')
ORDER BY n.priority DESC,n.id LIMIT 1`, explorationID).Scan(&intentID, &owner, &key, &mode, &expiry)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("resource-conflict:%d:%s:%s:%d", intentID, owner, key, expiry)
	return appendResourceEvent(tx, explorationID, &intentID, agent, EventResourceConflictWait, eventKey, map[string]any{"intent_id": intentID, "blocking_owner": owner, "resource_key": key, "blocking_mode": mode, "lease_expires_unix": expiry})
}
