package db

import (
	"database/sql"
	"fmt"
)

// schemaMigrations keeps schema changes ordered and auditable. schema.sql is
// still the idempotent bootstrap for fresh databases; incremental changes must
// be added here so upgrades do not depend on an untracked ALTER statement.
type migration struct {
	Version int
	Name    string
	Apply   func(*sql.Tx) error
}

var migrations = []migration{
	{
		Version: 1,
		Name:    "conversations_company_fk",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE conversations ADD COLUMN IF NOT EXISTS company_id BIGINT REFERENCES companies(id) ON DELETE SET NULL`)
			return err
		},
	},
	{
		Version: 2,
		Name:    "batch_task_leases",
		Apply: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE batch_tasks ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE batch_tasks ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_batch_tasks_lease ON batch_tasks(status, lease_until)`)
			return err
		},
	},
	{
		Version: 3,
		Name:    "canonical_agent_events",
		Apply: func(tx *sql.Tx) error {
			// schema.sql creates the tables and columns before migrations run. This
			// migration gives pre-existing UI projection rows a stable source event.
			if _, err := tx.Exec(`
INSERT INTO agent_events(
    exploration_id, turn_id, node_id, agent, event_type, correlation_id, payload,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, source_key, created_at)
SELECT exploration_id, NULL, node_id, worker,
       CASE kind
         WHEN 'tool_use' THEN 'tool_called'
         WHEN 'tool_result' THEN 'tool_result'
         WHEN 'usage' THEN 'budget_changed'
         WHEN 'result' THEN 'turn_finished'
         WHEN 'text' THEN 'assistant_text'
         WHEN 'thinking' THEN 'assistant_thinking'
         WHEN 'user' THEN 'user_message'
         ELSE 'activity_recorded'
       END,
       tool_use_id,
       jsonb_strip_nulls(jsonb_build_object(
         'kind', kind, 'tool', tool, 'is_error', is_error,
         'summary', summary, 'detail', detail)),
       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
       'activity:' || id::text, created_at
FROM activity
ON CONFLICT (source_key) DO NOTHING`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
UPDATE activity a SET event_id=e.id
FROM agent_events e
WHERE a.event_id IS NULL AND e.source_key='activity:' || a.id::text`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
INSERT INTO agent_events(
    conversation_id, turn_id, agent, event_type, correlation_id, payload,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, source_key, created_at)
SELECT conversation_id, NULL, worker,
       CASE kind
         WHEN 'tool_use' THEN 'tool_called'
         WHEN 'tool_result' THEN 'tool_result'
         WHEN 'usage' THEN 'budget_changed'
         WHEN 'result' THEN 'turn_finished'
         WHEN 'text' THEN 'assistant_text'
         WHEN 'thinking' THEN 'assistant_thinking'
         WHEN 'user' THEN 'user_message'
         ELSE 'activity_recorded'
       END,
       tool_use_id,
       jsonb_strip_nulls(jsonb_build_object(
         'kind', kind, 'tool', tool, 'is_error', is_error,
         'summary', summary, 'detail', detail)),
       input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
       'conversation_activity:' || id::text, created_at
FROM conversation_activities
ON CONFLICT (source_key) DO NOTHING`); err != nil {
				return err
			}
			_, err := tx.Exec(`
UPDATE conversation_activities a SET event_id=e.id
FROM agent_events e
WHERE a.event_id IS NULL AND e.source_key='conversation_activity:' || a.id::text`)
			return err
		},
	},
	{
		Version: 4,
		Name:    "exploration_intent_leases",
		Apply: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE exploration_nodes ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE exploration_nodes ADD COLUMN IF NOT EXISTS attempt_count INT NOT NULL DEFAULT 0`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE exploration_nodes ADD COLUMN IF NOT EXISTS last_lease_at TIMESTAMPTZ`); err != nil {
				return err
			}
			// Pre-lease running rows have no recoverable owner lifetime. Reopen them
			// once during migration; subsequent crash recovery is expiry-based.
			if _, err := tx.Exec(`UPDATE exploration_nodes SET state='open', owner=NULL, completed_at=NULL WHERE kind='intent' AND state='running' AND lease_expires_at IS NULL`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_expnodes_lease ON exploration_nodes(exploration_id, lease_expires_at) WHERE kind='intent' AND state='running'`)
			return err
		},
	},
	{
		Version: 5,
		Name:    "structured_task_delegations",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS task_delegations (
                child_task_id BIGINT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
                parent_ref TEXT,
                contract JSONB NOT NULL,
                created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
            )`)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_task_delegations_parent ON task_delegations(parent_ref) WHERE parent_ref IS NOT NULL`)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`DROP TRIGGER IF EXISTS trg_task_delegations_upd ON task_delegations;
CREATE TRIGGER trg_task_delegations_upd BEFORE UPDATE ON task_delegations
FOR EACH ROW EXECUTE FUNCTION set_updated_at()`)
			return err
		},
	},
	{
		Version: 6,
		Name:    "atomic_intent_dedupe",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_expnodes_intent_dedupe
ON exploration_nodes(exploration_id, (payload->>'dedupe_key'))
WHERE kind='intent' AND payload ? 'dedupe_key'`)
			return err
		},
	},
}

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
    )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	for _, m := range migrations {
		var exists bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, m.Version).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if exists {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}
		if err := m.Apply(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version,name) VALUES ($1,$2)`, m.Version, m.Name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}
