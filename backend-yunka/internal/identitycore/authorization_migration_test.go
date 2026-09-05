package identitycore

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"

	_ "modernc.org/sqlite"
)

func TestAuthorizationModelsCarryScopeAndHumanSubject(t *testing.T) {
	userID := "user-a"
	team := Team{ID: "team-a", OrganizationID: "org-a", Name: "Delivery", ScopeType: ScopeTypeProject, ScopeID: "project-a", Status: StatusActive}
	membership := Membership{TeamID: team.ID, OrganizationID: team.OrganizationID, UserID: userID}
	role := Role{ID: "project-administrator", BindingScope: ScopeTypeProject}
	permission := Permission{ID: "delivery.work-items.read", Resource: "delivery.work-items", Action: "read", AllowedScopes: []ScopeType{ScopeTypeProject, ScopeTypeObject}, Status: PermissionStatusActive}
	grant := RolePermissionGrant{RoleID: role.ID, PermissionID: permission.ID, AllowedScopes: permission.AllowedScopes}
	binding := RoleBinding{ID: "binding-a", OrganizationID: team.OrganizationID, RoleID: role.ID, ScopeType: ScopeTypeProject, ScopeID: team.ScopeID, UserID: &userID, Status: StatusActive}

	if membership.TeamID != team.ID || grant.RoleID != role.ID || binding.UserID == nil || *binding.UserID != userID {
		t.Fatalf("authorization models did not retain the expected identity relationship: membership=%+v grant=%+v binding=%+v", membership, grant, binding)
	}
}

func TestApplyMigrationsSeedsExactAuthorizationDictionary(t *testing.T) {
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		t.Fatalf("load authoritative permission dictionary: %v", err)
	}
	if len(dictionary.Roles) == 0 || len(dictionary.Permissions) == 0 {
		t.Fatal("authoritative permission dictionary must be nonempty")
	}
	database := openAuthorizationTestDatabase(t)
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}

	wantRoles := make(map[string]string, len(dictionary.Roles))
	wantGrantPairs := map[string]bool{}
	wantGrants := map[string]bool{}
	for _, role := range dictionary.Roles {
		wantRoles[role.ID] = role.BindingScope
		for _, grant := range role.Grants {
			wantGrantPairs[role.ID+"\x00"+grant.Permission] = true
			for _, scope := range grant.AllowedScopes {
				wantGrants[role.ID+"\x00"+grant.Permission+"\x00"+scope] = true
			}
		}
	}
	gotRoles := map[string]string{}
	rows, err := database.Query(`SELECT id, binding_scope FROM roles`)
	if err != nil {
		t.Fatalf("read seeded roles: %v", err)
	}
	for rows.Next() {
		var id, bindingScope string
		if err := rows.Scan(&id, &bindingScope); err != nil {
			t.Fatalf("scan seeded role: %v", err)
		}
		gotRoles[id] = bindingScope
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close seeded role rows: %v", err)
	}
	if !sameStringMap(wantRoles, gotRoles) {
		t.Fatalf("seeded roles = %#v, want %#v", gotRoles, wantRoles)
	}

	wantPermissions := map[string]string{}
	wantPermissionScopes := map[string]bool{}
	for _, permission := range dictionary.Permissions {
		wantPermissions[permission.ID] = strings.Join([]string{permission.Resource, permission.Action, permission.Status}, "\x00")
		for _, scope := range permission.AllowedScopes {
			wantPermissionScopes[permission.ID+"\x00"+scope] = true
		}
	}
	gotPermissions := map[string]string{}
	rows, err = database.Query(`SELECT id, resource, action, status FROM permissions`)
	if err != nil {
		t.Fatalf("read seeded permissions: %v", err)
	}
	for rows.Next() {
		var id, resource, action, status string
		if err := rows.Scan(&id, &resource, &action, &status); err != nil {
			t.Fatalf("scan seeded permission: %v", err)
		}
		gotPermissions[id] = strings.Join([]string{resource, action, status}, "\x00")
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close seeded permission rows: %v", err)
	}
	if !sameStringMap(wantPermissions, gotPermissions) {
		t.Fatalf("seeded permissions = %#v, want %#v", gotPermissions, wantPermissions)
	}
	assertExactKeyRows(t, database, `SELECT permission_id || char(0) || scope_type FROM permission_allowed_scopes`, wantPermissionScopes, "permission allowed scopes")
	assertExactKeyRows(t, database, `SELECT role_id || char(0) || permission_id FROM role_permission_grants`, wantGrantPairs, "role permission grant pairs")
	assertExactKeyRows(t, database, `SELECT role_id || char(0) || permission_id || char(0) || scope_type FROM role_permission_grant_allowed_scopes`, wantGrants, "role permission grants")
}

