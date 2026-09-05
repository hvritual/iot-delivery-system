package backendyunka

import (
	"slices"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

func init() {
	expectedResources["identity.users"] = []string{"organization"}
	expectedPermissions[localmemberadmin.PermissionManageUsers] = permissionContract{
		resource: "identity.users",
		action:   "manage",
		status:   "active",
		scopes:   []string{"organization"},
	}
	administrator := expectedRoles["system-administrator"]
	administrator.permissions = append(administrator.permissions, localmemberadmin.PermissionManageUsers)
	expectedRoles["system-administrator"] = administrator
}

func TestYU20PermissionIsSystemAdministratorOnlyAndPlansStayInternal(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	grantedBy := []string{}
	for _, role := range dictionary.Roles {
		for _, grant := range role.Grants {
			if grant.Permission == localmemberadmin.PermissionManageUsers {
				if !slices.Equal(grant.AllowedScopes, []string{"organization"}) {
					t.Fatalf("member admin grant scopes for role %q = %#v", role.ID, grant.AllowedScopes)
				}
				grantedBy = append(grantedBy, role.ID)
			}
		}
	}
	if !slices.Equal(grantedBy, []string{"system-administrator"}) {
		t.Fatalf("identity.users.manage granted by %#v, want system-administrator only", grantedBy)
	}
	for _, profile := range dictionary.DevelopmentCompatibility.LocalRoleProfiles {
		if slices.Contains(profile.Permissions, localmemberadmin.PermissionManageUsers) {
			t.Fatalf("development profile %q gained production member administration", profile.LocalRole)
		}
	}
	plans := localmemberadmin.OperationPlans()
	if len(plans) != 3 {
		t.Fatalf("member admin internal plan count=%d, want 3", len(plans))
	}
	if err := operationplan.Validate(operationplan.Set{SchemaVersion: operationplan.SchemaVersion, Operations: plans}); err != nil {
		t.Fatalf("member admin internal plan validation failed: %v", err)
	}
	for _, plan := range plans {
		if !plan.Security.TenantRequired || !slices.Equal(plan.Security.Authentication, []string{"jwt"}) || !slices.Equal(plan.Security.Permissions, []string{localmemberadmin.PermissionManageUsers}) || plan.Execution.Transaction != "local" || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 {
			t.Fatalf("member admin plan contract drift: %#v", plan)
		}
		for _, operation := range dictionary.Operations {
			if operation.ID == plan.OperationID {
				t.Fatalf("internal member admin operation %q leaked into service operation dictionary", plan.OperationID)
			}
		}
	}
}
