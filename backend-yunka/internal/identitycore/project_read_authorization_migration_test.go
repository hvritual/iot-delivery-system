package identitycore

import (
	"context"
	"database/sql"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
)

func TestApplyMigrationsAddsProjectReadAuthorizationToOldFourLedgerDatabase(t *testing.T) {
	database := legacyProjectReadAuthorizationDatabase(t, true)

	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade old four-ledger authorization database: %v", err)
	}
	assertProjectReadAuthorizationRows(t, database)
	assertPlanningListAuthorizationRows(t, database)
	assertProjectReadMigrationLedger(t, database, 7)
	var serviceGrantCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM service_operation_grants WHERE operation_id = 'delivery.projects.list'`).Scan(&serviceGrantCount); err != nil || serviceGrantCount != 0 {
		t.Fatalf("project list service grants = %d error=%v, want no default grants", serviceGrantCount, err)
	}
	assertSQLiteRejected(t, database, `UPDATE service_operations SET required_scope = 'organization' WHERE id = 'delivery.projects.list'`)
	assertSQLiteRejected(t, database, `DELETE FROM service_operations WHERE id = 'delivery.projects.list'`)

	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat upgraded project read migration: %v", err)
	}
	assertProjectReadAuthorizationRows(t, database)
	assertPlanningListAuthorizationRows(t, database)
	assertProjectReadMigrationLedger(t, database, 7)
}

func TestApplyMigrationsAddsProjectReadAuthorizationBeforeServiceGrantSeed(t *testing.T) {
	database := legacyProjectReadAuthorizationDatabase(t, false)

	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade authorization-only ledger before service grant schema: %v", err)
	}
	assertProjectReadAuthorizationRows(t, database)
	assertPlanningListAuthorizationRows(t, database)
	assertProjectReadMigrationLedger(t, database, 7)
}

func TestApplyMigrationsRejectsProjectReadConflictAndForgedLedger(t *testing.T) {
	t.Run("conflicting permission rolls back", func(t *testing.T) {
		database := legacyProjectReadAuthorizationDatabase(t, true)
		if _, err := database.Exec(`INSERT INTO permissions (id, resource, action, status) VALUES ('delivery.projects.read', 'delivery.projects', 'write', 'active')`); err != nil {
			t.Fatalf("seed conflicting project read permission: %v", err)
		}

		if err := ApplyMigrations(context.Background(), database); err == nil {
			t.Fatal("conflicting project read permission unexpectedly migrated")
		}
		var scopeCount, grantCount, ledgerCount int
		if err := database.QueryRow(`SELECT COUNT(*) FROM permission_allowed_scopes WHERE permission_id = 'delivery.projects.read'`).Scan(&scopeCount); err != nil || scopeCount != 0 {
			t.Fatalf("conflicting migration changed project read scopes = %d error=%v, want 0", scopeCount, err)
		}
		if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE permission_id = 'delivery.projects.read'`).Scan(&grantCount); err != nil || grantCount != 0 {
			t.Fatalf("conflicting migration changed project read grants = %d error=%v, want 0", grantCount, err)
		}
		if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ProjectReadAuthorizationMigrationID).Scan(&ledgerCount); err != nil || ledgerCount != 0 {
			t.Fatalf("conflicting migration ledger = %d error=%v, want 0", ledgerCount, err)
		}
	})

	t.Run("forged ledger cannot bypass exact verification", func(t *testing.T) {
		database := legacyProjectReadAuthorizationDatabase(t, true)
		if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, '2026-09-04T00:00:00Z')`, ProjectReadAuthorizationMigrationID); err != nil {
			t.Fatalf("forge project read migration ledger: %v", err)
		}

		if err := ApplyMigrations(context.Background(), database); err == nil {
			t.Fatal("forged project read migration ledger unexpectedly bypassed verification")
		}
		var permissionCount, grantCount, operationCount int
		if err := database.QueryRow(`SELECT COUNT(*) FROM permissions WHERE id = 'delivery.projects.read'`).Scan(&permissionCount); err != nil || permissionCount != 0 {
			t.Fatalf("forged ledger migration inserted permission count=%d error=%v, want 0", permissionCount, err)
		}
		if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE permission_id = 'delivery.projects.read'`).Scan(&grantCount); err != nil || grantCount != 0 {
			t.Fatalf("forged ledger migration inserted grants count=%d error=%v, want 0", grantCount, err)
		}
		if err := database.QueryRow(`SELECT COUNT(*) FROM service_operations WHERE id = 'delivery.projects.list'`).Scan(&operationCount); err != nil || operationCount != 0 {
			t.Fatalf("forged ledger migration inserted service operation count=%d error=%v, want 0", operationCount, err)
		}
	})
}

