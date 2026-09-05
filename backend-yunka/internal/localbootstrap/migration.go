package localbootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	MigrationID = "YU-19_local_admin_bootstrap_v1"
	stateID     = "local-admin"
)

const bootstrapStateSchema = `
CREATE TABLE IF NOT EXISTS iotd_local_admin_bootstrap_state (
    id TEXT PRIMARY KEY NOT NULL CHECK (id = 'local-admin'),
    state TEXT NOT NULL CHECK (state = 'closed'),
    close_reason TEXT NOT NULL CHECK (close_reason IN ('initialized', 'preexisting_identity')),
    organization_id TEXT,
    initialized_user_id TEXT,
    closed_at TEXT NOT NULL CHECK (length(trim(closed_at)) > 0),
    created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
    CHECK (
        (close_reason = 'initialized' AND organization_id IS NOT NULL AND length(trim(organization_id)) > 0 AND initialized_user_id IS NOT NULL AND length(trim(initialized_user_id)) > 0)
        OR
        (close_reason = 'preexisting_identity' AND organization_id IS NULL AND initialized_user_id IS NULL)
    )
);
CREATE TRIGGER IF NOT EXISTS iotd_local_admin_bootstrap_state_immutable_update
BEFORE UPDATE ON iotd_local_admin_bootstrap_state
BEGIN
    SELECT RAISE(ABORT, 'local administrator bootstrap state is immutable');
END;
CREATE TRIGGER IF NOT EXISTS iotd_local_admin_bootstrap_state_immutable_delete
BEFORE DELETE ON iotd_local_admin_bootstrap_state
BEGIN
    SELECT RAISE(ABORT, 'local administrator bootstrap state is immutable');
END;`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("local bootstrap SQLite database is required")
	}
	if err := requireDependencies(ctx, database); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable local bootstrap foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local bootstrap migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create local bootstrap migration ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, bootstrapStateSchema); err != nil {
		return fmt.Errorf("apply local bootstrap state schema: %w", err)
	}
	if err := verifyBootstrapStateSchema(ctx, tx); err != nil {
		return err
	}
	if err := closeForPreexistingIdentity(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read local bootstrap migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record local bootstrap migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("local bootstrap migration ledger is invalid")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local bootstrap migration: %w", err)
	}
	return nil
}

func requireDependencies(ctx context.Context, database *sql.DB) error {
	for _, table := range []string{"users", "roles", "role_bindings", "iotd_local_user_credentials"} {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect local bootstrap dependency %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("local bootstrap migration requires %s", table)
		}
	}
	var bindingScope string
	if err := database.QueryRowContext(ctx, `SELECT binding_scope FROM roles WHERE id = 'system-administrator'`).Scan(&bindingScope); err != nil {
		return errors.New("local bootstrap migration requires the system-administrator role")
	}
	if bindingScope != "organization" {
		return errors.New("local bootstrap system-administrator role has invalid scope")
	}
	return nil
}

type schemaColumn struct {
	name     string
	typeName string
	notNull  int
	pk       int
}

func verifyBootstrapStateSchema(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('iotd_local_admin_bootstrap_state')`)
	if err != nil {
		return fmt.Errorf("inspect local bootstrap state schema: %w", err)
	}
	defer rows.Close()
	columns := make([]schemaColumn, 0, 7)
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan local bootstrap state schema: %w", err)
		}
		columns = append(columns, schemaColumn{name: name, typeName: strings.ToUpper(typeName), notNull: notNull, pk: pk})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate local bootstrap state schema: %w", err)
	}
	want := []schemaColumn{
		{name: "id", typeName: "TEXT", notNull: 1, pk: 1},
		{name: "state", typeName: "TEXT", notNull: 1},
		{name: "close_reason", typeName: "TEXT", notNull: 1},
		{name: "organization_id", typeName: "TEXT"},
		{name: "initialized_user_id", typeName: "TEXT"},
		{name: "closed_at", typeName: "TEXT", notNull: 1},
		{name: "created_at", typeName: "TEXT", notNull: 1},
	}
	if len(columns) != len(want) {
		return fmt.Errorf("local bootstrap state column count = %d, want %d", len(columns), len(want))
	}
	for index := range want {
		if columns[index] != want[index] {
			return fmt.Errorf("local bootstrap state column %d = %#v, want %#v", index, columns[index], want[index])
		}
	}
	var triggerCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ('iotd_local_admin_bootstrap_state_immutable_update', 'iotd_local_admin_bootstrap_state_immutable_delete')`).Scan(&triggerCount); err != nil {
		return fmt.Errorf("inspect local bootstrap state triggers: %w", err)
	}
	if triggerCount != 2 {
		return errors.New("local bootstrap state immutability triggers are incomplete")
	}
	return verifyClosedState(ctx, tx)
}

func verifyClosedState(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_local_admin_bootstrap_state`).Scan(&count); err != nil {
		return fmt.Errorf("count local bootstrap state rows: %w", err)
	}
	if count == 0 {
		return nil
	}
	if count != 1 {
		return errors.New("local bootstrap state has multiple rows")
	}
	var id, state, reason, organizationID, userID, closedAt, createdAt string
	if err := tx.QueryRowContext(ctx, `SELECT id, state, close_reason, COALESCE(organization_id, ''), COALESCE(initialized_user_id, ''), closed_at, created_at FROM iotd_local_admin_bootstrap_state`).Scan(&id, &state, &reason, &organizationID, &userID, &closedAt, &createdAt); err != nil {
		return fmt.Errorf("read local bootstrap state: %w", err)
	}
	if id != stateID || state != "closed" || closedAt == "" || createdAt == "" {
		return errors.New("local bootstrap state row is invalid")
	}
	switch reason {
	case "initialized":
		if !canonicalIdentifier(organizationID) || !canonicalIdentifier(userID) {
			return errors.New("initialized local bootstrap state is invalid")
		}
	case "preexisting_identity":
		if organizationID != "" || userID != "" {
			return errors.New("preexisting local bootstrap state is invalid")
		}
	default:
		return errors.New("local bootstrap close reason is invalid")
	}
	return nil
}

func closeForPreexistingIdentity(ctx context.Context, tx *sql.Tx) error {
	var stateCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID).Scan(&stateCount); err != nil {
		return fmt.Errorf("read existing local bootstrap state: %w", err)
	}
	if stateCount != 0 {
		return nil
	}
	var userCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return fmt.Errorf("inspect preexisting identity before local bootstrap: %w", err)
	}
	if userCount == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO iotd_local_admin_bootstrap_state (id, state, close_reason, organization_id, initialized_user_id, closed_at, created_at)
VALUES (?, 'closed', 'preexisting_identity', NULL, NULL, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, stateID)
	if err != nil {
		return fmt.Errorf("close local bootstrap for preexisting identity: %w", err)
	}
	return nil
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}
