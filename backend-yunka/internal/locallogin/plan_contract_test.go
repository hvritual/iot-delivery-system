package locallogin

import (
	"testing"

	"github.com/hvritual/yunka.io/pkg/operationplan"
)

func TestYU21LoginPlanIsInternalUnprotectedLocalTransaction(t *testing.T) {
	plan := OperationPlan()
	if err := operationplan.Validate(operationplan.Set{SchemaVersion: operationplan.SchemaVersion, Operations: []operationplan.Plan{plan}}); err != nil {
		t.Fatalf("login plan validation: %v", err)
	}
	if plan.OperationID != OperationLogin || plan.Security.Public || plan.Security.TenantRequired || len(plan.Security.Authentication) != 0 || len(plan.Security.Permissions) != 0 || plan.Execution.Transaction != "local" || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 {
		t.Fatalf("login plan drift: %#v", plan)
	}
}
