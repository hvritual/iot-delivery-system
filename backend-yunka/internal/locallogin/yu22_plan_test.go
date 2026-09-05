package locallogin

import (
	"testing"

	"github.com/hvritual/yunka.io/pkg/operationplan"
)

func TestYU22SelfServicePlansAreInternalAndUseExpectedTransactions(t *testing.T) {
	plans := SelfServiceOperationPlans()
	if len(plans) != 3 {
		t.Fatalf("YU-22 self-service plan count=%d, want 3", len(plans))
	}
	if err := operationplan.Validate(operationplan.Set{SchemaVersion: operationplan.SchemaVersion, Operations: plans}); err != nil {
		t.Fatalf("YU-22 self-service plans invalid: %v", err)
	}
	transactions := map[string]string{
		OperationCurrentMember:  "none",
		OperationLogout:         "local",
		OperationChangePassword: "local",
	}
	for _, plan := range plans {
		if plan.Execution.Transaction != transactions[plan.OperationID] || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 || plan.Security.TenantRequired || len(plan.Security.Authentication) != 0 || len(plan.Security.Permissions) != 0 {
			t.Fatalf("YU-22 internal plan drift: %#v", plan)
		}
	}
}
