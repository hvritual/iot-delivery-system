package locallogin

import "github.com/hvritual/yunka.io/pkg/operationplan"

const OperationLogin = "identity.local-login.authenticate"

var loginPlan = operationplan.Plan{
	OperationID:  OperationLogin,
	Domain:       "identity",
	Application:  "local-login",
	UseCase:      "authenticate",
	RequestType:  "locallogin.LoginInput",
	ResponseType: "locallogin.LoginResult",
	Security:     operationplan.Security{PermissionMode: "all"},
	Execution:    operationplan.Execution{Transaction: "local", Idempotency: "none"},
	Composition:  operationplan.Composition{Boundary: "local"},
}

// OperationPlan returns the internal pre-authentication contract. It has no
// authentication/permission requirement because it is the boundary that
// establishes a human identity after password verification. No transport is
// bound in YU-21.
func OperationPlan() operationplan.Plan { return loginPlan }
