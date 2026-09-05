package localcredential

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const MigrationID = "YU-18_local_user_credentials_v1"

const credentialSchema = `
CREATE TABLE IF NOT EXISTS iotd_local_user_credentials (
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    user_id TEXT NOT NULL CHECK (length(trim(user_id)) > 0),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    policy_version INTEGER NOT NULL CHECK (policy_version >= 1),
    algorithm TEXT NOT NULL CHECK (algorithm = 'argon2id'),
    argon_version INTEGER NOT NULL CHECK (argon_version = 19),
    memory_kib INTEGER NOT NULL CHECK (memory_kib >= 7168 AND memory_kib <= 1048576),
    iterations INTEGER NOT NULL CHECK (iterations >= 1 AND iterations <= 100),
    parallelism INTEGER NOT NULL CHECK (parallelism = 1),
    salt BLOB NOT NULL CHECK (length(salt) BETWEEN 16 AND 64),
    password_hash BLOB NOT NULL CHECK (length(password_hash) BETWEEN 32 AND 64),
    created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
    updated_at TEXT NOT NULL CHECK (length(trim(updated_at)) > 0),
    PRIMARY KEY (organization_id, user_id),
    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT
);`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("local credential SQLite database is required")
	}
	if err := requireIdentityUserSchema(ctx, database); err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable local credential foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local credential migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create local credential migration ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, credentialSchema); err != nil {
		return fmt.Errorf("apply local credential schema: %w", err)
	}
	if err := verifyCredentialSchema(ctx, tx); err != nil {
		return err
	}
	if err := verifyCredentialForeignKey(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read local credential migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record local credential migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("local credential migration ledger is invalid")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local credential migration: %w", err)
	}
	return nil
}

func requireIdentityUserSchema(ctx context.Context, database *sql.DB) error {
	var sqlText string
	if err := database.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&sqlText); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("local credential migration requires the identity user schema")
		}
		return fmt.Errorf("inspect identity user schema: %w", err)
	}
	if !strings.Contains(sqlText, "UNIQUE (organization_id, id)") {
		return errors.New("local credential migration requires canonical tenant-bound users")
	}
	return nil
}

type schemaColumn struct {
	name     string
	typeName string
	notNull  int
	pk       int
}

func verifyCredentialSchema(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('iotd_local_user_credentials')`)
	if err != nil {
		return fmt.Errorf("inspect local credential schema: %w", err)
	}
	defer rows.Close()
	columns := make([]schemaColumn, 0, 13)
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan local credential schema: %w", err)
		}
		columns = append(columns, schemaColumn{name: name, typeName: strings.ToUpper(typeName), notNull: notNull, pk: pk})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate local credential schema: %w", err)
	}
	want := []schemaColumn{
		{name: "organization_id", typeName: "TEXT", notNull: 1, pk: 1},
		{name: "user_id", typeName: "TEXT", notNull: 1, pk: 2},
		{name: "revision", typeName: "INTEGER", notNull: 1},
		{name: "policy_version", typeName: "INTEGER", notNull: 1},
		{name: "algorithm", typeName: "TEXT", notNull: 1},
		{name: "argon_version", typeName: "INTEGER", notNull: 1},
		{name: "memory_kib", typeName: "INTEGER", notNull: 1},
		{name: "iterations", typeName: "INTEGER", notNull: 1},
		{name: "parallelism", typeName: "INTEGER", notNull: 1},
		{name: "salt", typeName: "BLOB", notNull: 1},
		{name: "password_hash", typeName: "BLOB", notNull: 1},
		{name: "created_at", typeName: "TEXT", notNull: 1},
		{name: "updated_at", typeName: "TEXT", notNull: 1},
	}
	if len(columns) != len(want) {
		return fmt.Errorf("local credential schema column count = %d, want %d", len(columns), len(want))
	}
	for index := range want {
		if columns[index] != want[index] {
			return fmt.Errorf("local credential schema column %d = %#v, want %#v", index, columns[index], want[index])
		}
	}
	return nil
}

type foreignKeyColumn struct {
	sequence int
	table    string
	from     string
	to       string
	onDelete string
}

func verifyCredentialForeignKey(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_list('iotd_local_user_credentials')`)
	if err != nil {
		return fmt.Errorf("inspect local credential foreign key: %w", err)
	}
	defer rows.Close()
	columns := make([]foreignKeyColumn, 0, 2)
	for rows.Next() {
		var id, sequence int
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &sequence, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return fmt.Errorf("scan local credential foreign key: %w", err)
		}
		if id != 0 {
			return errors.New("local credential schema contains an unexpected foreign key")
		}
		columns = append(columns, foreignKeyColumn{sequence: sequence, table: table, from: from, to: to, onDelete: onDelete})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate local credential foreign key: %w", err)
	}
	want := []foreignKeyColumn{
		{sequence: 0, table: "users", from: "organization_id", to: "organization_id", onDelete: "RESTRICT"},
		{sequence: 1, table: "users", from: "user_id", to: "id", onDelete: "RESTRICT"},
	}
	if len(columns) != len(want) {
		return errors.New("local credential schema is missing the tenant-bound user foreign key")
	}
	for index := range want {
		if columns[index] != want[index] {
			return errors.New("local credential schema has an invalid tenant-bound user foreign key")
		}
	}
	return nil
}
