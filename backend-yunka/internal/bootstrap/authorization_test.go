package bootstrap

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestProductionAuthorizationUsesHumanResolverAndGuardForEveryRegisteredOperation(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "authorization.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	security, guards, err := configuredAuthorization(context.Background(), Config{RuntimeEnvironment: RuntimeEnvironmentProduction}, repository)
	if err != nil {
		t.Fatalf("configure production authorization: %v", err)
	}
	for _, operation := range []authz.OperationID{
		"delivery.dashboard.get", "delivery.items.list", "delivery.projects.create", "delivery.items.create", "delivery.items.update", "delivery.items.comment.create", "delivery.items.update-context", "delivery.items.advance-gate", "delivery.items.close", "delivery.releases.create", "delivery.sprints.create", "delivery.milestones.create",
	} {
		if _, ok := guards.ResolveGuard(operation); !ok {
			t.Fatalf("production guard missing for %q", operation)
		}
	}
	decision, err := security.Authorize(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Roles: []string{"system-administrator"}}, authz.Policy{Operation: "delivery.dashboard.get", Authentication: []string{identity.AuthMethodJWT}, Permissions: []authz.PermissionKey{"delivery.dashboard.read"}, TenantRequired: true})
	if err != nil {
		t.Fatalf("authorize forged principal roles: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("forged principal roles decision = %#v, want deny", decision)
	}
}

func TestProductionAuthorizationUsesServiceResolverForTenantOptionalOperationPlan(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "service-authorization.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if _, err := repository.Database().Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A'); INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'Service A')`); err != nil {
		t.Fatalf("seed service identity: %v", err)
	}
	if err := repository.CreateProject(t.Context(), delivery.Project{ID: "project-a", OrganizationID: "org-a", Name: "Project A", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	manager, err := serviceauthz.NewManager(repository.Database(), repository)
	if err != nil {
		t.Fatalf("new service grant manager: %v", err)
	}
	if err := manager.Grant(t.Context(), serviceauthz.GrantInput{ID: "grant-a", ServiceAccountID: "service-a", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: "project-a"}); err != nil {
		t.Fatalf("grant service operation: %v", err)
	}
	security, _, err := configuredAuthorization(context.Background(), Config{RuntimeEnvironment: RuntimeEnvironmentProduction}, repository)
	if err != nil {
		t.Fatalf("configure production authorization: %v", err)
	}
	decision, err := security.Authorize(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/service-a"}, authz.Policy{Operation: "delivery.items.create", Authentication: []string{identity.AuthMethodServiceToken}, Permissions: []authz.PermissionKey{"delivery.work-items.create"}})
	if err != nil {
		t.Fatalf("authorize service identity: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("service decision = %#v, want allowed with TenantRequired=false", decision)
	}
}
