package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	deliveryrpc "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/transport/rpc"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
)

type executionFixture struct {
	repository    *delivery.SQLiteRepository
	outbox        *localoutbox.SQLiteStore
	operations    *deliveryapplication.Operations
	authenticator *localauth.Authenticator
	grpcClient    deliveryv1.DeliveryServiceClient
	connection    *grpc.ClientConn
	grpcServer    *grpc.Server
	listener      *bufconn.Listener
	observer      *operationObserver
}

type operationObserver struct {
	mu     sync.Mutex
	events []operation.Event
}

func (observer *operationObserver) Observe(_ context.Context, event operation.Event) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.events = append(observer.events, event)
}

func (observer *operationObserver) successfulOperations() map[string]int {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	result := make(map[string]int)
	for _, event := range observer.events {
		if event.Kind == operation.InvocationRoot && event.Phase == operation.PhaseOutcome && event.Outcome == operation.OutcomeSuccess {
			result[event.OperationID]++
		}
	}
	return result
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	t.Setenv(localauth.APIKeyEnvironment, "mcp-grpc-admin-key")
	t.Setenv(localauth.ViewerAPIKeyEnvironment, "mcp-grpc-viewer-key")
	authenticator, err := localauth.FromEnvironment()
	if err != nil {
		t.Fatalf("create local authenticator: %v", err)
	}
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite audit store: %v", err)
	}
	auditRecorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		t.Fatalf("open security audit recorder: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	observer := &operationObserver{}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	audited, err := deliveryapplication.NewAuditedDeliveryService(
		deliveryapplication.NewAdapter(service),
		auditStore,
		deliveryapplication.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		t.Fatalf("assemble audited application: %v", err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(
		security,
		operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())},
		observer,
	), auditRecorder)
	if err != nil {
		t.Fatalf("assemble recording executor: %v", err)
	}
	operations := deliveryapplication.NewOperations(audited, executor, service)
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			ctx = runtimecontext.WithTraceID(ctx, "grpc-trace-a")
			ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "grpc", Protocol: "grpc", Operation: info.FullMethod, RequestID: "grpc-request-a"})
			return handler(ctx, request)
		},
		authenticator.GRPCUnaryServerInterceptor(),
	))
	if err := deliveryrpc.RegisterOperationExecutor(grpcServer, audited, executor); err != nil {
		t.Fatalf("register generated gRPC operation executor: %v", err)
	}
	go func() { _ = grpcServer.Serve(listener) }()
	connection, err := grpc.DialContext(t.Context(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn gRPC server: %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		grpcServer.Stop()
		_ = listener.Close()
	})
	return &executionFixture{repository: repository, outbox: outbox, operations: operations, authenticator: authenticator, grpcClient: deliveryv1.NewDeliveryServiceClient(connection), connection: connection, grpcServer: grpcServer, listener: listener, observer: observer}
}

func (fixture *executionFixture) principal(t *testing.T, key string) identity.Principal {
	t.Helper()
	principal, err := fixture.authenticator.AuthenticateAPIKey(key)
	if err != nil {
		t.Fatalf("authenticate local test principal: %v", err)
	}
	return principal
}

func grpcContext(ctx context.Context, key string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, strings.ToLower(localauth.APIKeyHeader), key)
}

func callMCP(t *testing.T, operations *deliveryapplication.Operations, principal identity.Principal, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	server := mcpserver.New(operations, principal)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-grpc-boundary-test", Version: "v1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer clientSession.Close()
	ctx := runtimecontext.WithTraceID(t.Context(), "mcp-trace-a")
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call MCP tool %s: %v", name, err)
	}
	return result
}

func decodeStructured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal MCP structured result: %v", err)
	}
	if string(payload) == "null" && len(result.Content) == 1 {
		var contents []struct {
			Text string `json:"text"`
		}
		contentJSON, contentErr := json.Marshal(result.Content)
		if contentErr != nil {
			t.Fatalf("marshal MCP text content: %v", contentErr)
		}
		if contentErr := json.Unmarshal(contentJSON, &contents); contentErr != nil || len(contents) != 1 {
			t.Fatalf("decode MCP text content: %v; content=%s", contentErr, contentJSON)
		}
		payload = []byte(contents[0].Text)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode MCP structured result: %v", err)
	}
}

func mcpErrorText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(*mcp.TextContent); ok {
		return content.Text
	}
	return ""
}

