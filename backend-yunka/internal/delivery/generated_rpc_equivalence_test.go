package delivery_test

import (
	"bytes"
	"path/filepath"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGeneratedRPCOperationPlansRemainCanonical(t *testing.T) {
	t.Parallel()
	// This test proves the descriptor RPC/type bijection and full normalized
	// equality between the JSON artifact and generated Go policy plans. The
	// protobuf operation options and generated transport source remain guarded
	// by the canonical, read-only `make yunka-check` regeneration comparison.

	const (
		serviceName       = "iot.delivery.v1.DeliveryService"
		generatedRPCCount = 16
	)

	services := deliveryv1.File_iot_delivery_proto.Services()
	if services.Len() != 1 {
		t.Fatalf("generated descriptor service count = %d, want 1", services.Len())
	}
	service := services.Get(0)
	if got := string(service.FullName()); got != serviceName {
		t.Fatalf("generated descriptor service = %q, want %q", got, serviceName)
	}
	if got := service.Methods().Len(); got != generatedRPCCount {
		t.Fatalf("generated descriptor RPC count = %d, want %d", got, generatedRPCCount)
	}
	for _, methodName := range []string{"ListReleases", "ListSprints", "ListMilestones"} {
		if method := service.Methods().ByName(protoreflect.Name(methodName)); method == nil {
			t.Errorf("generated descriptor is missing %s", methodName)
		}
	}

	filePlans, err := operationplan.Load(filepath.Join("..", "..", "contracts", "generated", "operation-plans.json"))
	if err != nil {
		t.Fatalf("load canonical operation plans: %v", err)
	}
	if got := len(filePlans.Operations); got != generatedRPCCount {
		t.Fatalf("canonical operation plan count = %d, want %d", got, generatedRPCCount)
	}

	byRPC := make(map[string]operationplan.Plan, len(filePlans.Operations))
	for _, plan := range filePlans.Operations {
		if plan.Bindings.RPC == "" {
			t.Fatalf("canonical operation %q has no RPC binding", plan.OperationID)
		}
		if previous, exists := byRPC[plan.Bindings.RPC]; exists {
			t.Fatalf("RPC binding %q is shared by %q and %q", plan.Bindings.RPC, previous.OperationID, plan.OperationID)
		}
		byRPC[plan.Bindings.RPC] = plan
	}

	for index := 0; index < service.Methods().Len(); index++ {
		method := service.Methods().Get(index)
		rpcBinding := "/" + serviceName + "/" + string(method.Name())
		plan, exists := byRPC[rpcBinding]
		if !exists {
			t.Fatalf("generated descriptor RPC %q has no canonical operation plan", rpcBinding)
		}
		if got, want := plan.RequestType, string(method.Input().FullName()); got != want {
			t.Errorf("operation %s request type = %q, want %q", plan.OperationID, got, want)
		}
		if got, want := plan.ResponseType, string(method.Output().FullName()); got != want {
			t.Errorf("operation %s response type = %q, want %q", plan.OperationID, got, want)
		}
	}

	generatedPolicyPlans := operationplan.Set{
		SchemaVersion: operationplan.SchemaVersion,
		Operations: []operationplan.Plan{
			policy.OperationPlanAdvanceGate(),
			policy.OperationPlanCloseItem(),
			policy.OperationPlanCreateItem(),
			policy.OperationPlanCreateItemComment(),
			policy.OperationPlanCreateMilestone(),
			policy.OperationPlanCreateProject(),
			policy.OperationPlanCreateRelease(),
			policy.OperationPlanCreateSprint(),
			policy.OperationPlanGetDashboard(),
			policy.OperationPlanListItems(),
			policy.OperationPlanListProjects(),
			policy.OperationPlanUpdateItem(),
			policy.OperationPlanUpdateItemContext(),
		},
	}

	fileCanonical, err := operationplan.CanonicalJSON(filePlans)
	if err != nil {
		t.Fatalf("canonicalize operation plan artifact: %v", err)
	}
	policyCanonical, err := operationplan.CanonicalJSON(generatedPolicyPlans)
	if err != nil {
		t.Fatalf("canonicalize generated policy plans: %v", err)
	}
	if !bytes.Equal(policyCanonical, fileCanonical) {
		fileDigest, fileDigestErr := operationplan.Digest(filePlans)
		policyDigest, policyDigestErr := operationplan.Digest(generatedPolicyPlans)
		if fileDigestErr != nil || policyDigestErr != nil {
			t.Fatalf("generated policy plans differ from canonical artifact and digest failed: artifact=%v policy=%v", fileDigestErr, policyDigestErr)
		}
		t.Fatalf("generated policy plans differ from canonical artifact: artifact digest=%s policy digest=%s", fileDigest, policyDigest)
	}
}
