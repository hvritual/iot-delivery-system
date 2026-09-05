package mcpserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestYU23MCPReResolvesPrincipalForEveryToolInvocation(t *testing.T) {
	fixture := newExecutionFixture(t)
	admin := fixture.principal(t, "mcp-grpc-admin-key")
	calls := 0
	server := mcpserver.NewWithPrincipalResolver(fixture.operations, func(context.Context) (identity.Principal, error) {
		calls++
		if calls == 1 {
			return admin, nil
		}
		return identity.Principal{}, errors.New("session revoked")
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "yu23-dynamic-principal", Version: "v1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	first, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "delivery.list_projects", Arguments: map[string]any{}})
	if err != nil || first.IsError {
		t.Fatalf("first MCP call result=%#v error=%v", first, err)
	}
	second, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{Name: "delivery.list_projects", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("second MCP call transport error=%v", err)
	}
	if !second.IsError || mcpErrorText(second) != "unauthenticated" || calls != 2 {
		t.Fatalf("second MCP call result=%#v text=%q resolver calls=%d", second, mcpErrorText(second), calls)
	}
}
