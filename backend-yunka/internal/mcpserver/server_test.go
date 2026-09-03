package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yunka.io/framework/core/identity"
)

func TestServerManagesLocalDeliveryLifecycleThroughMCPTools(t *testing.T) {
	ctx := context.Background()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	server := mcpserver.New(service, identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		UserID:        "mcp-local-admin",
		Roles:         []string{"local-admin"},
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer clientSession.Close()

	projectResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "delivery.create_project",
		Arguments: map[string]any{
			"name":  "MCP OTA 交付",
			"board": "研发交付效能",
			"owner": "mcp-local-admin",
		},
	})
	if err != nil || projectResult.IsError {
		t.Fatalf("create project through MCP: result=%#v error=%v", projectResult, err)
	}
	var project struct {
		Project delivery.Project `json:"project"`
	}
	if err := decodeStructured(projectResult, &project); err != nil {
		t.Fatalf("decode MCP project result: %v", err)
	}
	if project.Project.ID == "" {
		t.Fatalf("MCP project = %#v, want ID", project.Project)
	}

	itemResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "delivery.create_work_item",
		Arguments: map[string]any{
			"title":     "MCP 创建灰度任务",
			"board":     "研发交付效能",
			"owner":     "mcp-local-admin",
			"projectId": project.Project.ID,
			"kind":      "task",
		},
	})
	if err != nil || itemResult.IsError {
		t.Fatalf("create work item through MCP: result=%#v error=%v", itemResult, err)
	}
	var item struct {
		Created delivery.WorkItem `json:"created"`
	}
	if err := decodeStructured(itemResult, &item); err != nil {
		t.Fatalf("decode MCP item result: %v", err)
	}
	if item.Created.ProjectID != project.Project.ID {
		t.Fatalf("MCP work item = %#v, want project %q", item.Created, project.Project.ID)
	}

	progressResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delivery.get_project_progress",
		Arguments: map[string]any{"projectId": project.Project.ID},
	})
	if err != nil || progressResult.IsError {
		t.Fatalf("get project progress through MCP: result=%#v error=%v", progressResult, err)
	}
	var progress struct {
		Progress delivery.ProjectProgress `json:"progress"`
	}
	if err := decodeStructured(progressResult, &progress); err != nil {
		t.Fatalf("decode MCP progress result: %v", err)
	}
	if progress.Progress.ProjectID != project.Project.ID || progress.Progress.TotalItems != 1 {
		t.Fatalf("MCP project progress = %#v, want one project item", progress.Progress)
	}

	scheduleResult, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "delivery.get_project_schedule",
		Arguments: map[string]any{"projectId": project.Project.ID},
	})
	if err != nil || scheduleResult.IsError {
		t.Fatalf("get project schedule through MCP: result=%#v error=%v", scheduleResult, err)
	}
	var schedule struct {
		Schedule delivery.ProjectSchedule `json:"schedule"`
	}
	if err := decodeStructured(scheduleResult, &schedule); err != nil {
		t.Fatalf("decode MCP schedule result: %v", err)
	}
	if schedule.Schedule.ProjectID != project.Project.ID || schedule.Schedule.TotalItems != 1 {
		t.Fatalf("MCP project schedule = %#v, want one project item", schedule.Schedule)
	}
}

func decodeStructured(result *mcp.CallToolResult, target any) error {
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, target)
}
