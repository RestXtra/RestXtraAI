package db

import (
	"database/sql"
	"fmt"
	"time"
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
	{
		Version: 7,
		Name:    "intent_scope_fuse",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_expnodes_intent_scope
ON exploration_nodes(exploration_id, (payload->>'intent_scope_key'), completed_at DESC, id DESC)
WHERE kind='intent' AND payload ? 'intent_scope_key'`)
			return err
		},
	},
	{
		Version: 8,
		Name:    "agent_resource_leases",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS agent_resource_leases (
                id BIGSERIAL PRIMARY KEY,
                exploration_id BIGINT NOT NULL REFERENCES explorations(id) ON DELETE CASCADE,
                intent_id BIGINT NOT NULL REFERENCES exploration_nodes(id) ON DELETE CASCADE,
                owner TEXT NOT NULL,
                resource_key TEXT NOT NULL,
                mode TEXT NOT NULL CHECK (mode IN ('shared','exclusive')),
                lease_expires_at TIMESTAMPTZ NOT NULL,
                created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
                UNIQUE (exploration_id, resource_key, owner)
            );
            CREATE INDEX IF NOT EXISTS idx_resource_leases_conflict
                ON agent_resource_leases(exploration_id, resource_key, lease_expires_at)`)
			return err
		},
	},
	{
		Version: 9,
		Name:    "canonical_graph_event_backfill",
		Apply: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`INSERT INTO agent_events(exploration_id,node_id,agent,event_type,payload,source_key,created_at)
SELECT n.exploration_id,n.id,n.origin,'node_created',jsonb_build_object(
    'node_id',n.id,'kind',n.kind,'payload',n.payload,'priority',n.priority,'state',n.state,'origin',COALESCE(n.origin,''),'backfilled',true),
    'graph-backfill:node:'||n.id::text,n.created_at
FROM exploration_nodes n
WHERE NOT EXISTS (SELECT 1 FROM agent_events e WHERE e.exploration_id=n.exploration_id AND e.event_type='node_created' AND (e.payload->>'node_id')::bigint=n.id)
ON CONFLICT (source_key) DO NOTHING`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO agent_events(exploration_id,event_type,payload,source_key,created_at)
SELECT e.exploration_id,'edge_created',jsonb_build_object('from',e.src_id,'rel',e.rel,'to',e.dst_id,'backfilled',true),
    'graph-backfill:edge:'||e.exploration_id::text||':'||e.src_id::text||':'||e.rel||':'||e.dst_id::text,e.created_at
FROM exploration_edges e
WHERE NOT EXISTS (SELECT 1 FROM agent_events a WHERE a.exploration_id=e.exploration_id AND a.event_type='edge_created'
  AND (a.payload->>'from')::bigint=e.src_id AND a.payload->>'rel'=e.rel AND (a.payload->>'to')::bigint=e.dst_id)
ON CONFLICT (source_key) DO NOTHING`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO agent_events(exploration_id,node_id,event_type,payload,source_key)
SELECT n.exploration_id,a.node_id,'anchor_created',jsonb_build_object('node_id',a.node_id,'asset_id',a.asset_id,'backfilled',true),
    'graph-backfill:anchor:'||a.node_id::text||':'||a.asset_id::text
FROM exploration_anchors a JOIN exploration_nodes n ON n.id=a.node_id
WHERE NOT EXISTS (SELECT 1 FROM agent_events e WHERE e.exploration_id=n.exploration_id AND e.event_type='anchor_created'
  AND (e.payload->>'node_id')::bigint=a.node_id AND (e.payload->>'asset_id')::bigint=a.asset_id)
ON CONFLICT (source_key) DO NOTHING`)
			return err
		},
	},
	{
		Version: 10,
		Name:    "agent_event_task_type_index",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_events_exploration_type ON agent_events(exploration_id,event_type,id) WHERE exploration_id IS NOT NULL`)
			return err
		},
	},
	{
		Version: 11,
		Name:    "c2_operations_upgrade",
		Apply: func(tx *sql.Tx) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS c2_profiles (
					id BIGSERIAL PRIMARY KEY,
					name TEXT NOT NULL,
					kind TEXT NOT NULL DEFAULT 'http',
					config JSONB NOT NULL DEFAULT '{}',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
				`CREATE TABLE IF NOT EXISTS c2_tasks (
					id BIGSERIAL PRIMARY KEY,
					session_id TEXT NOT NULL REFERENCES c2_sessions(session_id) ON DELETE CASCADE,
					command TEXT NOT NULL DEFAULT '',
					request JSONB NOT NULL DEFAULT '{}',
					state TEXT NOT NULL DEFAULT 'queued',
					description TEXT NOT NULL DEFAULT '',
					response JSONB NOT NULL DEFAULT '{}',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
					sent_at TIMESTAMPTZ,
					completed_at TIMESTAMPTZ)`,
				`CREATE INDEX IF NOT EXISTS idx_c2_tasks_sid ON c2_tasks(session_id, state)`,
				`CREATE TABLE IF NOT EXISTS c2_auto_tasks (
					id BIGSERIAL PRIMARY KEY,
					listener_id BIGINT REFERENCES c2_listeners(id) ON DELETE CASCADE,
					name TEXT NOT NULL,
					enabled BOOLEAN NOT NULL DEFAULT true,
					order_idx INTEGER NOT NULL DEFAULT 0,
					target TEXT NOT NULL DEFAULT 'commands',
					workflow_id BIGINT,
					commands JSONB NOT NULL DEFAULT '[]',
					conditions JSONB NOT NULL DEFAULT '{}',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'stopped'`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS profile_id BIGINT REFERENCES c2_profiles(id) ON DELETE SET NULL`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT '{}'`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS disguise JSONB NOT NULL DEFAULT '{}'`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS firewall JSONB NOT NULL DEFAULT '{}'`,
				`ALTER TABLE c2_listeners ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS remote_ip TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS hostname TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS uid TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS gid TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS os TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS arch TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS pid INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS process_name TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS connection TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_sessions ADD COLUMN IF NOT EXISTS first_seen TIMESTAMPTZ NOT NULL DEFAULT now()`,
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		Version: 12,
		Name:    "c2_vshell_modules",
		Apply: func(tx *sql.Tx) error {
			stmts := []string{
				`CREATE TABLE IF NOT EXISTS c2_plugins (
					id BIGSERIAL PRIMARY KEY,
					name TEXT NOT NULL,
					description TEXT NOT NULL DEFAULT '',
					commands JSONB NOT NULL DEFAULT '[]',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
				`CREATE TABLE IF NOT EXISTS c2_generated (
					id BIGSERIAL PRIMARY KEY,
					name TEXT NOT NULL,
					listener_id BIGINT REFERENCES c2_listeners(id) ON DELETE SET NULL,
					os TEXT NOT NULL DEFAULT 'linux',
					arch TEXT NOT NULL DEFAULT 'amd64',
					config JSONB NOT NULL DEFAULT '{}',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
				`CREATE TABLE IF NOT EXISTS c2_tunnels (
					id BIGSERIAL PRIMARY KEY,
					session_id TEXT NOT NULL,
					kind TEXT NOT NULL DEFAULT 'socks5',
					bind_host TEXT NOT NULL DEFAULT '127.0.0.1',
					bind_port INTEGER NOT NULL DEFAULT 1080,
					target TEXT NOT NULL DEFAULT '',
					state TEXT NOT NULL DEFAULT 'stopped',
					error TEXT NOT NULL DEFAULT '',
					created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		Version: 13,
		Name:    "c2_generated_build_columns",
		Apply: func(tx *sql.Tx) error {
			stmts := []string{
				`ALTER TABLE c2_generated ADD COLUMN IF NOT EXISTS format TEXT NOT NULL DEFAULT 'stageless'`,
				`ALTER TABLE c2_generated ADD COLUMN IF NOT EXISTS artifact TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE c2_generated ADD COLUMN IF NOT EXISTS size INTEGER NOT NULL DEFAULT 0`,
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		Version: 14,
		Name:    "c2_task_approval",
		Apply: func(tx *sql.Tx) error {
			stmts := []string{
				`ALTER TABLE c2_tasks ADD COLUMN IF NOT EXISTS approval TEXT NOT NULL DEFAULT 'approved'`,
				`CREATE INDEX IF NOT EXISTS idx_c2_tasks_approval ON c2_tasks(approval, state)`,
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		Version: 15,
		Name:    "audit_hash_chain",
		Apply: func(tx *sql.Tx) error {
			// 审计哈希链列(soc-autopilot 风格防篡改):每条审计的 hash 覆盖上一条 + 本条字段。
			stmts := []string{
				`ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS hash TEXT NOT NULL DEFAULT ''`,
			}
			for _, stmt := range stmts {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			// backfill: 给既有行按 id 顺序补 hash(老库升级不丢链)。
			rows, err := tx.Query(`SELECT id, actor, category, action, result, message, ip, created_at, prev_hash, hash FROM audit_logs ORDER BY id`)
			if err != nil {
				return err
			}
			type row struct {
				id      int64
				actor   string
				cat     string
				action  string
				result  string
				message string
				ip      string
				ts      time.Time
				prev    string
				hash    string
			}
			var items []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.id, &r.actor, &r.cat, &r.action, &r.result, &r.message, &r.ip, &r.ts, &r.prev, &r.hash); err != nil {
					rows.Close()
					return err
				}
				items = append(items, r)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			prev := ""
			for _, r := range items {
				if r.hash != "" {
					prev = r.hash
					continue
				}
				h := auditChainHash(prev, r.actor, r.cat, r.action, r.result, r.message, r.ip, r.ts)
				if _, err := tx.Exec(`UPDATE audit_logs SET prev_hash=$2, hash=$3 WHERE id=$1`, r.id, prev, h); err != nil {
					return err
				}
				prev = h
			}
			return nil
		},
	},
	{
		Version: 16,
		Name:    "llm_profile_session_id",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE llm_profiles ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT ''`)
			return err
		},
	},
	{
		Version: 17,
		Name:    "task_agent_key",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS agent_key TEXT NOT NULL DEFAULT ''`)
			return err
		},
	},
	{
		Version: 18,
		Name:    "finding_poc_packets",
		Apply: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE findings ADD COLUMN IF NOT EXISTS request_raw TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
			_, err := tx.Exec(`ALTER TABLE findings ADD COLUMN IF NOT EXISTS response_raw TEXT NOT NULL DEFAULT ''`)
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