func TestMCPAndGeneratedGRPCShareCreateAndAdvanceGateExecution(t *testing.T) {
	grpcFixture := newExecutionFixture(t)
	mcpFixture := newExecutionFixture(t)
	adminKey := "mcp-grpc-admin-key"
	admin := mcpFixture.principal(t, adminKey)
	grpcBefore, err := grpcFixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot initial gRPC outbox: %v", err)
	}
	mcpBefore, err := mcpFixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot initial MCP outbox: %v", err)
	}
	grpcCreated, err := grpcFixture.grpcClient.CreateItem(grpcContext(t.Context(), adminKey), &deliveryv1.CreateItemRequest{Title: "cross transport create", Board: string(delivery.BoardResearchDelivery), Owner: "delivery-owner", Priority: string(delivery.PriorityP1), Kind: string(delivery.WorkItemKindTask)})
	if err != nil {
		t.Fatalf("create item through generated gRPC: %v", err)
	}
	mcpCreatedResult := callMCP(t, mcpFixture.operations, admin, "delivery.create_work_item", map[string]any{"title": "cross transport create", "board": string(delivery.BoardResearchDelivery), "owner": "delivery-owner", "priority": string(delivery.PriorityP1), "kind": string(delivery.WorkItemKindTask)})
	if mcpCreatedResult.IsError {
		t.Fatalf("create item through MCP = %#v: %s", mcpCreatedResult, mcpErrorText(mcpCreatedResult))
	}
	var mcpCreated struct {
		Created delivery.WorkItem `json:"created"`
	}
	decodeStructured(t, mcpCreatedResult, &mcpCreated)
	assertEquivalentItem(t, grpcCreated.GetItem(), mcpCreated.Created)
	grpcAfterCreate, err := grpcFixture.outbox.Snapshot(t.Context())
	if err != nil || grpcAfterCreate.Pending != grpcBefore.Pending+1 {
		t.Fatalf("gRPC create outbox = %#v, %v; want pending %d", grpcAfterCreate, err, grpcBefore.Pending+1)
	}
	mcpAfterCreate, err := mcpFixture.outbox.Snapshot(t.Context())
	if err != nil || mcpAfterCreate.Pending != mcpBefore.Pending+1 {
		t.Fatalf("MCP create outbox = %#v, %v; want pending %d", mcpAfterCreate, err, mcpBefore.Pending+1)
	}
	assertStoredItemCount(t, grpcFixture.repository, 1)
	assertStoredItemCount(t, mcpFixture.repository, 1)
	grpcAdvanced, err := grpcFixture.grpcClient.AdvanceGate(grpcContext(t.Context(), adminKey), &deliveryv1.AdvanceGateRequest{Id: grpcCreated.GetItem().GetId(), ExpectedRevision: grpcCreated.GetItem().GetRevision(), Gate: string(delivery.GateSolutionReviewed), Evidence: []*deliveryv1.Evidence{{Kind: "review", Title: "solution approved", Reference: "ADR-CROSS-001"}}})
	if err != nil {
		t.Fatalf("advance gate through generated gRPC: %v", err)
	}
	mcpAdvancedResult := callMCP(t, mcpFixture.operations, admin, "delivery.advance_gate", map[string]any{"id": mcpCreated.Created.ID, "expectedRevision": mcpCreated.Created.Revision, "gate": string(delivery.GateSolutionReviewed), "evidence": []map[string]any{{"kind": "review", "title": "solution approved", "reference": "ADR-CROSS-001"}}})
	if mcpAdvancedResult.IsError {
		t.Fatalf("advance gate through MCP = %#v: %s", mcpAdvancedResult, mcpErrorText(mcpAdvancedResult))
	}
	var mcpAdvanced struct {
		Item delivery.WorkItem `json:"item"`
	}
	decodeStructured(t, mcpAdvancedResult, &mcpAdvanced)
	assertEquivalentItem(t, grpcAdvanced.GetItem(), mcpAdvanced.Item)
	if grpcAdvanced.GetItem().GetEvidence()[0].GetRecordedAt() == nil || mcpAdvanced.Item.Evidence[0].RecordedAt.IsZero() {
		t.Fatalf("AdvanceGate did not fill missing evidence timestamps: gRPC=%#v MCP=%#v", grpcAdvanced.GetItem().GetEvidence()[0], mcpAdvanced.Item.Evidence[0])
	}
	grpcAfterGate, err := grpcFixture.outbox.Snapshot(t.Context())
	if err != nil || grpcAfterGate.Pending != grpcBefore.Pending+2 {
		t.Fatalf("gRPC advance gate outbox = %#v, %v; want pending %d", grpcAfterGate, err, grpcBefore.Pending+2)
	}
	mcpAfterGate, err := mcpFixture.outbox.Snapshot(t.Context())
	if err != nil || mcpAfterGate.Pending != mcpBefore.Pending+2 {
		t.Fatalf("MCP advance gate outbox = %#v, %v; want pending %d", mcpAfterGate, err, mcpBefore.Pending+2)
	}
	assertStoredGate(t, grpcFixture.repository, grpcCreated.GetItem().GetId(), delivery.GateSolutionReviewed)
	assertStoredGate(t, mcpFixture.repository, mcpCreated.Created.ID, delivery.GateSolutionReviewed)
	assertTransportAuditEntry(t, grpcFixture.repository, "delivery.items.create", grpcCreated.GetItem().GetId(), "grpc", "grpc-trace-a")
	assertTransportAuditEntry(t, grpcFixture.repository, "delivery.items.advance-gate", grpcCreated.GetItem().GetId(), "grpc", "grpc-trace-a")
	assertTransportAuditEntry(t, mcpFixture.repository, "delivery.items.create", mcpCreated.Created.ID, "mcp", "")
	assertTransportAuditEntry(t, mcpFixture.repository, "delivery.items.advance-gate", mcpCreated.Created.ID, "mcp", "")
	for _, operationID := range []string{"delivery.items.create", "delivery.items.advance-gate"} {
		if grpcFixture.observer.successfulOperations()[operationID] != 1 || mcpFixture.observer.successfulOperations()[operationID] != 1 {
			t.Fatalf("successful %s operation IDs do not match across generated gRPC and MCP", operationID)
		}
	}
}

