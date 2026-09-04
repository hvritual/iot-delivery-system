package configrevision

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	_ "modernc.org/sqlite"
)

func TestApplyMigrationsCreatesImmutableConfigRevisionSchema(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply config revision migrations: %v", err)
	}

	for _, column := range []string{"id", "organization_id", "kind", "config_key", "revision", "parent_revision", "payload", "payload_hash", "created_by_type", "created_by_id", "created_at"} {
		var found string
		if err := database.QueryRow(`SELECT name FROM pragma_table_info('iotd_config_revisions') WHERE name = ?`, column).Scan(&found); err != nil {
			t.Fatalf("config revision column %q is required: %v", column, err)
		}
	}
	for _, trigger := range []string{"iotd_config_revisions_append_only_update", "iotd_config_revisions_append_only_delete"} {
		var found string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&found); err != nil {
			t.Fatalf("config revision trigger %q is required: %v", trigger, err)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("config revision migration ledger rows = %d error=%v, want 1", count, err)
	}
	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat config revision migrations: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotent config revision migration ledger rows = %d error=%v, want 1", count, err)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-immutable', 'org-immutable', 'Immutable')`); err != nil {
		t.Fatalf("insert organization for append-only check: %v", err)
	}
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("create config revision store: %v", err)
	}
	if _, err := store.Append(context.Background(), AppendInput{ID: "immutable-revision", OrganizationID: "org-immutable", Kind: KindIdentityProvider, ConfigKey: "default", Payload: `{}`, CreatedByType: CreatedBySystem, CreatedByID: "migration-test"}); err != nil {
		t.Fatalf("append revision for append-only check: %v", err)
	}
	if _, err := database.Exec(`UPDATE iotd_config_revisions SET config_key = 'tampered'`); err == nil {
		t.Fatal("config revision UPDATE unexpectedly succeeded")
	}
	if _, err := database.Exec(`DELETE FROM iotd_config_revisions`); err == nil {
		t.Fatal("config revision DELETE unexpectedly succeeded")
	}
}

func TestApplyMigrationsFailsClosedOnLedgerDriftAndRollsBackConflict(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL); INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES ('S0-04-05_config_revisions_v1', '2026-09-04T00:00:00Z')`); err != nil {
		t.Fatalf("prepare drift ledger: %v", err)
	}
	if err := ApplyMigrations(context.Background(), database); err == nil {
		t.Fatal("ledger-present schema drift unexpectedly migrated")
	}

	conflict, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conflict.Close() })
	if _, err := conflict.Exec(`CREATE TABLE iotd_config_revisions (id TEXT);`); err != nil {
		t.Fatalf("prepare conflicting table: %v", err)
	}
	if err := ApplyMigrations(context.Background(), conflict); err == nil {
		t.Fatal("conflicting config revision schema unexpectedly migrated")
	}
	var migrationTable string
	err = conflict.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'iotd_schema_migrations'`).Scan(&migrationTable)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("migration conflict must roll back shared ledger, found=%q error=%v", migrationTable, err)
	}
}

