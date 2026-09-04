// Package configrevision persists immutable, organization-scoped configuration
// snapshots. It deliberately exposes no configuration-change business operation.
package configrevision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const MigrationID = "S0-04-05_config_revisions_v1"

const configRevisionSchema = `
CREATE TABLE IF NOT EXISTS iotd_config_revisions (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    kind TEXT NOT NULL CHECK (kind IN ('identity_provider', 'membership', 'role_binding', 'domain_dictionary')),
    config_key TEXT NOT NULL CHECK (length(trim(config_key)) > 0),
    revision INTEGER NOT NULL CHECK (revision > 0),
    parent_revision INTEGER NOT NULL CHECK (parent_revision >= 0 AND revision = parent_revision + 1),
    payload TEXT NOT NULL CHECK (json_valid(payload) AND json_type(payload) = 'object'),
    payload_hash BLOB NOT NULL CHECK (length(payload_hash) = 32),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('human', 'service', 'system')),
    created_by_id TEXT NOT NULL CHECK (length(trim(created_by_id)) > 0),
    created_at TEXT NOT NULL CHECK (created_at GLOB '????-??-??T??:??:??*Z' AND strftime('%s', created_at) IS NOT NULL),
    UNIQUE (organization_id, kind, config_key, revision),
    UNIQUE (organization_id, kind, config_key, parent_revision)
);
CREATE INDEX IF NOT EXISTS idx_iotd_config_revisions_lookup ON iotd_config_revisions (organization_id, kind, config_key, revision DESC);
CREATE TRIGGER IF NOT EXISTS iotd_config_revisions_append_only_update
BEFORE UPDATE ON iotd_config_revisions
BEGIN
    SELECT RAISE(ABORT, 'config revisions are append-only');
END;
CREATE TRIGGER IF NOT EXISTS iotd_config_revisions_append_only_delete
BEFORE DELETE ON iotd_config_revisions
BEGIN
    SELECT RAISE(ABORT, 'config revisions are append-only');
END;`

var requiredConfigRevisionColumns = []string{
	"id", "organization_id", "kind", "config_key", "revision", "parent_revision",
	"payload", "payload_hash", "created_by_type", "created_by_id", "created_at",
}

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("config revision SQLite database is required")
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable config revision foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin config revision migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create config revision migration ledger: %w", err)
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read config revision migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, configRevisionSchema); err != nil {
			return fmt.Errorf("apply config revision schema: %w", err)
		}
		if err := verifySchema(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record config revision migration: %w", err)
		}
	}
	if err := verifySchema(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config revision migration: %w", err)
	}
	return nil
}

func verifySchema(ctx context.Context, tx *sql.Tx) error {
	var table string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'iotd_config_revisions'`).Scan(&table); err != nil {
		return fmt.Errorf("verify config revision table: %w", err)
	}
	if err := verifyNames(ctx, tx, `SELECT name FROM pragma_table_info('iotd_config_revisions')`, requiredConfigRevisionColumns, "column"); err != nil {
		return err
	}
	if err := verifyTableSemantics(ctx, tx); err != nil {
		return err
	}
	if err := verifyForeignKeySemantics(ctx, tx); err != nil {
		return err
	}
	if err := verifyIndexSemantics(ctx, tx); err != nil {
		return err
	}
	return verifyTriggerSemantics(ctx, tx)
}

func verifyTableSemantics(ctx context.Context, tx *sql.Tx) error {
	var definition string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'iotd_config_revisions'`).Scan(&definition); err != nil {
		return fmt.Errorf("read config revision table definition: %w", err)
	}
	expected := strings.Split(configRevisionSchema, ";")[0]
	expected = strings.Replace(expected, "CREATE TABLE IF NOT EXISTS", "CREATE TABLE", 1)
	if normalizeSchemaSQL(definition) != normalizeSchemaSQL(expected) {
		return errors.New("config revision table schema drift detected")
	}
	return nil
}