func assertTransportAuditEntry(t *testing.T, repository *delivery.SQLiteRepository, operationID, targetID, transport, traceID string) {
	t.Helper()
	var actorType, actorID, storedTargetID, result, storedTraceID, metadata string
	err := repository.Database().QueryRowContext(t.Context(), `SELECT actor_type, actor_id, target_id, result, COALESCE(trace_id, ''), metadata
FROM iotd_audit_entries WHERE operation = ? ORDER BY sequence DESC LIMIT 1`, operationID).Scan(&actorType, &actorID, &storedTargetID, &result, &storedTraceID, &metadata)
	if err != nil {
		t.Fatalf("read %s audit entry: %v", operationID, err)
	}
	if actorType != string(audit.ActorSystem) || actorID != "development-api-key" || storedTargetID != targetID || result != string(audit.ResultSuccess) || storedTraceID != traceID {
		t.Fatalf("%s audit fields = actor=%s/%s target=%s result=%s trace=%s", operationID, actorType, actorID, storedTargetID, result, storedTraceID)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil || parsed["transport"] != transport {
		t.Fatalf("%s audit metadata = %q, %v", operationID, metadata, err)
	}
}

func TestMCPAndGeneratedGRPCRejectViewerWritesWithoutSideEffects(t *testing.T) {
	fixture := newExecutionFixture(t)
	adminKey, viewerKey := "mcp-grpc-admin-key", "mcp-grpc-viewer-key"
	viewer := fixture.principal(t, viewerKey)
	created, err := fixture.grpcClient.CreateItem(grpcContext(t.Context(), adminKey), &deliveryv1.CreateItemRequest{Title: "protected item", Board: string(delivery.BoardResearchDelivery), Owner: "delivery-owner", Kind: string(delivery.WorkItemKindTask)})
	if err != nil {
		t.Fatalf("create protected item: %v", err)
	}
	beforeItem, err := fixture.repository.Get(t.Context(), created.GetItem().GetId())
	if err != nil {
		t.Fatalf("read protected item: %v", err)
	}
	beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot before denied writes: %v", err)
	}
	_, err = fixture.grpcClient.AdvanceGate(grpcContext(t.Context(), viewerKey), &deliveryv1.AdvanceGateRequest{Id: beforeItem.ID, ExpectedRevision: beforeItem.Revision, Gate: string(delivery.GateSolutionReviewed), Evidence: []*deliveryv1.Evidence{{Kind: "review", Title: "denied"}}})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("viewer gRPC advance gate code = %s, want PermissionDenied; error=%v", status.Code(err), err)
	}
	mcpDenied := callMCP(t, fixture.operations, viewer, "delivery.advance_gate", map[string]any{"id": beforeItem.ID, "expectedRevision": beforeItem.Revision, "gate": string(delivery.GateSolutionReviewed), "evidence": []map[string]any{{"kind": "review", "title": "denied"}}})
	if !mcpDenied.IsError {
		t.Fatalf("viewer MCP advance gate = %#v, want tool error", mcpDenied)
	}
	afterItem, err := fixture.repository.Get(t.Context(), beforeItem.ID)
	if err != nil || !sameItem(beforeItem, afterItem) {
		t.Fatalf("denied writes changed item = %#v, %v; want %#v", afterItem, err, beforeItem)
	}
	afterOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || afterOutbox != beforeOutbox {
		t.Fatalf("denied writes changed outbox = %#v, %v; want %#v", afterOutbox, err, beforeOutbox)
	}
	var authorizationAuditCount int
	if err := fixture.repository.Database().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM iotd_audit_entries WHERE event_category = 'authorization' AND result = 'denied'`).Scan(&authorizationAuditCount); err != nil || authorizationAuditCount != 2 {
		t.Fatalf("gRPC and MCP authorization audits = %d error=%v, want 2", authorizationAuditCount, err)
	}
	_, err = fixture.grpcClient.CreateItem(t.Context(), &deliveryv1.CreateItemRequest{Title: "unauthenticated", Board: string(delivery.BoardResearchDelivery), Owner: "viewer"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing gRPC API key code = %s, want Unauthenticated; error=%v", status.Code(err), err)
	}
	mcpUnauthenticated := callMCP(t, fixture.operations, identity.Principal{}, "delivery.create_work_item", map[string]any{"title": "unauthenticated", "board": string(delivery.BoardResearchDelivery), "owner": "viewer"})
	if !mcpUnauthenticated.IsError {
		t.Fatalf("unauthenticated MCP create = %#v, want tool error", mcpUnauthenticated)
	}
}

func TestMCPAuthorizationErrorsUseStableNormalizedCategories(t *testing.T) {
	fixture := newExecutionFixture(t)
	viewer := fixture.principal(t, "mcp-grpc-viewer-key")

	denied := callMCP(t, fixture.operations, viewer, "delivery.create_work_item", map[string]any{
		"title": "viewer must not create", "board": string(delivery.BoardResearchDelivery), "owner": "viewer",
	})
	if !denied.IsError || mcpErrorText(denied) != "permission_denied" {
		t.Fatalf("viewer MCP create result = %#v text=%q, want stable permission_denied", denied, mcpErrorText(denied))
	}
	if text := mcpErrorText(denied); strings.Contains(text, "grant") || strings.Contains(text, "role") || strings.Contains(text, "sql") {
		t.Fatalf("viewer MCP create leaked authorization internals: %q", text)
	}

	unauthenticated := callMCP(t, fixture.operations, identity.Principal{}, "delivery.create_work_item", map[string]any{
		"title": "unauthenticated", "board": string(delivery.BoardResearchDelivery), "owner": "anonymous",
	})
	if !unauthenticated.IsError || mcpErrorText(unauthenticated) != "unauthenticated" {
		t.Fatalf("unauthenticated MCP create result = %#v text=%q, want stable unauthenticated", unauthenticated, mcpErrorText(unauthenticated))
	}
}

func TestMCPRetainsOperationsLifecycleAndSaveViewExtension(t *testing.T) {
	fixture := newExecutionFixture(t)
	admin := fixture.principal(t, "mcp-grpc-admin-key")
	projectResult := callMCP(t, fixture.operations, admin, "delivery.create_project", map[string]any{"name": "MCP lifecycle", "board": string(delivery.BoardResearchDelivery), "owner": "delivery-owner"})
	if projectResult.IsError {
		t.Fatalf("create project through MCP = %#v", projectResult)
	}
	var project struct {
		Project delivery.Project `json:"project"`
	}
	decodeStructured(t, projectResult, &project)
	itemResult := callMCP(t, fixture.operations, admin, "delivery.create_work_item", map[string]any{"title": "MCP lifecycle item", "board": string(delivery.BoardResearchDelivery), "owner": "delivery-owner", "projectId": project.Project.ID, "kind": string(delivery.WorkItemKindTask)})
	if itemResult.IsError {
		t.Fatalf("create work item through MCP = %#v", itemResult)
	}
	var item struct {
		Created delivery.WorkItem `json:"created"`
	}
	decodeStructured(t, itemResult, &item)
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{name: "delivery.update_work_item", args: map[string]any{"id": item.Created.ID, "expectedRevision": item.Created.Revision, "progressPercent": 30}},
		{name: "delivery.add_comment", args: map[string]any{"id": item.Created.ID, "expectedRevision": item.Created.Revision + 1, "body": "MCP executor-backed comment"}},
		{name: "delivery.create_release", args: map[string]any{"projectId": project.Project.ID, "name": "R1", "version": "1.0.0", "status": "planned", "targetDate": "2026-09-10", "description": "MCP lifecycle release"}},
		{name: "delivery.save_view", args: map[string]any{"name": "MCP lifecycle view", "filter": map[string]any{"projectId": project.Project.ID}}},
	} {
		result := callMCP(t, fixture.operations, admin, call.name, call.args)
		if result.IsError {
			t.Fatalf("%s through MCP = %#v: %s", call.name, result, mcpErrorText(result))
		}
	}
}

func TestMCPAndRPCSourceRequireSingleGeneratedExecutionBoundary(t *testing.T) {
	mcpSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read MCP server source: %v", err)
	}
	for _, forbidden := range []string{"type Lifecycle interface", "*delivery.Service", "repository."} {
		if strings.Contains(string(mcpSource), forbidden) {
			t.Errorf("MCP production source retains forbidden bypass %q", forbidden)
		}
	}
	if !strings.Contains(string(mcpSource), "func New(operations *application.Operations") {
		t.Errorf("MCP server constructor does not require *application.Operations")
	}
	entries, err := os.ReadDir(filepath.Join("..", "rpcapi"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read handwritten rpcapi directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("handwritten rpcapi bypass still exists: %v", entries)
	}
	assemblySource, err := os.ReadFile(filepath.Join("..", "assembly", "zz_yunka_assembly_gen.go"))
	if err != nil {
		t.Fatalf("read generated assembly: %v", err)
	}
	for _, required := range []string{"deliveryrpc.RegisterOperationExecutor", "internal/delivery/transport/rpc"} {
		if !strings.Contains(string(assemblySource), required) {
			t.Errorf("generated assembly is missing %q", required)
		}
	}
	if strings.Contains(string(assemblySource), "rpcapi") {
		t.Errorf("generated assembly references handwritten rpcapi")
	}
}

func assertEquivalentItem(t *testing.T, grpcItem *deliveryv1.WorkItem, mcpItem delivery.WorkItem) {
	t.Helper()
	if grpcItem == nil {
		t.Fatal("generated gRPC item is nil")
	}
	if grpcItem.GetTitle() != mcpItem.Title || grpcItem.GetBoard() != string(mcpItem.Board) || grpcItem.GetOwner() != mcpItem.Owner || grpcItem.GetKind() != string(mcpItem.Kind) || grpcItem.GetPriority() != string(mcpItem.Priority) || grpcItem.GetStatus() != string(mcpItem.Status) || grpcItem.GetGate() != string(mcpItem.Gate) {
		t.Fatalf("transport items differ: gRPC=%#v MCP=%#v", grpcItem, mcpItem)
	}
	if len(grpcItem.GetEvidence()) != len(mcpItem.Evidence) || len(grpcItem.GetActivities()) != len(mcpItem.Activities) {
		t.Fatalf("transport item evidence/activity counts differ: gRPC=%#v MCP=%#v", grpcItem, mcpItem)
	}
	for index, evidence := range grpcItem.GetEvidence() {
		if evidence.GetKind() != mcpItem.Evidence[index].Kind || evidence.GetTitle() != mcpItem.Evidence[index].Title || evidence.GetReference() != mcpItem.Evidence[index].Reference {
			t.Fatalf("transport evidence %d differs: gRPC=%#v MCP=%#v", index, evidence, mcpItem.Evidence[index])
		}
	}
	for index, activity := range grpcItem.GetActivities() {
		if activity.GetType() != mcpItem.Activities[index].Type || activity.GetSummary() != mcpItem.Activities[index].Summary {
			t.Fatalf("transport activity %d differs: gRPC=%#v MCP=%#v", index, activity, mcpItem.Activities[index])
		}
	}
}

func sameItem(left, right delivery.WorkItem) bool {
	left.CreatedAt, right.CreatedAt = left.CreatedAt.UTC(), right.CreatedAt.UTC()
	left.UpdatedAt, right.UpdatedAt = left.UpdatedAt.UTC(), right.UpdatedAt.UTC()
	return reflect.DeepEqual(left, right)
}

func assertStoredItemCount(t *testing.T, repository *delivery.SQLiteRepository, want int) {
	t.Helper()
	items, err := repository.List(t.Context())
	if err != nil || len(items) != want {
		t.Fatalf("SQLite item count = %d, %v; want %d", len(items), err, want)
	}
}

func assertStoredGate(t *testing.T, repository *delivery.SQLiteRepository, itemID string, want delivery.Gate) {
	t.Helper()
	item, err := repository.Get(t.Context(), itemID)
	if err != nil || item.Gate != want {
		t.Fatalf("SQLite item %q gate = %q, %v; want %q", itemID, item.Gate, err, want)
	}
}
