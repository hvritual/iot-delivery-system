package localmemberadmin

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	_ "modernc.org/sqlite"
)

func TestYU20MigrationUpgradesExistingUsersAndIsRepeatable(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-existing', 'org-existing', 'Existing')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name) VALUES ('user-existing', 'org-existing', 'Existing User')`); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := database.QueryRow(`SELECT revision FROM users WHERE id = 'user-existing'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("upgraded user revision=%d error=%v, want 1", revision, err)
	}
	var ledger int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("migration ledger=%d error=%v, want 1", ledger, err)
	}
	var grantCount, scopeCount, triggerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE permission_id = ? AND role_id = 'system-administrator'`, PermissionManageUsers).Scan(&grantCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE permission_id = ? AND role_id <> 'system-administrator'`, PermissionManageUsers).Scan(&scopeCount); err != nil {
		t.Fatal(err)
	}
	if grantCount != 1 || scopeCount != 0 {
		t.Fatalf("member admin grants system=%d other=%d", grantCount, scopeCount)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name = 'users_preserve_last_system_administrator'`).Scan(&triggerCount); err != nil || triggerCount != 1 {
		t.Fatalf("last administrator trigger count=%d error=%v", triggerCount, err)
	}
}

func TestYU20MigrationRejectsForgedLastAdministratorTrigger(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER users_preserve_last_system_administrator BEFORE UPDATE OF status ON users BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged no-op last administrator trigger passed migration verification")
	}
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "yu20-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}
