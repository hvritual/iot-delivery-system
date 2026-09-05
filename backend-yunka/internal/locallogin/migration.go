package locallogin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const MigrationID = "YU-21_local_sessions_v1"

const sessionSchema = `
CREATE TABLE IF NOT EXISTS iotd_local_sessions (
    id TEXT PRIMARY KEY NOT NULL,
    organization_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    secret_digest BLOB NOT NULL UNIQUE CHECK (length(secret_digest) = 32),
    status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
    credential_revision INTEGER NOT NULL CHECK (credential_revision >= 1),
    created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
    expires_at TEXT NOT NULL CHECK (length(trim(expires_at)) > 0),
    revoked_at TEXT,
    CHECK (expires_at > created_at),
    CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL)),
    FOREIGN KEY (organization_id, user_id)
        REFERENCES users(organization_id, id)
        ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_iotd_local_sessions_user_active
    ON iotd_local_sessions (organization_id, user_id, status, expires_at);`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("local login SQLite database is required")
	}
	for _, table := range []string{"users", "iotd_local_user_credentials", "iotd_schema_migrations"} {
		var count int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect local login dependency %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("local login migration requires %s", table)
		}
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable local login foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local login migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, sessionSchema); err != nil {
		return fmt.Errorf("apply local session schema: %w", err)
	}
	if err := verifySessionSchema(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read local login migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record local login migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("local login migration ledger is invalid")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local login migration: %w", err)
	}
	return nil
}

type sessionColumn struct {
	name     string
	typeName string
	notNull  int
	pk       int
}

func verifySessionSchema(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('iotd_local_sessions')`)
	if err != nil {
		return fmt.Errorf("inspect local session schema: %w", err)
	}
	defer rows.Close()
	columns := make([]sessionColumn, 0, 9)
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan local session schema: %w", err)
		}
		columns = append(columns, sessionColumn{name: name, typeName: strings.ToUpper(typeName), notNull: notNull, pk: pk})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate local session schema: %w", err)
	}
	want := []sessionColumn{
		{name: "id", typeName: "TEXT", notNull: 1, pk: 1},
		{name: "organization_id", typeName: "TEXT", notNull: 1},
		{name: "user_id", typeName: "TEXT", notNull: 1},
		{name: "secret_digest", typeName: "BLOB", notNull: 1},
		{name: "status", typeName: "TEXT", notNull: 1},
		{name: "credential_revision", typeName: "INTEGER", notNull: 1},
		{name: "created_at", typeName: "TEXT", notNull: 1},
		{name: "expires_at", typeName: "TEXT", notNull: 1},
		{name: "revoked_at", typeName: "TEXT"},
	}
	if len(columns) != len(want) {
		return fmt.Errorf("local session column count = %d, want %d", len(columns), len(want))
	}
	for index := range want {
		if columns[index] != want[index] {
			return fmt.Errorf("local session column %d = %#v, want %#v", index, columns[index], want[index])
		}
	}
	var tableSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'iotd_local_sessions'`).Scan(&tableSQL); err != nil {
		return errors.New("local session physical schema is missing")
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(tableSQL), " "))
	for _, required := range []string{
		"secret_digest blob not null unique check (length(secret_digest) = 32)",
		"status text not null check (status in ('active', 'revoked'))",
		"credential_revision integer not null check (credential_revision >= 1)",
		"check (expires_at > created_at)",
		"status = 'active' and revoked_at is null",
		"status = 'revoked' and revoked_at is not null",
		"foreign key (organization_id, user_id) references users(organization_id, id) on delete restrict",
	} {
		if !strings.Contains(normalized, required) {
			return errors.New("local session physical schema contract is invalid")
		}
	}
	fkRows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_list('iotd_local_sessions')`)
	if err != nil {
		return fmt.Errorf("inspect local session foreign key: %w", err)
	}
	defer fkRows.Close()
	orgFK, userFK := -1, -2
	for fkRows.Next() {
		var id, seq int
		var table, from, to, onUpdate, onDelete, match string
		if err := fkRows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return fmt.Errorf("scan local session foreign key: %w", err)
		}
		if table != "users" || strings.ToUpper(onDelete) != "RESTRICT" {
			continue
		}
		if from == "organization_id" && to == "organization_id" {
			orgFK = id
		}
		if from == "user_id" && to == "id" {
			userFK = id
		}
	}
	if orgFK < 0 || userFK < 0 || orgFK != userFK {
		return errors.New("local session tenant/User composite foreign key is invalid")
	}
	var indexSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_iotd_local_sessions_user_active'`).Scan(&indexSQL); err != nil {
		return errors.New("local session active-user index is missing")
	}
	indexNormalized := strings.ToLower(strings.Join(strings.Fields(indexSQL), " "))
	if !strings.Contains(indexNormalized, "on iotd_local_sessions (organization_id, user_id, status, expires_at)") {
		return errors.New("local session active-user index contract is invalid")
	}
	return nil
}
