package localprojectroleadmin

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
)

func newMigrationDatabase(t *testing.T) (*delivery.SQLiteRepository, *sql.DB) {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu24-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatal(err)
	}
	return repository, repository.Database()
}

func TestYU24MigrationActivatesPermissionAddsRevisionAndIsRepeatable(t *testing.T) {
	_, database := newMigrationDatabase(t)
	// Simulate an upgraded pre-YU-24 database whose dictionary row was still reserved.
	if _, err := database.Exec(`UPDATE permissions SET status = 'reserved' WHERE id = ?`, PermissionManageRoleBindings); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM permissions WHERE id = ?`, PermissionManageRoleBindings).Scan(&status); err != nil || status != "active" {
		t.Fatalf("permission status=%q error=%v", status, err)
	}
	var revisionType string
	var revisionNotNull int
	var revisionDefault any
	rows, err := database.Query(`PRAGMA table_info('role_bindings')`)
	if err != nil {
		t.Fatal(err)
	}
	foundRevision := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "revision" {
			foundRevision = true
			revisionType, revisionNotNull, revisionDefault = typeName, notNull, defaultValue
		}
	}
	_ = rows.Close()
	if !foundRevision || strings.ToUpper(revisionType) != "INTEGER" || revisionNotNull != 1 || fmt.Sprint(revisionDefault) != "1" {
		t.Fatalf("revision contract found=%v type=%q notnull=%d default=%#v", foundRevision, revisionType, revisionNotNull, revisionDefault)
	}
	var ledger int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("migration ledger=%d error=%v", ledger, err)
	}
}

func TestYU24PersistentBindingInvariantsRejectBypassWrites(t *testing.T) {
	_, database := newMigrationDatabase(t)
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Org A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('user-a', 'org-a', 'User A', 'active')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-a', 'org-a', 'contributor', 'project', 'project-a', 'user-a', 'active')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('organization-binding-a', 'org-a', 'system-administrator', 'organization', 'org-a', 'user-a', 'active')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'binding-a'`); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(bindingRevisionAbort)) {
		t.Fatalf("revocation without revision CAS error=%v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET revision = revision + 1 WHERE id = 'binding-a'`); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(bindingRevisionOnlyAbort)) {
		t.Fatalf("revision-only mutation error=%v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET role_id = 'viewer' WHERE id = 'binding-a'`); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(bindingIdentityAbort)) {
		t.Fatalf("binding identity mutation error=%v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled', revision = 2 WHERE id = 'binding-a'`); err != nil {
		t.Fatalf("legal revocation: %v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'active', revision = 3 WHERE id = 'binding-a'`); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(bindingReactivationAbort)) {
		t.Fatalf("binding reactivation error=%v", err)
	}
	if _, err := database.Exec(`DELETE FROM role_bindings WHERE id = 'binding-a'`); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(bindingDeleteAbort)) {
		t.Fatalf("binding history delete error=%v", err)
	}
	// YU-24 owns only user-scoped project RoleBindings. Organization bindings
	// keep their pre-YU-24 lifecycle semantics and are not frozen by these CAS triggers.
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'organization-binding-a'`); err != nil {
		t.Fatalf("YU-24 project triggers overreached organization binding: %v", err)
	}
}

func TestYU24ForgedLedgerDoesNotHideMalformedRevisionColumn(t *testing.T) {
	_, database := newMigrationDatabase(t)
	if _, err := database.Exec(`ALTER TABLE role_bindings ADD COLUMN revision TEXT NOT NULL DEFAULT 'bad'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id) VALUES (?)`, MigrationID); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil || !strings.Contains(err.Error(), "revision column has an invalid contract") {
		t.Fatalf("forged-ledger malformed revision error=%v", err)
	}
}
