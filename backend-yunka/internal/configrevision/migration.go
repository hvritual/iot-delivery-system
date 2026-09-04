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

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
)

const MigrationID = "S0-04-05_config_revisions_v1"
const ServiceGrantMigrationID = "S0-04-06_config_service_grants_v1"

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

const configServiceGrantSchema = `
CREATE TABLE iotd_config_service_grants (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    service_account_id TEXT NOT NULL REFERENCES service_accounts(id) ON DELETE RESTRICT,
    operation_id TEXT NOT NULL REFERENCES service_operations(id) ON DELETE RESTRICT,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    revoked_at TEXT,
    CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL)),
    UNIQUE (service_account_id, operation_id, permission_id)
);
CREATE TRIGGER iotd_config_service_grants_valid_on_insert
BEFORE INSERT ON iotd_config_service_grants
WHEN NOT EXISTS (
    SELECT 1 FROM service_accounts accounts
    JOIN service_operations operations ON operations.id = NEW.operation_id AND operations.id GLOB 'config.revisions.*' AND operations.permission_id = NEW.permission_id AND operations.required_scope = 'organization' AND operations.status = 'active'
    JOIN permissions permissions ON permissions.id = NEW.permission_id AND permissions.status = 'active'
    WHERE accounts.id = NEW.service_account_id AND accounts.organization_id = NEW.organization_id AND accounts.status = 'active'
)
BEGIN SELECT RAISE(ABORT, 'config service grant is invalid'); END;
CREATE TRIGGER iotd_config_service_grants_append_only
BEFORE UPDATE OF id, organization_id, service_account_id, operation_id, permission_id ON iotd_config_service_grants
BEGIN SELECT RAISE(ABORT, 'config service grants are immutable'); END;`

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
	var authorizationTable string
	err = tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'permissions'`).Scan(&authorizationTable)
	if errors.Is(err, sql.ErrNoRows) {
		// Identity authorization is a prerequisite only for the S0-04-06
		// service-grant extension. Do not record that migration before it can
		// be fully seeded; a later call after identity setup will complete it.
		return tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("inspect configuration authorization schema: %w", err)
	}
	var serviceApplied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ServiceGrantMigrationID).Scan(&serviceApplied); err != nil {
		return fmt.Errorf("read config service grant migration ledger: %w", err)
	}
	if serviceApplied == 0 {
		if _, err := tx.ExecContext(ctx, configServiceGrantSchema); err != nil {
			return fmt.Errorf("apply config service grant schema: %w", err)
		}
		if err := seedConfigAuthorization(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, ServiceGrantMigrationID); err != nil {
			return fmt.Errorf("record config service grant migration: %w", err)
		}
	}
	if err := verifyConfigServiceGrantSchema(ctx, tx); err != nil {
		return err
	}
	if err := verifyConfigAuthorizationSeed(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config revision migration: %w", err)
	}
	return nil
}

func verifyConfigAuthorizationSeed(ctx context.Context, tx *sql.Tx) error {
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return err
	}
	for _, permission := range dictionary.Permissions {
		if !strings.HasPrefix(permission.ID, "config.revisions.") {
			continue
		}
		var resource, action, status string
		if err := tx.QueryRowContext(ctx, `SELECT resource, action, status FROM permissions WHERE id = ?`, permission.ID).Scan(&resource, &action, &status); err != nil || resource != permission.Resource || action != permission.Action || status != permission.Status {
			return errors.New("config permission seed drift detected")
		}
		var scopes int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM permission_allowed_scopes WHERE permission_id = ? AND scope_type = 'organization'`, permission.ID).Scan(&scopes); err != nil || scopes != 1 {
			return errors.New("config permission scope seed drift detected")
		}
		var extras int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM permission_allowed_scopes WHERE permission_id = ? AND scope_type <> 'organization'`, permission.ID).Scan(&extras); err != nil || extras != 0 {
			return errors.New("config permission scope seed drift detected")
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permission_grants WHERE role_id = 'system-administrator' AND permission_id = ?`, permission.ID).Scan(&scopes); err != nil || scopes != 1 {
			return errors.New("config administrator grant seed drift detected")
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permission_grant_allowed_scopes WHERE role_id = 'system-administrator' AND permission_id = ? AND scope_type = 'organization'`, permission.ID).Scan(&scopes); err != nil || scopes != 1 {
			return errors.New("config administrator scope seed drift detected")
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permission_grant_allowed_scopes WHERE role_id = 'system-administrator' AND permission_id = ? AND scope_type <> 'organization'`, permission.ID).Scan(&extras); err != nil || extras != 0 {
			return errors.New("config administrator scope seed drift detected")
		}
	}
	for _, operation := range dictionary.Operations {
		if !strings.HasPrefix(operation.ID, "config.revisions.") {
			continue
		}
		var permission, scope, status string
		if err := tx.QueryRowContext(ctx, `SELECT permission_id, required_scope, status FROM service_operations WHERE id = ?`, operation.ID).Scan(&permission, &scope, &status); err != nil || permission != operation.Permission || scope != operation.RequiredScope || status != "active" {
			return errors.New("config service operation seed drift detected")
		}
	}
	return nil
}

