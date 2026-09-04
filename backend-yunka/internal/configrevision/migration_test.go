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
