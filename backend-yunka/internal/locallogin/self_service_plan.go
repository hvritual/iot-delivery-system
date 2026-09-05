package locallogin

import "github.com/hvritual/yunka.io/pkg/operationplan"

const (
	OperationCurrentMember  = "identity.local-session.current"
	OperationLogout         = "identity.local-session.logout"
	OperationChangePassword = "identity.local-session.change-password"
)

var currentMemberPlan = operationplan.Plan{
	OperationID:  OperationCurrentMember,
	Domain:       "identity",
	Application:  "local-session",
	UseCase:      "current-member",
	RequestType:  "locallogin.CurrentMemberInput",
	ResponseType: "locallogin.CurrentMember",
	Security:     operationplan.Security{PermissionMode: "all"},
	Execution:    operationplan.Execution{Transaction: "none", Idempotency: "none"},
	Composition:  operationplan.Composition{Boundary: "local"},
}

var logoutPlan = operationplan.Plan{
	OperationID:  OperationLogout,
	Domain:       "identity",
	Application:  "local-session",
	UseCase:      "logout",
	RequestType:  "locallogin.LogoutInput",
	ResponseType: "locallogin.LogoutResult",
	Security:     operationplan.Security{PermissionMode: "all"},
	Execution:    operationplan.Execution{Transaction: "local", Idempotency: "none"},
	Composition:  operationplan.Composition{Boundary: "local"},
}

var changePasswordPlan = operationplan.Plan{
	OperationID:  OperationChangePassword,
	Domain:       "identity",
	Application:  "local-session",
	UseCase:      "change-password",
	RequestType:  "locallogin.ChangePasswordInput",
	ResponseType: "locallogin.ChangePasswordResult",
	Security:     operationplan.Security{PermissionMode: "all"},
	Execution:    operationplan.Execution{Transaction: "local", Idempotency: "none"},
	Composition:  operationplan.Composition{Boundary: "local"},
}

// SelfServiceOperationPlans returns YU-22's internal self-service contracts.
// They remain transport-free until YU-26. Authentication is performed by the
// verified local session/JWT boundary inside this consumer capability; YU-23
// later wires the same facts into Yunka's shared transport security phase.
func SelfServiceOperationPlans() []operationplan.Plan {
	return []operationplan.Plan{currentMemberPlan, logoutPlan, changePasswordPlan}
}
