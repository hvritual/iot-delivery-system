package bootstrap

import (
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU24ConfiguredAuthorizationIncludesProjectRoleGuard(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu24-bootstrap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatal(err)
	}
	if err := localprojectroleadmin.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatal(err)
	}
	authorizer, guards, err := configuredAuthorization(t.Context(), Config{RuntimeEnvironment: RuntimeEnvironmentProduction}, repository)
	if err != nil {
		t.Fatal(err)
	}
	if authorizer == nil || guards == nil {
		t.Fatal("YU-24 configured authorization returned nil components")
	}
	guard, ok := guards.ResolveGuard(authz.OperationID(localprojectroleadmin.OperationAssignProjectRole))
	if !ok || guard == nil {
		t.Fatal("YU-24 assign operation is missing its dedicated operation guard")
	}
	guard, ok = guards.ResolveGuard(authz.OperationID(localprojectroleadmin.OperationRevokeProjectRole))
	if !ok || guard == nil {
		t.Fatal("YU-24 revoke operation is missing its dedicated operation guard")
	}
}
