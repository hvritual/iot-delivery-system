package localprojectroleadmin

import (
	"slices"

	"github.com/hvritual/yunka.io/pkg/operationplan"
)

const (
	PermissionManageRoleBindings = "identity.role-bindings.manage"
	OperationAssignProjectRole   = "identity.project-role-bindings.assign"
	OperationRevokeProjectRole   = "identity.project-role-bindings.revoke"
)

var assignProjectRolePlan = projectRolePlan(OperationAssignProjectRole, "localprojectroleadmin.AssignInput")
var revokeProjectRolePlan = projectRolePlan(OperationRevokeProjectRole, "localprojectroleadmin.RevokeInput")

func projectRolePlan(operationID, requestType string) operationplan.Plan {
	return operationplan.Plan{
		OperationID:  operationID,
		Domain:       "identity",
		Application:  "project-role-bindings",
		UseCase:      operationID,
		RequestType:  requestType,
		ResponseType: "localprojectroleadmin.BindingResult",
		Security: operationplan.Security{
			TenantRequired: true,
			Authentication: []string{"jwt"},
			Permissions:    []string{PermissionManageRoleBindings},
			PermissionMode: "all",
		},
		Execution:   operationplan.Execution{Transaction: "local", Idempotency: "none"},
		Composition: operationplan.Composition{Boundary: "local"},
	}
}

// OperationPlans is the canonical internal registration for YU-24 project
// RoleBinding administration. Remote BFF exposure remains YU-26 work.
func OperationPlans() []operationplan.Plan {
	return []operationplan.Plan{clonePlan(assignProjectRolePlan), clonePlan(revokeProjectRolePlan)}
}

func clonePlan(plan operationplan.Plan) operationplan.Plan {
	plan.Security.Authentication = slices.Clone(plan.Security.Authentication)
	plan.Security.Permissions = slices.Clone(plan.Security.Permissions)
	plan.Composition.RequiresOperations = slices.Clone(plan.Composition.RequiresOperations)
	plan.Composition.PermissionClosure = slices.Clone(plan.Composition.PermissionClosure)
	plan.ApplicationRequires = slices.Clone(plan.ApplicationRequires)
	plan.Bindings.HTTP = slices.Clone(plan.Bindings.HTTP)
	return plan
}