func verifyConfigServiceGrantSchema(ctx context.Context, tx *sql.Tx) error {
	var definition string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'iotd_config_service_grants'`).Scan(&definition); err != nil {
		return errors.New("config service grant schema is missing")
	}
	expectedTable := strings.TrimRight(strings.TrimSpace(strings.Split(configServiceGrantSchema, "CREATE TRIGGER")[0]), ";")
	if normalizeSchemaSQL(definition) != normalizeSchemaSQL(expectedTable) {
		return errors.New("config service grant table schema drift detected")
	}
	expected := map[string]string{
		"iotd_config_service_grants_valid_on_insert": "CREATE TRIGGER iotd_config_service_grants_valid_on_insert BEFORE INSERT ON iotd_config_service_grants WHEN NOT EXISTS ( SELECT 1 FROM service_accounts accounts JOIN service_operations operations ON operations.id = NEW.operation_id AND operations.id GLOB 'config.revisions.*' AND operations.permission_id = NEW.permission_id AND operations.required_scope = 'organization' AND operations.status = 'active' JOIN permissions permissions ON permissions.id = NEW.permission_id AND permissions.status = 'active' WHERE accounts.id = NEW.service_account_id AND accounts.organization_id = NEW.organization_id AND accounts.status = 'active' ) BEGIN SELECT RAISE(ABORT, 'config service grant is invalid'); END",
		"iotd_config_service_grants_append_only":     "CREATE TRIGGER iotd_config_service_grants_append_only BEFORE UPDATE OF id, organization_id, service_account_id, operation_id, permission_id ON iotd_config_service_grants BEGIN SELECT RAISE(ABORT, 'config service grants are immutable'); END",
	}
	for name, want := range expected {
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&definition); err != nil || normalizeSchemaSQL(definition) != normalizeSchemaSQL(want) {
			return errors.New("config service grant trigger schema drift detected")
		}
	}
	return nil
}

// seedConfigAuthorization is an additive upgrade for databases whose
// S0-03 authorization migration was recorded before configuration permissions
// existed. The embedded dictionary remains the only authority for all values.
func seedConfigAuthorization(ctx context.Context, tx *sql.Tx) error {
	var table string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'permissions'`).Scan(&table); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect configuration authorization schema: %w", err)
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return fmt.Errorf("load configuration authorization dictionary: %w", err)
	}
	for _, permission := range dictionary.Permissions {
		if !strings.HasPrefix(permission.ID, "config.revisions.") {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO permissions (id, resource, action, status) VALUES (?, ?, ?, ?)`, permission.ID, permission.Resource, permission.Action, permission.Status); err != nil {
			return fmt.Errorf("seed configuration permission: %w", err)
		}
		for _, scope := range permission.AllowedScopes {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO permission_allowed_scopes (permission_id, scope_type) VALUES (?, ?)`, permission.ID, scope); err != nil {
				return fmt.Errorf("seed configuration permission scope: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO role_permission_grants (role_id, permission_id) VALUES ('system-administrator', ?)`, permission.ID); err != nil {
				return fmt.Errorf("seed configuration administrator grant: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES ('system-administrator', ?, ?)`, permission.ID, scope); err != nil {
				return fmt.Errorf("seed configuration administrator grant scope: %w", err)
			}
		}
	}
	for _, operation := range dictionary.Operations {
		if !strings.HasPrefix(operation.ID, "config.revisions.") {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO service_operations (id, permission_id, required_scope, status) VALUES (?, ?, ?, 'active')`, operation.ID, operation.Permission, operation.RequiredScope); err != nil {
			return fmt.Errorf("seed configuration service operation: %w", err)
		}
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