func TestApplyMigrationsPreservesExistingDeliveryAndAuditRecords(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
CREATE TABLE iotd_delivery_items (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE iotd_audit_entries (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
INSERT INTO iotd_delivery_items (id, payload) VALUES ('delivery-before', '{"keep":true}');
INSERT INTO iotd_audit_entries (id, payload) VALUES ('audit-before', '{"keep":true}');`); err != nil {
		t.Fatalf("prepare existing records: %v", err)
	}
	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply config revision migrations: %v", err)
	}
	for _, check := range []struct {
		query string
		want  string
	}{
		{query: `SELECT payload FROM iotd_delivery_items WHERE id = 'delivery-before'`, want: `{"keep":true}`},
		{query: `SELECT payload FROM iotd_audit_entries WHERE id = 'audit-before'`, want: `{"keep":true}`},
	} {
		var got string
		if err := database.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("existing record readback = %q error=%v, want %q", got, err, check.want)
		}
	}
}

func TestServiceGrantSchemaDriftFailsClosed(t *testing.T) {
	for name, mutate := range map[string]string{
		"ledgered missing table":   `DROP TABLE iotd_config_service_grants`,
		"table definition drift":   `ALTER TABLE iotd_config_service_grants ADD COLUMN drift TEXT`,
		"valid trigger when zero":  `DROP TRIGGER iotd_config_service_grants_valid_on_insert; CREATE TRIGGER iotd_config_service_grants_valid_on_insert BEFORE INSERT ON iotd_config_service_grants WHEN 0 BEGIN SELECT RAISE(ABORT, 'config service grant is invalid'); END`,
		"immutable trigger fields": `DROP TRIGGER iotd_config_service_grants_append_only; CREATE TRIGGER iotd_config_service_grants_append_only BEFORE UPDATE OF status ON iotd_config_service_grants BEGIN SELECT RAISE(ABORT, 'config service grants are immutable'); END`,
	} {
		t.Run(name, func(t *testing.T) {
			database := migratedDatabase(t, ":memory:")
			if err := ApplyMigrations(t.Context(), database); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(mutate); err != nil {
				t.Fatal(err)
			}
			if err := ApplyMigrations(t.Context(), database); err == nil {
				t.Fatal("service grant drift unexpectedly accepted")
			}
		})
	}
}

func TestConfigAuthorizationSeedDriftFailsClosed(t *testing.T) {
	mutations := map[string]string{
		"missing permission":           `PRAGMA foreign_keys = OFF; DELETE FROM permissions WHERE id = 'config.revisions.write'; PRAGMA foreign_keys = ON`,
		"permission wrong resource":    `UPDATE permissions SET resource = 'config.other' WHERE id = 'config.revisions.write'`,
		"permission wrong action":      `UPDATE permissions SET action = 'read' WHERE id = 'config.revisions.write'`,
		"permission wrong status":      `UPDATE permissions SET status = 'reserved' WHERE id = 'config.revisions.write'`,
		"missing permission scope":     `PRAGMA foreign_keys = OFF; DELETE FROM permission_allowed_scopes WHERE permission_id = 'config.revisions.read'; PRAGMA foreign_keys = ON`,
		"extra permission scope":       `INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES ('config.revisions.read', 'project')`,
		"missing administrator grant":  `PRAGMA foreign_keys = OFF; DELETE FROM role_permission_grant_allowed_scopes WHERE role_id = 'system-administrator' AND permission_id = 'config.revisions.rollback'; DELETE FROM role_permission_grants WHERE role_id = 'system-administrator' AND permission_id = 'config.revisions.rollback'; PRAGMA foreign_keys = ON`,
		"missing administrator scope":  `DELETE FROM role_permission_grant_allowed_scopes WHERE role_id = 'system-administrator' AND permission_id = 'config.revisions.write'`,
		"extra administrator scope":    `PRAGMA foreign_keys = OFF; INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES ('system-administrator', 'config.revisions.read', 'project'); PRAGMA foreign_keys = ON`,
		"missing service operation":    `DROP TRIGGER service_operations_immutable_on_delete; DELETE FROM service_operations WHERE id = 'config.revisions.compare'`,
		"service operation permission": `DROP TRIGGER service_operations_immutable_on_update; UPDATE service_operations SET permission_id = 'config.revisions.read' WHERE id = 'config.revisions.change'`,
		"service operation scope":      `DROP TRIGGER service_operations_immutable_on_update; UPDATE service_operations SET required_scope = 'project' WHERE id = 'config.revisions.change'`,
		"service operation status":     `DROP TRIGGER service_operations_immutable_on_update; UPDATE service_operations SET status = 'disabled' WHERE id = 'config.revisions.change'`,
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			database := migratedDatabase(t, ":memory:")
			if err := ApplyMigrations(t.Context(), database); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if err := ApplyMigrations(t.Context(), database); err == nil {
				t.Fatal("seed drift unexpectedly accepted")
			}
		})
	}
}

func TestServiceGrantMigrationWaitsForIdentityThenSeeds(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ServiceGrantMigrationID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("premature service ledger=%d error=%v", count, err)
	}
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ServiceGrantMigrationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("service ledger=%d error=%v", count, err)
	}
}