func TestApplyMigrationsAuthorizationConstraintsFailClosed(t *testing.T) {
	database := openAuthorizationTestDatabase(t)
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A')`,
		`INSERT INTO organizations (id, slug, name) VALUES ('org-b', 'org-b', 'Organization B')`,
		`INSERT INTO users (id, organization_id, display_name) VALUES ('user-a', 'org-a', 'Alice')`,
		`INSERT INTO users (id, organization_id, display_name) VALUES ('user-b', 'org-b', 'Bob')`,
		`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-a', 'org-a', 'Delivery', 'project', 'project-a')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("seed authorization constraint test with %q: %v", statement, err)
		}
	}
	assertSQLiteRejected(t, database, `INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-empty', 'org-a', 'Empty', 'project', ' ')`)
	assertSQLiteRejected(t, database, `INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-unknown', 'org-a', 'Unknown', 'object', 'object-a')`)
	assertSQLiteRejected(t, database, `INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-wrong-org-scope', 'org-a', 'Wrong organization scope', 'organization', 'org-b')`)
	if _, err := database.Exec(`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-org-a', 'org-a', 'Organization team', 'organization', 'org-a')`); err != nil {
		t.Fatalf("add organization-scoped team: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-a', 'org-a', 'user-a')`); err != nil {
		t.Fatalf("add same-organization team membership: %v", err)
	}
	assertSQLiteRejected(t, database, `INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-a', 'org-a', 'user-a')`)
	assertSQLiteRejected(t, database, `INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-a', 'org-a', 'user-b')`)
	assertSQLiteRejected(t, database, `DELETE FROM users WHERE id = 'user-a'`)
	assertSQLiteRejected(t, database, `DELETE FROM teams WHERE id = 'team-a'`)

	if _, err := database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-a', 'org-a', 'project-administrator', 'project', 'project-a', 'user-a', 'active')`); err != nil {
		t.Fatalf("add valid user role binding: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-project-team', 'org-a', 'viewer', 'project', 'project-a', 'team-a', 'active')`); err != nil {
		t.Fatalf("add same-project team role binding: %v", err)
	}
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-project-team-other-project', 'org-a', 'viewer', 'project', 'project-b', 'team-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-project-team-organization', 'org-a', 'auditor', 'organization', 'org-a', 'team-a', 'active')`)
	if _, err := database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-org-team-organization', 'org-a', 'auditor', 'organization', 'org-a', 'team-org-a', 'active')`); err != nil {
		t.Fatalf("bind organization-scoped team at organization: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-org-team-project', 'org-a', 'viewer', 'project', 'project-a', 'team-org-a', 'active')`); err != nil {
		t.Fatalf("bind organization-scoped team at project: %v", err)
	}
	assertSQLiteRejected(t, database, `UPDATE teams SET scope_id = 'project-b' WHERE id = 'team-a'`)
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'binding-project-team'`); err != nil {
		t.Fatalf("disable same-project team role binding: %v", err)
	}
	if _, err := database.Exec(`UPDATE teams SET scope_id = 'project-b' WHERE id = 'team-a'`); err != nil {
		t.Fatalf("move team after disabling its role binding: %v", err)
	}
	assertSQLiteRejected(t, database, `UPDATE role_bindings SET status = 'active' WHERE id = 'binding-project-team'`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-duplicate', 'org-a', 'project-administrator', 'project', 'project-a', 'user-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-scope-mismatch', 'org-a', 'viewer', 'organization', 'org-a', 'team-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-cross-org', 'org-a', 'viewer', 'project', 'project-a', 'user-b', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id, status) VALUES ('binding-cross-org-team', 'org-b', 'viewer', 'project', 'project-b', 'team-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, status) VALUES ('binding-no-subject', 'org-a', 'viewer', 'project', 'project-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, team_id, status) VALUES ('binding-two-subjects', 'org-a', 'viewer', 'project', 'project-a', 'user-a', 'team-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-unknown-role', 'org-a', 'unknown', 'project', 'project-a', 'user-a', 'active')`)
	assertSQLiteRejected(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-unknown-status', 'org-a', 'viewer', 'project', 'project-a', 'user-a', 'unknown')`)
	assertSQLiteRejected(t, database, `DELETE FROM roles WHERE id = 'project-administrator'`)
	assertSQLiteRejected(t, database, `DELETE FROM permissions WHERE id = 'delivery.work-items.read'`)
}

func TestApplyMigrationsRollsBackAuthorizationMigrationOnConflict(t *testing.T) {
	database := openAuthorizationTestDatabase(t)
	if _, err := database.Exec(identitySchema); err != nil {
		t.Fatalf("create S0-02-01 schema: %v", err)
	}
	if _, err := database.Exec(serviceCredentialSchema); err != nil {
		t.Fatalf("create S0-02-07 schema: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create migration ledger: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES ('S0-02-01_identity_core_v1', '2026-09-03T00:00:00Z'), ('S0-02-07_service_credentials_v1', '2026-09-03T00:00:00Z')`); err != nil {
		t.Fatalf("record existing migrations: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE roles (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("precreate conflicting authorization table: %v", err)
	}

	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("authorization migration with a conflicting roles table unexpectedly succeeded")
	}
	for _, table := range []string{"teams", "team_memberships", "permissions", "role_bindings"} {
		var found string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&found); err != sql.ErrNoRows {
			t.Fatalf("failed authorization migration must roll back %q, found=%q error=%v", table, found, err)
		}
	}
	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = 'S0-03-02_authorization_dictionary_v1'`).Scan(&migrationCount); err != nil || migrationCount != 0 {
		t.Fatalf("failed authorization migration ledger count = %d error=%v, want 0", migrationCount, err)
	}
}

func openAuthorizationTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign keys = %d error=%v, want 1", foreignKeys, err)
	}
	return database
}

func assertSQLiteRejected(t *testing.T, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.Exec(statement); err == nil {
		t.Fatalf("SQLite unexpectedly accepted %q", statement)
	}
}

func assertExactKeyRows(t *testing.T, database *sql.DB, statement string, want map[string]bool, label string) {
	t.Helper()
	rows, err := database.Query(statement)
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("scan %s: %v", label, err)
		}
		got[key] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s: %v", label, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d; got=%#v want=%#v", label, len(got), len(want), got, want)
	}
	for key := range want {
		if !got[key] {
			t.Fatalf("%s missing %q; got=%#v", label, key, got)
		}
	}
}

func sameStringMap(want, got map[string]string) bool {
	if len(want) != len(got) {
		return false
	}
	for key, value := range want {
		if got[key] != value {
			return false
		}
	}
	return true
}
