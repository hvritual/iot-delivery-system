package serviceauthz

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	_ "modernc.org/sqlite"
	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
)

func TestServiceGrantsDefaultDenyExactScopeAndImmediateRevocation(t *testing.T) {
	database := migratedDatabase(t)
	projects := delivery.NewMemoryRepository()
	seedOrganization(t, database, "org-a")
	seedOrganization(t, database, "org-b")
	seedServiceAccount(t, database, "service-a", "org-a")
	seedProject(t, projects, "project-a", "org-a")
	seedProject(t, projects, "project-b", "org-a")
	seedProject(t, projects, "project-c", "org-b")

	manager, err := NewManager(database, projects)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	resolver, err := NewGrantResolver(database)
	if err != nil {
		t.Fatalf("new service grant resolver: %v", err)
	}
	principal := servicePrincipal("org-a", "service-a")
	request := grantRequest(principal, "delivery.items.create", "delivery.work-items.create")

	assertGrants(t, resolve(t, resolver, request), nil)
	if err := manager.Grant(t.Context(), GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant exact service permission: %v", err)
	}
	assertGrants(t, resolve(t, resolver, request), []authz.Grant{{Permission: "delivery.work-items.create", RoleID: "service-account:service-a", Scope: "project:project-a"}})
	if err := manager.Grant(t.Context(), GrantInput{ID: "grant-duplicate", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err == nil {
		t.Fatal("duplicate active service grant unexpectedly succeeded")
	}
	rolesForged := principal
	rolesForged.Roles = []string{"system-administrator"}
	assertGrants(t, resolve(t, resolver, grantRequest(rolesForged, "delivery.items.create", "delivery.work-items.create")), nil)
	assertGrants(t, resolve(t, resolver, grantRequest(principal, "delivery.items.create", "delivery.work-items.update")), nil)
	assertGrants(t, resolve(t, resolver, grantRequest(principal, "delivery.items.update", "delivery.work-items.create")), nil)
	if err := manager.Grant(t.Context(), GrantInput{ID: "cross-org", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-c"}); err == nil {
		t.Fatal("cross-organization project grant unexpectedly succeeded")
	}
	if err := manager.Revoke(t.Context(), "grant-a"); err != nil {
		t.Fatalf("revoke service grant: %v", err)
	}
	assertGrants(t, resolve(t, resolver, request), nil)
}

func TestServiceGrantsRejectOrganizationOperationsAndRemainProjectSpecific(t *testing.T) {
	database := migratedDatabase(t)
	projects := delivery.NewMemoryRepository()
	seedOrganization(t, database, "org-a")
	seedServiceAccount(t, database, "service-a", "org-a")
	seedProject(t, projects, "project-a", "org-a")
	seedProject(t, projects, "project-b", "org-a")
	manager, err := NewManager(database, projects)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	resolver, err := NewGrantResolver(database)
	if err != nil {
		t.Fatalf("new service grant resolver: %v", err)
	}
	if err := manager.Grant(t.Context(), GrantInput{ID: "dashboard", ServiceAccountID: "service-a", OperationID: "delivery.dashboard.get", Permission: "delivery.dashboard.read", ProjectID: "project-a"}); err == nil {
		t.Fatal("organization dashboard service grant unexpectedly succeeded")
	}
	if err := manager.Grant(t.Context(), GrantInput{ID: "project-create", ServiceAccountID: "service-a", OperationID: "delivery.projects.create", Permission: "delivery.projects.create", ProjectID: "project-a"}); err == nil {
		t.Fatal("organization project-create service grant unexpectedly succeeded")
	}
	for _, input := range []GrantInput{
		{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"},
		{ID: "grant-b", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-b"},
	} {
		if err := manager.Grant(t.Context(), input); err != nil {
			t.Fatalf("grant %s: %v", input.ID, err)
		}
	}
	assertGrants(t, resolve(t, resolver, grantRequest(servicePrincipal("org-a", "service-a"), "delivery.items.create", "delivery.work-items.create")), []authz.Grant{
		{Permission: "delivery.work-items.create", RoleID: "service-account:service-a", Scope: "project:project-a"},
		{Permission: "delivery.work-items.create", RoleID: "service-account:service-a", Scope: "project:project-b"},
	})
}

func TestServiceGrantRowsRejectRedirectionAndReactivationOutsideManagementPort(t *testing.T) {
	database := migratedDatabase(t)
	projects := delivery.NewMemoryRepository()
	seedOrganization(t, database, "org-a")
	seedServiceAccount(t, database, "service-a", "org-a")
	seedProject(t, projects, "project-a", "org-a")
	seedProject(t, projects, "project-b", "org-a")
	manager, err := NewManager(database, projects)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	if err := manager.Grant(t.Context(), GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant service account: %v", err)
	}
	for _, statement := range []string{
		`UPDATE service_operation_grants SET project_id = 'project-b' WHERE id = 'grant-a'`,
		`UPDATE service_operation_grants SET operation_id = 'delivery.items.update', permission_id = 'delivery.work-items.update' WHERE id = 'grant-a'`,
		`UPDATE service_operations SET required_scope = 'organization' WHERE id = 'delivery.items.create'`,
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Fatalf("SQLite unexpectedly accepted grant dictionary or tuple mutation: %s", statement)
		}
	}
	if err := manager.Revoke(t.Context(), "grant-a"); err != nil {
		t.Fatalf("revoke service grant: %v", err)
	}
	if _, err := database.Exec(`UPDATE service_operation_grants SET status = 'active', revoked_at = NULL WHERE id = 'grant-a'`); err == nil {
		t.Fatal("SQLite unexpectedly reactivated a revoked service grant")
	}
}

func TestGrantUsesSharedSQLiteProjectLookupWithoutConnectionDeadlock(t *testing.T) {
	databasePath := t.TempDir() + "/service-grants.db"
	repository, err := delivery.NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	seedOrganization(t, repository.Database(), "org-a")
	seedServiceAccount(t, repository.Database(), "service-a", "org-a")
	if err := repository.CreateProject(t.Context(), delivery.Project{ID: "project-a", OrganizationID: "org-a", Name: "Project A", Board: delivery.BoardResearchDelivery, Owner: "owner"}); err != nil {
		t.Fatalf("seed shared SQLite project: %v", err)
	}
	manager, err := NewManager(repository.Database(), repository)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Grant(ctx, GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant through shared SQLite repository: %v", err)
	}
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

func seedOrganization(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES (?, ?, ?)`, id, id, id); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
}

func seedServiceAccount(t *testing.T, database *sql.DB, id, organizationID string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES (?, ?, ?)`, id, organizationID, id); err != nil {
		t.Fatalf("seed service account: %v", err)
	}
}

func seedProject(t *testing.T, projects *delivery.MemoryRepository, id, organizationID string) {
	t.Helper()
	if err := projects.CreateProject(context.Background(), delivery.Project{ID: id, OrganizationID: organizationID, Name: id, Board: delivery.BoardResearchDelivery, Owner: "owner"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func servicePrincipal(tenantID, serviceAccountID string) identity.Principal {
	return identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: tenantID, Subject: "service-account/" + serviceAccountID}
}

func grantRequest(principal identity.Principal, operation, permission string) authz.GrantRequest {
	// Generated delivery OperationPlans currently set TenantRequired=false.
	// A service grant remains tenant-constrained by the authenticated principal,
	// the durable account organization, and the guard's owned-project check.
	return authz.GrantRequest{Principal: principal, Operation: authz.OperationID(operation), Permissions: []authz.PermissionKey{authz.PermissionKey(permission)}}
}

func resolve(t *testing.T, resolver *Resolver, request authz.GrantRequest) []authz.Grant {
	t.Helper()
	grants, err := resolver.ResolveGrants(t.Context(), request)
	if err != nil {
		t.Fatalf("resolve service grants: %v", err)
	}
	return grants
}

func assertGrants(t *testing.T, got, want []authz.Grant) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("grants = %#v, want %#v", got, want)
	}
}
