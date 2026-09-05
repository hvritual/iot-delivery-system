package backendyunka

import "testing"

func TestAuthorizationTransportMatrixHasTwentyFiveRPCAndTwentyTwoMCPPublicOperations(t *testing.T) {
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
		if definition.Transports.GRPC == "" {
			t.Fatalf("operation %q lacks gRPC registration", definition.ID)
		}
		for _, tool := range definition.Transports.MCP {
			if prior, duplicate := mcpTools[tool]; duplicate {
				t.Fatalf("MCP tool %q is shared by %q and %q", tool, prior, definition.ID)
			}
			mcpTools[tool] = definition.ID
		}
	}
	if generatedOperations != 25 {
		t.Fatalf("REST/gRPC operation count = %d, want 25", generatedOperations)
	}
	if got := len(mcpTools); got != 22 {
		t.Fatalf("public MCP operation count = %d, want 22", got)
	}
}
