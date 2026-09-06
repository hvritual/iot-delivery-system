package backendyunka

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	_ "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const deliveryServiceFullName = protoreflect.FullName("iot.delivery.v1.DeliveryService")

func deliveryServiceDescriptor(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(deliveryServiceFullName)
	if err != nil {
		t.Fatalf("find DeliveryService descriptor: %v", err)
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("descriptor %q is %T, want protoreflect.ServiceDescriptor", deliveryServiceFullName, descriptor)
	}
	return service
}

func TestDeliveryContractDeclaresMVPReadAndGovernedMutationBoundaries(t *testing.T) {
	service := deliveryServiceDescriptor(t)
	for _, methodName := range []protoreflect.Name{
		"GetDashboard",
		"ListItems",
		"CreateItem",
		"UpdateItemContext",
		"AdvanceGate",
		"CloseItem",
	} {
		if method := service.Methods().ByName(methodName); method == nil {
			t.Fatalf("DeliveryService is missing RPC %q", methodName)
		}
	}
	for _, messageName := range []protoreflect.FullName{
		"iot.delivery.v1.WorkItem",
		"iot.delivery.v1.Evidence",
		"iot.delivery.v1.Decision",
	} {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(messageName)
		if err != nil {
			t.Fatalf("find delivery message %q: %v", messageName, err)
		}
		if _, ok := descriptor.(protoreflect.MessageDescriptor); !ok {
			t.Fatalf("delivery descriptor %q is %T, want protoreflect.MessageDescriptor", messageName, descriptor)
		}
	}
}

func TestPlanningCreateOperationsAreGeneratedContractBoundaries(t *testing.T) {
	service := deliveryServiceDescriptor(t)
	for _, methodName := range []protoreflect.Name{
		"CreateProject",
		"ListProjects",
		"CreateRelease",
		"ListReleases",
		"CreateSprint",
		"ListSprints",
		"CreateMilestone",
		"ListMilestones",
	} {
		if method := service.Methods().ByName(methodName); method == nil {
			t.Errorf("DeliveryService is missing RPC %q", methodName)
		}
	}

	for _, generatedPath := range []string{
		"internal/delivery/application/zz_yunka_management_application_port_gen.go",
		"internal/delivery/transport/rpc/zz_yunka_management_operation_executor_gen.go",
	} {
		generated, readErr := os.ReadFile(generatedPath)
		if readErr != nil {
			t.Fatalf("read generated contract artifact %s: %v", generatedPath, readErr)
		}
		for _, method := range []string{"CreateProject", "ListProjects", "CreateRelease", "ListReleases", "CreateSprint", "ListSprints", "CreateMilestone", "ListMilestones"} {
			if !strings.Contains(string(generated), method) {
				t.Errorf("generated contract artifact %s is missing %q", generatedPath, method)
			}
		}
	}
}

func TestGeneratedOperationPlansDeclareLocalAPIKeyAuthorizationAndTransactions(t *testing.T) {
	contents, err := os.ReadFile("contracts/generated/operation-plans.json")
	if err != nil {
		t.Fatalf("read generated operation plans: %v", err)
	}
	var plans operationplan.Set
	if err := json.Unmarshal(contents, &plans); err != nil {
		t.Fatalf("decode generated operation plans: %v", err)
	}
	want := map[string]struct {
		permission  string
		permissions []string
		transaction string
	}{
		"delivery.dashboard.get":        {permission: "delivery.dashboard.read", transaction: "read_only"},
		"delivery.items.list":           {permission: "delivery.work-items.read", transaction: "read_only"},
		"delivery.items.get":            {permission: "delivery.work-items.read", transaction: "read_only"},
		"delivery.items.search":         {permission: "delivery.work-items.read", transaction: "read_only"},
		"delivery.items.similarity":     {permission: "delivery.work-items.read", transaction: "read_only"},
		"delivery.items.create":         {permission: "delivery.work-items.create", transaction: "local"},
		"delivery.items.update":         {permissions: []string{"delivery.work-items.context.update", "delivery.work-items.update"}, transaction: "local"},
		"delivery.items.comment.create": {permission: "delivery.work-items.comment.create", transaction: "local"},
		"delivery.items.update-context": {permission: "delivery.work-items.context.update", transaction: "local"},
		"delivery.items.advance-gate":   {permission: "delivery.work-items.gate.advance", transaction: "local"},
		"delivery.items.close":          {permission: "delivery.work-items.close", transaction: "local"},
		"delivery.projects.create":      {permission: "delivery.projects.create", transaction: "local"},
		"delivery.projects.list":        {permission: "delivery.projects.read", transaction: "read_only"},
		"delivery.projects.progress":    {permission: "delivery.projects.read", transaction: "read_only"},
		"delivery.projects.schedule":    {permission: "delivery.projects.read", transaction: "read_only"},
		"delivery.notifications.list":   {permission: "delivery.work-items.read", transaction: "read_only"},
		"delivery.releases.create":      {permission: "delivery.releases.create", transaction: "local"},
		"delivery.releases.list":        {permission: "delivery.releases.read", transaction: "read_only"},
		"delivery.sprints.create":       {permission: "delivery.sprints.create", transaction: "local"},
		"delivery.sprints.list":         {permission: "delivery.sprints.read", transaction: "read_only"},
		"delivery.milestones.create":    {permission: "delivery.milestones.create", transaction: "local"},
		"delivery.milestones.list":      {permission: "delivery.milestones.read", transaction: "read_only"},
		"delivery.views.save":           {permission: "delivery.views.write", transaction: "local"},
		"delivery.views.list":           {permission: "delivery.views.read", transaction: "read_only"},
		"delivery.members.week":         {permission: "delivery.members.read", transaction: "read_only"},
	}
	if len(plans.Operations) != len(want) {
		t.Fatalf("generated operation count = %d, want %d", len(plans.Operations), len(want))
	}
	for _, plan := range plans.Operations {
		expected, ok := want[plan.OperationID]
		if !ok {
			t.Fatalf("unexpected generated operation %q", plan.OperationID)
		}
		if plan.Execution.Transaction != expected.transaction {
			t.Fatalf("operation %s transaction = %q, want %q", plan.OperationID, plan.Execution.Transaction, expected.transaction)
		}
		wantAuthentication := []string{"api-key", "jwt", "service-token"}
		if plan.OperationID == "delivery.views.save" || plan.OperationID == "delivery.views.list" {
			wantAuthentication = []string{"api-key", "jwt"}
		}
		if plan.Security.Public || !slices.Equal(plan.Security.Authentication, wantAuthentication) {
			t.Fatalf("operation %s security = %#v, want authentication %v", plan.OperationID, plan.Security, wantAuthentication)
		}
		expectedPermissions := expected.permissions
		if expectedPermissions == nil {
			expectedPermissions = []string{expected.permission}
		}
		if !slices.Equal(plan.Security.Permissions, expectedPermissions) {
			t.Fatalf("operation %s permissions = %#v, want %#v", plan.OperationID, plan.Security.Permissions, expectedPermissions)
		}
	}
}
