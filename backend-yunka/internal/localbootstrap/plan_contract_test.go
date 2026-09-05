package localbootstrap

import (
	"testing"

	"github.com/hvritual/yunka.io/pkg/operationplan"
)

func TestYU19BootstrapOperationPlanIsStandaloneCanonical(t *testing.T) {
	plan := OperationPlan()
	set := operationplan.Set{SchemaVersion: operationplan.SchemaVersion, Operations: []operationplan.Plan{plan}}
	if err := operationplan.Validate(set); err != nil {
		t.Fatalf("bootstrap operation plan is not canonical: %v", err)
	}
}
