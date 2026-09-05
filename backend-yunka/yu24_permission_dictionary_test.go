package backendyunka

import (
	"slices"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

func init() {
	expectedPermissions[localprojectroleadmin.PermissionManageRoleBindings] = permissionContract{
		resource: "identity.role-bindings",
		action:   "manage",
		status:   "active",
		scopes:   []string{"project"},
	}
}

func TestYU24PermissionUsesExistingProjectRoleContractAndPlansStayInternal(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	foundPermission := false
	for _, permission := range dictionary.Permissions {
		if permission.ID != localprojectroleadmin.PermissionManageRoleBindings {
			continue
		}
		foundPermission = true
		if permission.Resource != "identity.role-bindings" || permission.Action != "manage" || permission.Status != "active" || !slices.Equal(permission.AllowedScopes, []string{"project"}) {
			t.Fatalf("active role binding permission=%#v", permission)
		}
	}
	if !foundPermission {
		t.Fatal("identity.role-bindings.manage permission is missing")
	}
	grantedBy := []string{}
	for _, role := range dictionary.Roles {
		for _, grant := range role.Grants {
			if grant.Permission == localprojectroleadmin.PermissionManageRoleBindings {
				if !slices.Equal(grant.AllowedScopes, []string{"project"}) {
					t.Fatalf("role binding management scopes for %q = %#v", role.ID, grant.AllowedScopes)
				}
				grantedBy = append(grantedBy, role.ID)
			}
	}
	if !slices.Equal(grantedBy, []string{"system-administrator", "project-administrator"}) {
		t.Fatalf("identity.role-bindings.manage granted by %#v", grantedBy)
	}
	for _, profile := range dictionary.DevelopmentCompatibility.LocalRoleProfiles {
		if slices.Contains(profile.Permissions, localprojectroleadmin.PermissionManageRoleBindings) {
			t.Fatalf("development profile %q gained durable RoleBinding management", profile.LocalRole)
		}
	}
	plans := localprojectroleadmin.OperationPlans()
	if len(plans) != 2 {
		t.Fatalf("project role internal plan count=%d, want 2", len(plans))
	}
	if err := operationplan.Validate(operationplan.Set{SchemaVersion: operationplan.SchemaVersion, Operations: plans}); err != nil {
		t.Fatalf("project role internal plan validation failed: %v", err)
	}
	for _, plan := range plans {
		if !plan.Security.TenantRequired || !slices.Equal(plan.Security.Authentication, []string{"jwt"}) || !slices.Equal(plan.Security.Permissions, []string{localprojectroleadmin.PermissionManageRoleBindings}) || plan.Execution.Transaction != "local" || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 {
			t.Fatalf("project role plan contract drift: %#v", plan)
		}
		for _, operation := range dictionary.Operations {
			if operation.ID == plan.OperationID {
				t.Fatalf("internal project role operation %q leaked into service dictionary", plan.OperationID)
			}
		}
	}
}
