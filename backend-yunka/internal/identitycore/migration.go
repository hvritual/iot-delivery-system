package identitycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const MigrationID = "S0-02-01_identity_core_v1"

const identitySchema = `
CREATE TABLE organizations (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    slug TEXT NOT NULL UNIQUE CHECK (length(trim(slug)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE users (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) > 0),
    email TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    UNIQUE (organization_id, id)
);
CREATE TABLE external_identities (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    user_id TEXT NOT NULL CHECK (length(trim(user_id)) > 0),
    issuer TEXT NOT NULL CHECK (length(trim(issuer)) > 0),
    subject TEXT NOT NULL CHECK (length(trim(subject)) > 0),
    email_snapshot TEXT,
    display_name_snapshot TEXT,
    last_seen_at TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT,
    UNIQUE (issuer, subject)
);
CREATE TABLE service_accounts (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    UNIQUE (organization_id, name)
);`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("identity SQLite database is required")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin identity migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create identity migration ledger: %w", err)
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read identity migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, identitySchema); err != nil {
			return fmt.Errorf("apply identity core schema: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id) VALUES (?)`, MigrationID); err != nil {
			return fmt.Errorf("record identity migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit identity migration: %w", err)
	}
	return nil
}
