package localmemberadmin

import (
	"slices"

	"github.com/hvritual/yunka.io/pkg/operationplan"
)

const (
	OperationCreateMember       = "identity.members.create"
	OperationDisableMember      = "identity.members.disable"
	OperationResetCredential    = "identity.members.credentials.reset"
)

var createMemberPlan = memberAdminPlan(OperationCreateMember, "localmemberadmin.CreateInput", "localmemberadmin.MemberResult")
var disableMemberPlan = memberAdminPlan(OperationDisableMember, "localmemberadmin.DisableInput", "localmemberadmin.MemberResult")
var resetCredentialPlan = memberAdminPlan(OperationResetCredential, "localmemberadmin.ResetCredentialInput", "localmemberadmin.MemberResult")

func memberAdminPlan(operationID, requestType, responseType string) operationplan.Plan {
	return operationplan.Plan{
		OperationID:   operationID,
		Domain:        "identity",
		Application:   "members",
		UseCase:       operationID,
		RequestType:   requestType,
		ResponseType:  responseType,
		Security: operationplan.Security{
			TenantRequired: true,
			Authentication: []string{"jwt"},
			Permissions:    []string{PermissionManageUsers},
			PermissionMode: "all",
		},
		Execution: operationplan.Execution{Transaction: "local", Idempotency: "none"},
		Composition: operationplan.Composition{Boundary: "local"},
	}
}

// OperationPlans is the canonical internal registration for YU-20 member administration.
// The operations deliberately have no transport bindings; YU-26 owns BFF exposure.
func OperationPlans() []operationplan.Plan {
	return []operationplan.Plan{clonePlan(createMemberPlan), clonePlan(disableMemberPlan), clonePlan(resetCredentialPlan)}
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
