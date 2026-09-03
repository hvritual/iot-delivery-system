package deliveryauthz_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	_ "modernc.org/sqlite"
	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
)

func TestOperationGuardProjectBindingAllowsOwnedProjectAndRejectsOtherProject(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	seedProject(t, repository, "project-b", "org-a")
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	authorized := authorizedOperation("org-a", "delivery.items.create", "delivery.work-items.create", "contributor", "project:project-a")
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); err != nil {
		t.Fatalf("owned project prepare: %v", err)
	}
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-b"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("other project error = %v, want denied", err)
	}
}

func TestOperationGuardRejectsWorkItemWithoutOwnedProject(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	if err := repository.Create(t.Context(), delivery.WorkItem{ID: "item-orphan", ProjectID: "", Title: "orphan"}); err != nil {
		t.Fatal(err)
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	authorized := authorizedOperation("org-a", "delivery.items.update", "delivery.work-items.update", "contributor", "project:project-a")
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.UpdateItemRequest{Id: "item-orphan"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("orphan item error = %v, want denied", err)
	}
}

func TestOperationGuardDashboardPublishesOnlyOwnedProjectScope(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	seedProject(t, repository, "project-b", "org-b")
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	secured, err := guard.Prepare(t.Context(), authorizedOperation("org-a", "delivery.dashboard.get", "delivery.dashboard.read", "auditor", "organization:org-a"), &deliveryv1.GetDashboardRequest{})
	if err != nil {
		t.Fatalf("dashboard prepare: %v", err)
	}
	projects, ok := deliveryauthz.AuthorizedProjectsFromContext(secured)
	if !ok || len(projects) != 1 || !projects["project-a"] {
		t.Fatalf("authorized projects = %#v, present=%v, want project-a only", projects, ok)
	}
}

func TestOperationGuardRejectsUnverifiedGrantMixedWithValidGrant(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	seedProject(t, repository, "project-b", "org-a")
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatalf("new guard: %v", err)
	}
	authorized := authorizedOperation("org-a", "delivery.items.create", "delivery.work-items.create", "contributor", "project:project-a")
	authorized.Decision.Grants = append(authorized.Decision.Grants, authz.Grant{Permission: "delivery.work-items.create", RoleID: "forged-role", Scope: "project:project-b"})
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.CreateItemRequest{ProjectId: "project-b"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("mixed grants error = %v, want denied", err)
	}
}

func TestOperationGuardRejectsCrossProjectItemAndMissingProjectOwnership(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	seedProject(t, repository, "project-b", "org-b")
	if err := repository.Create(t.Context(), delivery.WorkItem{ID: "item-b", ProjectID: "project-b", Title: "item"}); err != nil {
		t.Fatal(err)
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	authorized := authorizedOperation("org-a", "delivery.items.update", "delivery.work-items.update", "contributor", "project:project-a")
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.UpdateItemRequest{Id: "item-b"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("cross organization item error = %v, want denied", err)
	}
	if _, err := guard.Prepare(t.Context(), authorized, &deliveryv1.UpdateItemRequest{Id: "missing"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("missing item error = %v, want denied", err)
	}
}

func TestOperationGuardRequiresRoleGrantAllowedScope(t *testing.T) {
	database := migratedDatabase(t)
	if _, err := database.Exec(`DELETE FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = ? AND scope_type = ?`, "contributor", "delivery.work-items.create", "project"); err != nil {
		t.Fatal(err)
	}
	repository := delivery.NewMemoryRepository()
	seedProject(t, repository, "project-a", "org-a")
	guard, err := deliveryauthz.NewOperationGuard(repository, database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Prepare(t.Context(), authorizedOperation("org-a", "delivery.items.create", "delivery.work-items.create", "contributor", "project:project-a"), &deliveryv1.CreateItemRequest{ProjectId: "project-a"}); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("absent allowed scope error = %v, want denied", err)
	}
}

func TestOperationGuardRejectsNilRegisteredInputBeforeApplication(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Prepare(t.Context(), authorizedOperation("org-a", "delivery.projects.create", "delivery.projects.create", "system-administrator", "organization:org-a"), nil); !errors.Is(err, deliveryauthz.ErrDenied) {
		t.Fatalf("nil input error = %v, want denied", err)
	}
}

func TestCreateProjectStampsTrustedOrganizationContext(t *testing.T) {
	repository := delivery.NewMemoryRepository()
	guard, err := deliveryauthz.NewOperationGuard(repository, migratedDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	request := &deliveryv1.CreateProjectRequest{Name: "project", Board: string(delivery.BoardResearchDelivery), Owner: "owner"}
	secured, err := guard.Prepare(t.Context(), authorizedOperation("org-a", "delivery.projects.create", "delivery.projects.create", "system-administrator", "organization:org-a"), request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := application.NewAdapter(delivery.NewService(repository, nil)).CreateProject(secured, request)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetProject(t.Context(), response.GetProject().GetId())
	if err != nil {
		t.Fatal(err)
	}
	if stored.OrganizationID != "org-a" {
		t.Fatalf("stamped organization = %q, want org-a", stored.OrganizationID)
	}
}

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return database
}

func seedProject(t *testing.T, repository *delivery.MemoryRepository, id, organizationID string) {
	t.Helper()
	if err := repository.CreateProject(t.Context(), delivery.Project{ID: id, OrganizationID: organizationID, Name: id, Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func authorizedOperation(tenantID, operation, permission, roleID, scope string) authz.AuthorizedOperation {
	return authz.AuthorizedOperation{
		Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: tenantID, UserID: "user-a"},
		Policy:    authz.Policy{Operation: authz.OperationID(operation), Permissions: []authz.PermissionKey{authz.PermissionKey(permission)}},
		Decision:  authz.Decision{Allowed: true, Operation: authz.OperationID(operation), Permissions: []authz.PermissionKey{authz.PermissionKey(permission)}, Grants: []authz.Grant{{Permission: authz.PermissionKey(permission), RoleID: roleID, Scope: scope}}},
	}
}
