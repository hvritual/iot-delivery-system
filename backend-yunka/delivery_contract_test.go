package backendyunka

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"yunka.io/pkg/operationplan"
)

func TestDeliveryContractDeclaresMVPReadAndGovernedMutationBoundaries(t *testing.T) {
	contents, err := os.ReadFile("contracts/proto/iot_delivery.proto")
	if err != nil {
		t.Fatalf("read delivery proto contract: %v", err)
	}
	contract := string(contents)
	for _, declaration := range []string{
		"service DeliveryService",
		"rpc GetDashboard",
		"rpc ListItems",
		"rpc CreateItem",
		"rpc UpdateItemContext",
		"rpc AdvanceGate",
		"rpc CloseItem",
		"message WorkItem",
		"message Evidence",
		"message Decision",
	} {
		if !strings.Contains(contract, declaration) {
			t.Fatalf("delivery contract is missing %q", declaration)
		}
	}
}

func TestPlanningCreateOperationsAreGeneratedContractBoundaries(t *testing.T) {
	contents, err := os.ReadFile("contracts/proto/iot_delivery.proto")
	if err != nil {
		t.Fatalf("read delivery proto contract: %v", err)
	}
	contract := string(contents)
	for _, declaration := range []string{
		"rpc CreateProject(CreateProjectRequest) returns (ProjectResponse)",
		"rpc CreateRelease(CreateReleaseRequest) returns (ReleaseResponse)",
		"rpc CreateSprint(CreateSprintRequest) returns (SprintResponse)",
		"rpc CreateMilestone(CreateMilestoneRequest) returns (MilestoneResponse)",
	} {
		if !strings.Contains(contract, declaration) {
			t.Errorf("delivery proto contract is missing %q", declaration)
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
		for _, method := range []string{"CreateProject", "CreateRelease", "CreateSprint", "CreateMilestone"} {
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
		transaction string
	}{
		"delivery.dashboard.get":        {permission: "delivery.dashboard.read", transaction: "read_only"},
		"delivery.items.list":           {permission: "delivery.items.read", transaction: "read_only"},
		"delivery.items.create":         {permission: "delivery.items.write", transaction: "local"},
		"delivery.items.update":         {permission: "delivery.items.write", transaction: "local"},
		"delivery.items.comment.create": {permission: "delivery.items.write", transaction: "local"},
		"delivery.items.update-context": {permission: "delivery.items.write", transaction: "local"},
		"delivery.items.advance-gate":   {permission: "delivery.items.gate", transaction: "local"},
		"delivery.items.close":          {permission: "delivery.items.close", transaction: "local"},
		"delivery.projects.create":       {permission: "delivery.items.write", transaction: "local"},
		"delivery.releases.create":       {permission: "delivery.items.write", transaction: "local"},
		"delivery.sprints.create":        {permission: "delivery.items.write", transaction: "local"},
		"delivery.milestones.create":     {permission: "delivery.items.write", transaction: "local"},
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
		if plan.Security.Public || len(plan.Security.Authentication) != 1 || plan.Security.Authentication[0] != "api-key" {
			t.Fatalf("operation %s security = %#v, want local API key protection", plan.OperationID, plan.Security)
		}
		if len(plan.Security.Permissions) != 1 || plan.Security.Permissions[0] != expected.permission {
			t.Fatalf("operation %s permissions = %#v, want %q", plan.OperationID, plan.Security.Permissions, expected.permission)
		}
	}
}