func verifyForeignKeySemantics(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list('iotd_config_revisions')`)
	if err != nil {
		return fmt.Errorf("read config revision foreign keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, from, to, onDelete string
		if err := rows.Scan(&table, &from, &to, &onDelete); err != nil {
			return fmt.Errorf("scan config revision foreign key: %w", err)
		}
		if table == "organizations" && from == "organization_id" && to == "id" && strings.EqualFold(onDelete, "RESTRICT") {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate config revision foreign keys: %w", err)
	}
	return errors.New("config revision organization foreign key schema drift detected")
}

func verifyIndexSemantics(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT name, "unique" FROM pragma_index_list('iotd_config_revisions')`)
	if err != nil {
		return fmt.Errorf("read config revision indexes: %w", err)
	}
	defer rows.Close()
	foundLookup, foundRevisionChain, foundParentChain := false, false, false
	for rows.Next() {
		var name string
		var unique int
		if err := rows.Scan(&name, &unique); err != nil {
			return fmt.Errorf("scan config revision index: %w", err)
		}
		columns, err := indexColumns(ctx, tx, name)
		if err != nil {
			return err
		}
		switch {
		case name == "idx_iotd_config_revisions_lookup":
			foundLookup = equalColumns(columns, []string{"organization_id", "kind", "config_key", "revision"}) && indexDescending(ctx, tx, name)
		case unique == 1 && equalColumns(columns, []string{"organization_id", "kind", "config_key", "revision"}):
			foundRevisionChain = true
		case unique == 1 && equalColumns(columns, []string{"organization_id", "kind", "config_key", "parent_revision"}):
			foundParentChain = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate config revision indexes: %w", err)
	}
	if !foundLookup || !foundRevisionChain || !foundParentChain {
		return errors.New("config revision index schema drift detected")
	}
	return nil
}

func indexDescending(ctx context.Context, tx *sql.Tx, name string) bool {
	rows, err := tx.QueryContext(ctx, `SELECT "desc" FROM pragma_index_xinfo(?) WHERE key = 1 ORDER BY seqno ASC`, name)
	if err != nil {
		return false
	}
	defer rows.Close()
	want := []int{0, 0, 0, 1}
	index := 0
	for rows.Next() {
		var descending int
		if err := rows.Scan(&descending); err != nil || index >= len(want) || descending != want[index] {
			return false
		}
		index++
	}
	return rows.Err() == nil && index == len(want)
}

func indexColumns(ctx context.Context, tx *sql.Tx, name string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno ASC`, name)
	if err != nil {
		return nil, fmt.Errorf("read config revision index columns: %w", err)
	}
	defer rows.Close()
	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("scan config revision index column: %w", err)
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func verifyTriggerSemantics(ctx context.Context, tx *sql.Tx) error {
	for name, expected := range map[string]string{
		"iotd_config_revisions_append_only_update": "CREATE TRIGGER iotd_config_revisions_append_only_update BEFORE UPDATE ON iotd_config_revisions BEGIN SELECT RAISE(ABORT, 'config revisions are append-only'); END",
		"iotd_config_revisions_append_only_delete": "CREATE TRIGGER iotd_config_revisions_append_only_delete BEFORE DELETE ON iotd_config_revisions BEGIN SELECT RAISE(ABORT, 'config revisions are append-only'); END",
	} {
		var definition string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ? AND tbl_name = 'iotd_config_revisions'`, name).Scan(&definition); err != nil {
			return fmt.Errorf("read config revision trigger: %w", err)
		}
		if normalizeSchemaSQL(definition) != normalizeSchemaSQL(expected) {
			return errors.New("config revision trigger schema drift detected")
		}
	}
	return nil
}

func normalizeSchemaSQL(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, strings.ToLower(value))
}

func equalColumns(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func verifyNames(ctx context.Context, tx *sql.Tx, query string, required []string, kind string) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read config revision %s metadata: %w", kind, err)
	}
	defer rows.Close()
	found := make(map[string]bool, len(required))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan config revision %s metadata: %w", kind, err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate config revision %s metadata: %w", kind, err)
	}
	for _, name := range required {
		if !found[name] {
			return fmt.Errorf("config revision schema is missing required %s %q", kind, name)
		}
	}
	return nil
}