func legacyProjectReadAuthorizationDatabase(t *testing.T, includeServiceGrantSchema bool) *sql.DB {
	t.Helper()
	database := openAuthorizationTestDatabase(t)
	for _, schema := range []string{identitySchema, serviceCredentialSchema, authorizationSchema} {
		if _, err := database.Exec(schema); err != nil {
			t.Fatalf("create legacy identity schema: %v", err)
		}
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		t.Fatalf("load current authorization dictionary: %v", err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("begin legacy authorization seed: %v", err)
	}
	if err := seedAuthorizationDictionary(t.Context(), tx, dictionary); err != nil {
		_ = tx.Rollback()
		t.Fatalf("seed legacy authorization dictionary: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit legacy authorization seed: %v", err)
	}
	removeProjectReadAuthorization(t, database)
	removePlanningListAuthorization(t, database)
	if includeServiceGrantSchema {
		if _, err := database.Exec(serviceGrantSchema); err != nil {
			t.Fatalf("create legacy service grant schema: %v", err)
		}
		for _, operation := range dictionary.Operations {
			if operation.ID == "delivery.projects.list" || operation.ID == "delivery.releases.list" || operation.ID == "delivery.sprints.list" || operation.ID == "delivery.milestones.list" {
				continue
			}
			if _, err := database.Exec(`INSERT INTO service_operations (id, permission_id, required_scope, status) VALUES (?, ?, ?, 'active')`, operation.ID, operation.Permission, operation.RequiredScope); err != nil {
				t.Fatalf("seed legacy service operation %q: %v", operation.ID, err)
			}
		}
	}
	if _, err := database.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy migration ledger: %v", err)
	}
	migrationIDs := []string{MigrationID, ServiceCredentialMigrationID, AuthorizationMigrationID}
	if includeServiceGrantSchema {
		migrationIDs = append(migrationIDs, ServiceGrantMigrationID)
	}
	for _, migrationID := range migrationIDs {
		if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, '2026-09-03T00:00:00Z')`, migrationID); err != nil {
			t.Fatalf("record legacy migration %q: %v", migrationID, err)
		}
	}
	return database
}

func removeProjectReadAuthorization(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DELETE FROM role_permission_grant_allowed_scopes WHERE permission_id = 'delivery.projects.read'`,
		`DELETE FROM role_permission_grants WHERE permission_id = 'delivery.projects.read'`,
		`DELETE FROM permission_allowed_scopes WHERE permission_id = 'delivery.projects.read'`,
		`DELETE FROM permissions WHERE id = 'delivery.projects.read'`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("remove current project read authorization from legacy fixture: %v", err)
		}
	}
}

func removePlanningListAuthorization(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, permissionID := range []string{"delivery.releases.read", "delivery.sprints.read", "delivery.milestones.read"} {
		for _, statement := range []string{
			`DELETE FROM role_permission_grant_allowed_scopes WHERE permission_id = ?`,
			`DELETE FROM role_permission_grants WHERE permission_id = ?`,
			`DELETE FROM permission_allowed_scopes WHERE permission_id = ?`,
			`DELETE FROM permissions WHERE id = ?`,
		} {
			if _, err := database.Exec(statement, permissionID); err != nil {
				t.Fatalf("remove planning-list authorization %q from legacy fixture: %v", permissionID, err)
			}
		}
	}
}

