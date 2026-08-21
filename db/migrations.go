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
