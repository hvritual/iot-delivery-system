package deliveryauthz_test

import (
	"errors"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestOperationGuardRechecksServiceGrantWithoutRebuildingAfterRevocation(t *testing.T) {
	database := migratedDatabase(t)
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'Service A')`); err != nil {
		t.Fatal(err)
	}
	manager, err := serviceauthz.NewManager(database, repository)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	if err := manager.Grant(t.Context(), serviceauthz.GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant service account: %v", err)
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatalf("new operation guard: %v", err)
	}
	authorized := authz.AuthorizedOperation{
		Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/service-a"},
		Policy:    authz.Policy{Operation: "delivery.items.create", Permissions: []authz.PermissionKey{"delivery.work-items.create"}},
		Decision: authz.Decision{Allowed: true, Operation: "delivery.items.create", Permissions: []authz.PermissionKey{"delivery.work-items.create"}, Grants: []authz.Grant{{
			Permission: "delivery.work-items.create", RoleID: "service-account:service-a", Scope: "project:project-a",
		}}},
	}
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); err != nil {
		t.Fatalf("active service grant prepare: %v", err)
	}
	if err := manager.Revoke(t.Context(), "grant-a"); err != nil {
		t.Fatalf("revoke service grant: %v", err)
	}
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("stale service decision error = %v, want denied", err)
	}
}

func TestOperationGuardRechecksDisabledServiceAccountWithoutRebuilding(t *testing.T) {
	database := migratedDatabase(t)
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'Service A')`); err != nil {
		t.Fatal(err)
	}
	manager, err := serviceauthz.NewManager(database, repository)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	if err := manager.Grant(t.Context(), serviceauthz.GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant service account: %v", err)
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatalf("new operation guard: %v", err)
	}
	authorized := authorizedServiceOperation()
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); err != nil {
		t.Fatalf("active service account prepare: %v", err)
	}
	if _, err := database.Exec(`UPDATE service_accounts SET status = 'disabled' WHERE id = 'service-a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("disabled service account stale decision error = %v, want denied", err)
	}
}

func TestOperationGuardRechecksDirectAndTeamHumanRoleRevocationWithoutRebuilding(t *testing.T) {
	database := migratedDatabase(t)
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatalf("new operation guard: %v", err)
	}
	direct := authorizedOperation("org-a", "delivery.items.create", "delivery.work-items.create", "contributor", "project:project-a")
	if _, err := guard.Prepare(t.Context(), direct, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); err != nil {
		t.Fatalf("active direct human role prepare: %v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'binding-contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Prepare(t.Context(), direct, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("revoked direct human decision error = %v, want denied", err)
	}
	if _, err := database.Exec(`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-a', 'org-a', 'Team A', 'project', 'project-a'); INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-a', 'org-a', 'user-a'); INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id) VALUES ('binding-team-contributor', 'org-a', 'contributor', 'project', 'project-a', 'team-a')`); err != nil {
		t.Fatal(err)
	}
	team := authorizedOperation("org-a", "delivery.items.create", "delivery.work-items.create", "contributor", "project:project-a")
	if _, err := guard.Prepare(t.Context(), team, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); err != nil {
		t.Fatalf("active team human role prepare: %v", err)
	}
	if _, err := database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'binding-team-contributor'`); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Prepare(t.Context(), team, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("revoked team human decision error = %v, want denied", err)
	}
}

func authorizedServiceOperation() authz.AuthorizedOperation {
	return authz.AuthorizedOperation{
		Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/service-a"},
		Policy:    authz.Policy{Operation: "delivery.items.create", Permissions: []authz.PermissionKey{"delivery.work-items.create"}},
		Decision: authz.Decision{Allowed: true, Operation: "delivery.items.create", Permissions: []authz.PermissionKey{"delivery.work-items.create"}, Grants: []authz.Grant{{
			Permission: "delivery.work-items.create", RoleID: "service-account:service-a", Scope: "project:project-a",
		}}},
	}
}
