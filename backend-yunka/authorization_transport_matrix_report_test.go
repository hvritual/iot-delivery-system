package backendyunka

import "testing"

func TestAuthorizationTransportMatrixHasTwelveRPCAndTenMCPPublicOperations(t *testing.T) {
	dictionary := loadPermissionDictionary(t)
	plans := loadGeneratedOperationPlans(t)
	if err := validatePermissionDictionary(dictionary, plans); err != nil {
		t.Fatalf("authorization transport authority drift: %v", err)
	}
	generatedOperations := 0
	mcpTools := map[string]string{}
	for _, definition := range dictionary.Operations {
		if definition.Transports.GRPC == "" {
			continue
		}
		generatedOperations++
		if definition.Transports.GRPC == "" || len(definition.Transports.REST) == 0 {
			t.Fatalf("operation %q lacks REST or gRPC registration", definition.ID)
		}
		for _, tool := range definition.Transports.MCP {
			if prior, duplicate := mcpTools[tool]; duplicate {
				t.Fatalf("MCP tool %q is shared by %q and %q", tool, prior, definition.ID)
			}
			mcpTools[tool] = definition.ID
		}
	}
	if generatedOperations != 12 {
		t.Fatalf("REST/gRPC operation count = %d, want 12", generatedOperations)
	}
	if got := len(mcpTools); got != 10 {
		t.Fatalf("public MCP operation count = %d, want 10", got)
	}
}