func assertPlanningListAuthorizationRows(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, specification := range []struct {
		permission string
		resource   string
		operation  string
	}{
		{permission: "delivery.releases.read", resource: "delivery.releases", operation: "delivery.releases.list"},
		{permission: "delivery.sprints.read", resource: "delivery.sprints", operation: "delivery.sprints.list"},
		{permission: "delivery.milestones.read", resource: "delivery.milestones", operation: "delivery.milestones.list"},
	} {
		var resource, action, status string
		if err := database.QueryRow(`SELECT resource, action, status FROM permissions WHERE id = ?`, specification.permission).Scan(&resource, &action, &status); err != nil || resource != specification.resource || action != "read" || status != "active" {
			t.Fatalf("planning permission %q = (%q, %q, %q) error=%v", specification.permission, resource, action, status, err)
		}
		assertExactKeyRows(t, database, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = '`+specification.permission+`'`, map[string]bool{"project": true}, specification.permission+" scopes")
		for _, roleID := range []string{"system-administrator", "project-administrator", "release-approver", "contributor", "viewer", "auditor"} {
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE role_id = ? AND permission_id = ?`, roleID, specification.permission).Scan(&count); err != nil || count != 1 {
				t.Fatalf("planning permission %q role=%q count=%d error=%v, want 1", specification.permission, roleID, count, err)
			}
		}
		var permissionID, requiredScope, operationStatus string
		if err := database.QueryRow(`SELECT permission_id, required_scope, status FROM service_operations WHERE id = ?`, specification.operation).Scan(&permissionID, &requiredScope, &operationStatus); err != nil || permissionID != specification.permission || requiredScope != "project" || operationStatus != "active" {
			t.Fatalf("planning operation %q = (%q, %q, %q) error=%v", specification.operation, permissionID, requiredScope, operationStatus, err)
		}
	}
	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, PlanningListAuthorizationMigrationID).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("planning list migration ledger count = %d error=%v, want 1", migrationCount, err)
	}
}

func assertProjectReadAuthorizationRows(t *testing.T, database *sql.DB) {
	t.Helper()
	var resource, action, status string
	if err := database.QueryRow(`SELECT resource, action, status FROM permissions WHERE id = 'delivery.projects.read'`).Scan(&resource, &action, &status); err != nil || resource != "delivery.projects" || action != "read" || status != "active" {
		t.Fatalf("project read permission = (%q, %q, %q) error=%v, want active delivery.projects/read", resource, action, status, err)
	}
	assertExactKeyRows(t, database, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = 'delivery.projects.read'`, map[string]bool{"project": true}, "project read permission scopes")
	for _, roleID := range []string{"system-administrator", "project-administrator", "release-approver", "contributor", "viewer", "auditor"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grants WHERE role_id = ? AND permission_id = 'delivery.projects.read'`, roleID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("project read grant role=%q count=%d error=%v, want 1", roleID, count, err)
		}
		var scopeCount, projectScopeCount int
		if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = 'delivery.projects.read'`, roleID).Scan(&scopeCount); err != nil || scopeCount != 1 {
			t.Fatalf("project read grant scopes role=%q count=%d error=%v, want exactly 1", roleID, scopeCount, err)
		}
		if err := database.QueryRow(`SELECT COUNT(*) FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = 'delivery.projects.read' AND scope_type = 'project'`, roleID).Scan(&projectScopeCount); err != nil || projectScopeCount != 1 {
			t.Fatalf("project read project scope role=%q count=%d error=%v, want 1", roleID, projectScopeCount, err)
		}
	}
	var permissionID, requiredScope, operationStatus string
	if err := database.QueryRow(`SELECT permission_id, required_scope, status FROM service_operations WHERE id = 'delivery.projects.list'`).Scan(&permissionID, &requiredScope, &operationStatus); err != nil || permissionID != "delivery.projects.read" || requiredScope != "project" || operationStatus != "active" {
		t.Fatalf("project list service operation = (%q, %q, %q) error=%v, want active project delivery.projects.read", permissionID, requiredScope, operationStatus, err)
	}
}

func assertProjectReadMigrationLedger(t *testing.T, database *sql.DB, wantTotal int) {
	t.Helper()
	var migrationCount, ledgerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ProjectReadAuthorizationMigrationID).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("project read migration ledger count = %d error=%v, want 1", migrationCount, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations`).Scan(&ledgerCount); err != nil || ledgerCount != wantTotal {
		t.Fatalf("migration ledger total = %d error=%v, want %d", ledgerCount, err, wantTotal)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ItemReadAuthorizationMigrationID).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("item read migration ledger count = %d error=%v, want 1", migrationCount, err)
	}
	var operationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM service_operations WHERE id IN ('delivery.items.get', 'delivery.items.search', 'delivery.items.similarity') AND permission_id = 'delivery.work-items.read' AND status = 'active' AND ((id = 'delivery.items.get' AND required_scope = 'object') OR (id <> 'delivery.items.get' AND required_scope = 'project'))`).Scan(&operationCount); err != nil || operationCount != 3 {
		t.Fatalf("item read service operation count = %d error=%v, want 3", operationCount, err)
	}
}
