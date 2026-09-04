package humanauthz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	_ "modernc.org/sqlite"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestNewGrantResolverRequiresDatabase(t *testing.T) {
	if _, err := NewGrantResolver(nil); !errors.Is(err, ErrDatabaseRequired) {
		t.Fatalf("NewGrantResolver(nil) error = %v, want ErrDatabaseRequired", err)
	}
}

func TestResolverResolvesActiveDirectOrganizationBinding(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-admin", "org-a", "system-administrator", "organization", "org-a", "user-a", "")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", []string{"forged-role"}), "delivery.dashboard.read", "delivery.work-items.read")
	want := []authz.Grant{{Permission: "delivery.dashboard.read", RoleID: "system-administrator", Scope: "organization:org-a"}, {Permission: "delivery.work-items.read", RoleID: "system-administrator", Scope: "organization:org-a"}}
	assertGrants(t, grants, want)
}

func TestResolverResolvesDirectProjectBindingAtCanonicalProjectScope(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read")
	assertGrants(t, grants, []authz.Grant{{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"}})
}

func TestResolverResolvesProjectTeamBinding(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedTeam(t, database, "team-project", "org-a", "project", "project-a", identitycore.StatusActive)
	seedMembership(t, database, "team-project", "org-a", "user-a")
	seedBinding(t, database, "binding-team", "org-a", "contributor", "project", "project-a", "", "team-project")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", nil), "delivery.work-items.create", "delivery.work-items.update")
	want := []authz.Grant{
		{Permission: "delivery.work-items.create", RoleID: "contributor", Scope: "project:project-a"},
		{Permission: "delivery.work-items.update", RoleID: "contributor", Scope: "project:project-a"},
	}
	assertGrants(t, grants, want)
}

func TestResolverKeepsOrganizationTeamProjectBindingAtItsProjectScope(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedTeam(t, database, "team-organization", "org-a", "organization", "org-a", identitycore.StatusActive)
	seedMembership(t, database, "team-organization", "org-a", "user-a")
	seedBinding(t, database, "binding-team", "org-a", "viewer", "project", "project-a", "", "team-organization")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read")
	assertGrants(t, grants, []authz.Grant{{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"}})
}

func TestResolverIgnoresPrincipalRolesAndDeduplicatesStableResults(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedTeam(t, database, "team-project", "org-a", "project", "project-a", identitycore.StatusActive)
	seedMembership(t, database, "team-project", "org-a", "user-a")
	seedBinding(t, database, "binding-direct-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
	seedBinding(t, database, "binding-team-viewer", "org-a", "viewer", "project", "project-a", "", "team-project")
	seedBinding(t, database, "binding-contributor", "org-a", "contributor", "project", "project-a", "user-a", "")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", []string{"system-administrator", "unknown"}), "delivery.work-items.update", "delivery.work-items.read")
	want := []authz.Grant{
		{Permission: "delivery.work-items.read", RoleID: "contributor", Scope: "project:project-a"},
		{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"},
		{Permission: "delivery.work-items.update", RoleID: "contributor", Scope: "project:project-a"},
	}
	assertGrants(t, grants, want)
}

func TestResolverRetainsSameRolePermissionAcrossDifferentProjectScopes(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-viewer-project-a", "org-a", "viewer", "project", "project-a", "user-a", "")
	seedBinding(t, database, "binding-viewer-project-b", "org-a", "viewer", "project", "project-b", "user-a", "")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read")
	want := []authz.Grant{
		{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-a"},
		{Permission: "delivery.work-items.read", RoleID: "viewer", Scope: "project:project-b"},
	}
	assertGrants(t, grants, want)
}

func TestResolverRequiresRolePermissionGrantAllowedScope(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
	mustExec(t, database, `DELETE FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = ?`, "viewer", "delivery.work-items.read")

	assertGrants(t, resolve(t, database, humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read"), nil)
}

func TestResolverFailsClosedForAbsentOrInactiveHumanAuthorization(t *testing.T) {
	for name, setup := range map[string]func(*testing.T, *sql.DB){
		"no binding": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusActive)
			seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
		},
		"cross organization user": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusActive)
			seedOrganization(t, database, "org-b", identitycore.StatusActive)
			seedUser(t, database, "user-b", "org-b", identitycore.StatusActive)
			seedBinding(t, database, "binding-viewer", "org-b", "viewer", "project", "project-b", "user-b", "")
		},
		"disabled organization": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusDisabled)
			seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
			seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
		},
		"disabled user": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusActive)
			seedUser(t, database, "user-a", "org-a", identitycore.StatusDisabled)
			seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
		},
		"disabled team": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusActive)
			seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
			seedTeam(t, database, "team-project", "org-a", "project", "project-a", identitycore.StatusDisabled)
			seedMembership(t, database, "team-project", "org-a", "user-a")
			seedBinding(t, database, "binding-team", "org-a", "viewer", "project", "project-a", "", "team-project")
		},
		"disabled binding": func(t *testing.T, database *sql.DB) {
			seedOrganization(t, database, "org-a", identitycore.StatusActive)
			seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
			seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
			mustExec(t, database, `UPDATE role_bindings SET status = 'disabled' WHERE id = ?`, "binding-viewer")
		},
	} {
		t.Run(name, func(t *testing.T) {
			database := migratedDatabase(t)
			setup(t, database)
			principal := humanPrincipal("org-a", "user-a", []string{"system-administrator"})
			if name == "cross organization user" {
				principal = humanPrincipal("org-a", "user-b", []string{"system-administrator"})
			}
			assertGrants(t, resolve(t, database, principal, "delivery.work-items.read"), nil)
		})
	}
}

func TestResolverFiltersReservedUnknownAndUnrequestedPermissions(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-admin", "org-a", "system-administrator", "organization", "org-a", "user-a", "")

	grants := resolve(t, database, humanPrincipal("org-a", "user-a", nil), "identity.teams.manage", "unknown.permission")
	assertGrants(t, grants, nil)
}

func TestResolverRejectsNonJWTAndInvalidHumanPrincipals(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")

	for name, principal := range map[string]identity.Principal{
		"api key":        {Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a", UserID: "user-a", Roles: []string{"viewer"}},
		"service token":  {Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", UserID: "user-a"},
		"unknown":        {Authenticated: true, AuthMethod: "unknown", TenantID: "org-a", UserID: "user-a"},
		"anonymous":      {Authenticated: false, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"},
		"missing tenant": {Authenticated: true, AuthMethod: identity.AuthMethodJWT, UserID: "user-a"},
		"missing user":   {Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a"},
		"noncanonical":   {Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: " org-a", UserID: "user-a"},
	} {
		t.Run(name, func(t *testing.T) {
			assertGrants(t, resolve(t, database, principal, "delivery.work-items.read"), nil)
		})
	}
}

func TestResolverReturnsRecognizableErrorsForCanceledAndClosedDatabase(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganization(t, database, "org-a", identitycore.StatusActive)
	seedUser(t, database, "user-a", "org-a", identitycore.StatusActive)
	seedBinding(t, database, "binding-viewer", "org-a", "viewer", "project", "project-a", "user-a", "")
	resolver, err := NewGrantResolver(database)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolver.ResolveGrants(canceled, grantRequest(humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read")); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrGrantResolution) {
		t.Fatalf("canceled resolve error = %v, want context.Canceled and ErrGrantResolution", err)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if _, err := resolver.ResolveGrants(t.Context(), grantRequest(humanPrincipal("org-a", "user-a", nil), "delivery.work-items.read")); !errors.Is(err, ErrGrantResolution) {
		t.Fatalf("closed database resolve error = %v, want ErrGrantResolution", err)
	}
}

func resolve(t *testing.T, database *sql.DB, principal identity.Principal, permissions ...string) []authz.Grant {
	t.Helper()
	resolver, err := NewGrantResolver(database)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	grants, err := resolver.ResolveGrants(t.Context(), grantRequest(principal, permissions...))
	if err != nil {
		t.Fatalf("resolve grants: %v", err)
	}
	return grants
}

func grantRequest(principal identity.Principal, permissions ...string) authz.GrantRequest {
	requested := make([]authz.PermissionKey, 0, len(permissions))
	for _, permission := range permissions {
		requested = append(requested, authz.PermissionKey(permission))
	}
	return authz.GrantRequest{Principal: principal, Permissions: requested}
}

func humanPrincipal(tenantID, userID string, roles []string) identity.Principal {
	return identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: tenantID, UserID: userID, Roles: roles}
}

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_")))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func seedOrganization(t *testing.T, database *sql.DB, id string, status identitycore.Status) {
	t.Helper()
	mustExec(t, database, `INSERT INTO organizations (id, slug, name, status) VALUES (?, ?, ?, ?)`, id, id, id, status)
}

func seedUser(t *testing.T, database *sql.DB, id, organizationID string, status identitycore.Status) {
	t.Helper()
	mustExec(t, database, `INSERT INTO users (id, organization_id, display_name, status) VALUES (?, ?, ?, ?)`, id, organizationID, id, status)
}

func seedTeam(t *testing.T, database *sql.DB, id, organizationID, scopeType, scopeID string, status identitycore.Status) {
	t.Helper()
	mustExec(t, database, `INSERT INTO teams (id, organization_id, name, scope_type, scope_id, status) VALUES (?, ?, ?, ?, ?, ?)`, id, organizationID, id, scopeType, scopeID, status)
}

func seedMembership(t *testing.T, database *sql.DB, teamID, organizationID, userID string) {
	t.Helper()
	mustExec(t, database, `INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES (?, ?, ?)`, teamID, organizationID, userID)
}

func seedBinding(t *testing.T, database *sql.DB, id, organizationID, roleID, scopeType, scopeID, userID, teamID string) {
	t.Helper()
	if userID != "" {
		mustExec(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id) VALUES (?, ?, ?, ?, ?, ?)`, id, organizationID, roleID, scopeType, scopeID, userID)
		return
	}
	mustExec(t, database, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id) VALUES (?, ?, ?, ?, ?, ?)`, id, organizationID, roleID, scopeType, scopeID, teamID)
}

func mustExec(t *testing.T, database *sql.DB, statement string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(statement, arguments...); err != nil {
		t.Fatalf("execute %q: %v", statement, err)
	}
}

func assertGrants(t *testing.T, got, want []authz.Grant) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("grants = %#v, want %#v", got, want)
	}
}
