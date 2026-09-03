package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
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
