package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU23DevelopmentEnvironmentDoesNotTurnJWTHumanIntoLocalAdmin(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu23-development-auth.db"))
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil { t.Fatal(err) }
	authorizer, guards, err := configuredAuthorization(context.Background(), Config{RuntimeEnvironment: RuntimeEnvironmentDevelopment}, repository)
	if err != nil { t.Fatal(err) }
	if guards == nil {
		t.Fatal("development authorization lost durable operation guards")
	}
	policy := authz.Policy{
		Operation: "delivery.dashboard.get",
		Authentication: []string{identity.AuthMethodJWT},
		Permissions: []authz.PermissionKey{"delivery.dashboard.read"},
		TenantRequired: true,
	}
	forged := identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Roles: []string{localauth.RoleLocalAdmin, "system-administrator"}}
	decision, err := authorizer.Authorize(t.Context(), forged, policy)
	if err != nil { t.Fatal(err) }
	if decision.Allowed {
		t.Fatalf("development JWT forged Roles decision=%#v, want durable deny", decision)
	}
}

func TestYU23DevelopmentAPIKeyCompatibilityStillUsesExplicitLocalProfile(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu23-development-api-key.db"))
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil { t.Fatal(err) }
	authorizer, _, err := configuredAuthorization(context.Background(), Config{RuntimeEnvironment: RuntimeEnvironmentDevelopment}, repository)
	if err != nil { t.Fatal(err) }
	principal := identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "local-api-key/local-admin", Roles: []string{localauth.RoleLocalAdmin}}
	decision, err := authorizer.Authorize(t.Context(), principal, authz.Policy{Operation: "delivery.dashboard.get", Authentication: []string{identity.AuthMethodAPIKey}, Permissions: []authz.PermissionKey{"delivery.dashboard.read"}, TenantRequired: true})
	if err != nil { t.Fatal(err) }
	if !decision.Allowed {
		t.Fatalf("development API-key compatibility decision=%#v, want allow", decision)
	}
}
