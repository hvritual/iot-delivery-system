package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	deliveryrpc "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/transport/rpc"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/mcpserver"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

type authorizationMatrixFixture struct {
	repository  *delivery.SQLiteRepository
	outbox      *localoutbox.SQLiteStore
	operations  *deliveryapplication.Operations
	application deliveryapplication.DeliveryService
	executor    operation.Executor
	handler     http.Handler
}

const (
	revisionRaceWinner = "winner"
	revisionRaceLoser  = "loser"
)

// controlledRevisionExecutor is test-only. It admits real transport calls at
// the operation boundary before delegating to the production executor.
type controlledRevisionExecutor struct {
	delegate          operation.Executor
	winnerEntered     chan struct{}
	loserEntered      chan struct{}
	releaseWinner     chan struct{}
	releaseLoser      chan struct{}
	winnerOnce        sync.Once
	loserOnce         sync.Once
	releaseWinnerOnce sync.Once
	releaseLoserOnce  sync.Once
}

func newControlledRevisionExecutor(delegate operation.Executor) *controlledRevisionExecutor {
	return &controlledRevisionExecutor{
		delegate: delegate, winnerEntered: make(chan struct{}), loserEntered: make(chan struct{}),
		releaseWinner: make(chan struct{}), releaseLoser: make(chan struct{}),
	}
}

func (executor *controlledRevisionExecutor) Execute(ctx context.Context, plan operationplan.Plan, input any, invoke operation.Invoker) (any, error) {
	if plan.OperationID != policy.OperationPlanUpdateItem().OperationID {
		return executor.delegate.Execute(ctx, plan, input, invoke)
	}
	principal, _ := identity.FromContext(ctx)
	if strings.HasSuffix(principal.Subject, "/"+revisionRaceWinner) {
		executor.winnerOnce.Do(func() { close(executor.winnerEntered) })
		select {
		case <-executor.releaseWinner:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else if strings.HasSuffix(principal.Subject, "/"+revisionRaceLoser) {
		executor.loserOnce.Do(func() { close(executor.loserEntered) })
		select {
		case <-executor.releaseLoser:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return executor.delegate.Execute(ctx, plan, input, invoke)
}

func (executor *controlledRevisionExecutor) releaseAll() {
	executor.releaseWinnerOnce.Do(func() { close(executor.releaseWinner) })
	executor.releaseLoserOnce.Do(func() { close(executor.releaseLoser) })
}

func newAuthorizationMatrixFixture(t *testing.T) *authorizationMatrixFixture {
	return newAuthorizationMatrixFixtureWithExecutor(t, nil)
}

func newAuthorizationMatrixFixtureWithExecutor(t *testing.T, decorate func(operation.Executor) operation.Executor) *authorizationMatrixFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "authorization-matrix.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply identity and authorization migrations: %v", err)
	}
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A')`,
		`INSERT INTO users (id, organization_id, display_name) VALUES ('admin', 'org-a', 'Admin'), ('reviewer', 'org-a', 'Reviewer'), ('viewer', 'org-a', 'Viewer'), ('scoped', 'org-a', 'Scoped'), ('unbound', 'org-a', 'Unbound')`,
		`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-admin', 'org-a', 'Administrators', 'organization', 'org-a'), ('team-reviewer', 'org-a', 'Reviewers', 'organization', 'org-a')`,
		`INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-admin', 'org-a', 'admin'), ('team-reviewer', 'org-a', 'reviewer')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id) VALUES ('binding-admin', 'org-a', 'system-administrator', 'organization', 'org-a', 'team-admin'), ('binding-reviewer', 'org-a', 'system-administrator', 'organization', 'org-a', 'team-reviewer')`,
	} {
		if _, err := repository.Database().Exec(statement); err != nil {
			t.Fatalf("seed production authorization fixture with %q: %v", statement, err)
		}
	}
	authorizer, guards, err := configuredAuthorization(t.Context(), Config{RuntimeEnvironment: RuntimeEnvironmentProduction}, repository)
	if err != nil {
		t.Fatalf("configure production authorization: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guards)
	if err != nil {
		t.Fatalf("create production execution security: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite audit store: %v", err)
	}
	auditRecorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		t.Fatalf("open production security audit recorder: %v", err)
	}
	application, err := deliveryapplication.NewAuditedDeliveryService(deliveryapplication.NewAdapter(service), auditStore, deliveryapplication.WithWorkItemResolver(service.Get))
	if err != nil {
		t.Fatalf("assemble production audited delivery application: %v", err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}), auditRecorder)
	if err != nil {
		t.Fatalf("assemble production recording executor: %v", err)
	}
	// Do not attach legacy service extensions: this matrix must exercise the
	// registered OperationPlans rather than their unregistered compatibility
	// helpers.
	var operationExecutor operation.Executor = executor
	if decorate != nil {
		operationExecutor = decorate(operationExecutor)
	}
	operations := deliveryapplication.NewOperations(application, operationExecutor)
	return &authorizationMatrixFixture{repository: repository, outbox: outbox, operations: operations, application: application, executor: operationExecutor, handler: httpapi.NewHandler(operations)}
}

func (fixture *authorizationMatrixFixture) revision(t *testing.T, id string) int64 {
	t.Helper()
	item, err := fixture.repository.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return item.Revision
}

func (fixture *authorizationMatrixFixture) grpcGate(t *testing.T, principal identity.Principal, itemID string, gate delivery.Gate) codes.Code {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register matrix gRPC operation executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///authorization-matrix", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial matrix gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	expectedRevision := int64(1)
	if item, getErr := fixture.repository.Get(t.Context(), itemID); getErr == nil {
		expectedRevision = item.Revision
	}
	_, err = deliveryv1.NewDeliveryServiceClient(connection).AdvanceGate(t.Context(), &deliveryv1.AdvanceGateRequest{Id: itemID, ExpectedRevision: expectedRevision, Gate: string(gate), Evidence: []*deliveryv1.Evidence{{Kind: "review", Title: "authorization matrix"}}})
	return status.Code(err)
}

func (fixture *authorizationMatrixFixture) grpcCreate(t *testing.T, principal identity.Principal, projectID string) codes.Code {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register service gRPC executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///authorization-service", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial service gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_, err = deliveryv1.NewDeliveryServiceClient(connection).CreateItem(t.Context(), &deliveryv1.CreateItemRequest{Title: "service-created", Board: string(delivery.BoardResearchDelivery), Owner: "service", ProjectId: projectID, Kind: string(delivery.WorkItemKindTask)})
	return status.Code(err)
}

func (fixture *authorizationMatrixFixture) grpcUpdate(t *testing.T, principal identity.Principal, itemID string, expectedRevision int64, progress int32) (codes.Code, string) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register revision matrix gRPC executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///revision-matrix", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial revision matrix gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_, err = deliveryv1.NewDeliveryServiceClient(connection).UpdateItem(t.Context(), &deliveryv1.UpdateItemRequest{Id: itemID, ExpectedRevision: expectedRevision, UpdateMask: []string{"progress_percent"}, ProgressPercent: progress})
	result := status.Convert(err)
	return result.Code(), result.Message()
}

func matrixPrincipal(userID string) identity.Principal {
	if userID == "" {
		return identity.Principal{}
	}
	return identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: userID, Subject: "oidc-bff/" + userID}
}

func (fixture *authorizationMatrixFixture) createProtectedItem(t *testing.T) (delivery.Project, delivery.WorkItem) {
	t.Helper()
	admin := identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
	project, err := fixture.operations.CreateProject(admin, delivery.ProjectInput{Name: "Authorization Matrix", Board: delivery.BoardResearchDelivery, Owner: "admin"})
	if err != nil {
		t.Fatalf("create protected project: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-viewer', 'org-a', 'Viewers', 'project', ?)`, project.ID); err != nil {
		t.Fatalf("create viewer team: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-viewer', 'org-a', 'viewer')`); err != nil {
		t.Fatalf("add viewer membership: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id) VALUES ('binding-viewer', 'org-a', 'viewer', 'project', ?, 'team-viewer')`, project.ID); err != nil {
		t.Fatalf("bind viewer to protected project: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO teams (id, organization_id, name, scope_type, scope_id) VALUES ('team-scoped', 'org-a', 'Scoped administrators', 'project', ?)`, project.ID); err != nil {
		t.Fatalf("create scoped project team: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO team_memberships (team_id, organization_id, user_id) VALUES ('team-scoped', 'org-a', 'scoped')`); err != nil {
		t.Fatalf("add scoped membership: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, team_id) VALUES ('binding-scoped', 'org-a', 'project-administrator', 'project', ?, 'team-scoped')`, project.ID); err != nil {
		t.Fatalf("bind scoped project administrator: %v", err)
	}
	item, err := fixture.operations.Create(admin, delivery.CreateInput{Title: "Protected item", Board: delivery.BoardResearchDelivery, Owner: "admin", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
	if err != nil {
		t.Fatalf("create protected item: %v", err)
	}
	return project, item
}

func (fixture *authorizationMatrixFixture) restGate(t *testing.T, principal identity.Principal, itemID string, gate delivery.Gate) (int, string) {
	t.Helper()
	expectedRevision := int64(1)
	if item, err := fixture.repository.Get(t.Context(), itemID); err == nil {
		expectedRevision = item.Revision
	}
	body, err := json.Marshal(map[string]any{"expectedRevision": expectedRevision, "evidence": []map[string]string{{"kind": "review", "title": "authorization matrix"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/items/"+itemID+"/gates/"+string(gate), bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(identity.WithPrincipal(request.Context(), principal))
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	var payload map[string]any
	_ = json.NewDecoder(recorder.Body).Decode(&payload)
	category, _ := payload["error"].(string)
	return recorder.Code, category
}

func (fixture *authorizationMatrixFixture) restGateAtRevision(t *testing.T, principal identity.Principal, itemID string, expectedRevision int64, gate delivery.Gate) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"expectedRevision": expectedRevision, "evidence": []map[string]string{{"kind": "review", "title": "revision matrix"}}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/items/"+itemID+"/gates/"+string(gate), bytes.NewReader(body)).WithContext(identity.WithPrincipal(t.Context(), principal))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	var payload map[string]any
	_ = json.NewDecoder(recorder.Body).Decode(&payload)
	category, _ := payload["error"].(string)
	return recorder.Code, category
}

func (fixture *authorizationMatrixFixture) restCombinedPatch(t *testing.T, principal identity.Principal, itemID string, expectedRevision int64) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"expectedRevision": expectedRevision, "progressPercent": 47, "plan": "production atomic context"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/items/"+itemID, bytes.NewReader(body)).WithContext(identity.WithPrincipal(t.Context(), principal))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	var payload map[string]any
	_ = json.NewDecoder(recorder.Body).Decode(&payload)
	category, _ := payload["error"].(string)
	return recorder.Code, category
}

func (fixture *authorizationMatrixFixture) grpcGateAtRevision(t *testing.T, principal identity.Principal, itemID string, expectedRevision int64, gate delivery.Gate) (codes.Code, string) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register revision matrix gRPC gate executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///revision-gate-matrix", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial revision matrix gRPC gate server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	_, err = deliveryv1.NewDeliveryServiceClient(connection).AdvanceGate(t.Context(), &deliveryv1.AdvanceGateRequest{Id: itemID, ExpectedRevision: expectedRevision, Gate: string(gate), Evidence: []*deliveryv1.Evidence{{Kind: "review", Title: "revision matrix"}}})
	result := status.Convert(err)
	return result.Code(), result.Message()
}

func callMatrixMCP(t *testing.T, operations *deliveryapplication.Operations, principal identity.Principal, itemID string, expectedRevision int64, gate delivery.Gate) *mcp.CallToolResult {
	return callMatrixMCPContext(t, t.Context(), operations, principal, "delivery.advance_gate", map[string]any{"id": itemID, "expectedRevision": expectedRevision, "gate": string(gate), "evidence": []map[string]any{{"kind": "review", "title": "authorization matrix"}}})
}

func callMatrixMCPContext(t *testing.T, ctx context.Context, operations *deliveryapplication.Operations, principal identity.Principal, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	server := mcpserver.New(operations, principal)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect matrix MCP server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "authorization-matrix", Version: "v1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect matrix MCP client: %v", err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call matrix MCP tool %s: %v", name, err)
	}
	return result
}

func matrixMCPError(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) != 1 {
		return ""
	}
	if text, ok := result.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

func TestProductionAuthorizationMatrixUsesStableGRPCRevisionErrors(t *testing.T) {
	fixture := newAuthorizationMatrixFixture(t)
	_, item := fixture.createProtectedItem(t)
	admin := matrixPrincipal("admin")
	if code, message := fixture.grpcUpdate(t, admin, item.ID, item.Revision, 25); code != codes.OK || message != "" {
		t.Fatalf("prepare gRPC revision conflict = code %s message %q, want OK", code, message)
	}
	for _, scenario := range []struct {
		name             string
		expectedRevision int64
		wantCode         codes.Code
		wantMessage      string
	}{
		{name: "missing revision", wantCode: codes.InvalidArgument, wantMessage: "invalid_expected_revision"},
		{name: "stale revision", expectedRevision: item.Revision, wantCode: codes.Aborted, wantMessage: "revision_conflict"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			code, message := fixture.grpcUpdate(t, admin, item.ID, scenario.expectedRevision, 50)
			if code != scenario.wantCode || message != scenario.wantMessage {
				t.Fatalf("gRPC %s = code %s message %q, want code %s message %q", scenario.name, code, message, scenario.wantCode, scenario.wantMessage)
			}
		})
	}
}

func TestProductionAuthorizationCombinedRESTPatchRequiresBothRegisteredPermissions(t *testing.T) {
	fixture := newAuthorizationMatrixFixture(t)
	_, item := fixture.createProtectedItem(t)
	if status, category := fixture.restCombinedPatch(t, matrixPrincipal("admin"), item.ID, item.Revision); status != http.StatusOK || category != "" {
		t.Fatalf("production combined REST patch = status %d category %q, want 200", status, category)
	}
	updated, err := fixture.repository.Get(t.Context(), item.ID)
	if err != nil || updated.ProgressPercent != 47 || updated.Plan != "production atomic context" || updated.Revision != item.Revision+2 {
		t.Fatalf("production combined REST patch stored item = %#v err=%v", updated, err)
	}
	beforeConflictOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot production combined conflict Outbox: %v", err)
	}
	if status, category := fixture.restCombinedPatch(t, matrixPrincipal("admin"), item.ID, item.Revision); status != http.StatusConflict || category != "revision_conflict" {
		t.Fatalf("stale production combined REST patch = status %d category %q, want 409/revision_conflict", status, category)
	}
	afterConflict, itemErr := fixture.repository.Get(t.Context(), item.ID)
	afterConflictOutbox, outboxErr := fixture.outbox.Snapshot(t.Context())
	if itemErr != nil || !reflect.DeepEqual(afterConflict, updated) || outboxErr != nil || !reflect.DeepEqual(afterConflictOutbox, beforeConflictOutbox) {
		t.Fatalf("stale production combined patch left partial state: item=%#v itemErr=%v outbox=%#v outboxErr=%v", afterConflict, itemErr, afterConflictOutbox, outboxErr)
	}

	deniedFixture := newAuthorizationMatrixFixture(t)
	_, deniedItem := deniedFixture.createProtectedItem(t)
	beforeItem, err := deniedFixture.repository.Get(t.Context(), deniedItem.ID)
	if err != nil {
		t.Fatalf("read denied combined patch item: %v", err)
	}
	beforeOutbox, err := deniedFixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot denied combined patch Outbox: %v", err)
	}
	if status, category := deniedFixture.restCombinedPatch(t, matrixPrincipal("viewer"), deniedItem.ID, deniedItem.Revision); status != http.StatusForbidden || category != "permission_denied" {
		t.Fatalf("denied combined REST patch = status %d category %q, want 403/permission_denied", status, category)
	}
	afterItem, itemErr := deniedFixture.repository.Get(t.Context(), deniedItem.ID)
	afterOutbox, outboxErr := deniedFixture.outbox.Snapshot(t.Context())
	if itemErr != nil || !reflect.DeepEqual(afterItem, beforeItem) || outboxErr != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
		t.Fatalf("denied combined REST patch changed SQLite or Outbox: item=%#v itemErr=%v outbox=%#v outboxErr=%v", afterItem, itemErr, afterOutbox, outboxErr)
	}
}

type revisionRaceResult struct {
	success bool
	detail  string
}

func newRevisionRaceGRPCClient(t *testing.T, fixture *authorizationMatrixFixture, principal identity.Principal) deliveryv1.DeliveryServiceClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register revision race gRPC executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///revision-race", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial revision race gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return deliveryv1.NewDeliveryServiceClient(connection)
}

func newRevisionRaceMCPCall(t *testing.T, operations *deliveryapplication.Operations, principal identity.Principal) func(context.Context, map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	server := mcpserver.New(operations, principal)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect revision race MCP server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "revision-race", Version: "v1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect revision race MCP client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return func(ctx context.Context, arguments map[string]any) (*mcp.CallToolResult, error) {
		return clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "delivery.update_work_item", Arguments: arguments})
	}
}

func TestProductionRevisionConflictMatrixPreservesExactlyOneCrossTransportWrite(t *testing.T) {
	for _, scenario := range []struct{ name, winner, loser string }{
		{name: "REST wins over gRPC", winner: "REST", loser: "gRPC"},
		{name: "REST wins over MCP", winner: "REST", loser: "MCP"},
		{name: "gRPC wins over REST", winner: "gRPC", loser: "REST"},
		{name: "gRPC wins over MCP", winner: "gRPC", loser: "MCP"},
		{name: "MCP wins over REST", winner: "MCP", loser: "REST"},
		{name: "MCP wins over gRPC", winner: "MCP", loser: "gRPC"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var control *controlledRevisionExecutor
			fixture := newAuthorizationMatrixFixtureWithExecutor(t, func(delegate operation.Executor) operation.Executor {
				control = newControlledRevisionExecutor(delegate)
				return control
			})
			t.Cleanup(control.releaseAll)
			_, item := fixture.createProtectedItem(t)
			beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("snapshot outbox before revision contest: %v", err)
			}
			var beforeSuccessAudit int
			if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE result = 'success'`).Scan(&beforeSuccessAudit); err != nil {
				t.Fatalf("count successful audit entries before revision contest: %v", err)
			}

			principal := matrixPrincipal("admin")
			winnerPrincipal, loserPrincipal := principal, principal
			winnerPrincipal.Subject += "/" + revisionRaceWinner
			loserPrincipal.Subject += "/" + revisionRaceLoser
			winnerGRPC := newRevisionRaceGRPCClient(t, fixture, winnerPrincipal)
			loserGRPC := newRevisionRaceGRPCClient(t, fixture, loserPrincipal)
			winnerMCP := newRevisionRaceMCPCall(t, fixture.operations, winnerPrincipal)
			loserMCP := newRevisionRaceMCPCall(t, fixture.operations, loserPrincipal)

			call := func(ctx context.Context, transport string, principal identity.Principal) revisionRaceResult {
				title := transport + " payload"
				progress := int32(map[string]int{"REST": 31, "gRPC": 47, "MCP": 63}[transport])
				switch transport {
				case "REST":
					body, marshalErr := json.Marshal(map[string]any{"expectedRevision": item.Revision, "title": title, "progressPercent": progress})
					if marshalErr != nil {
						return revisionRaceResult{detail: marshalErr.Error()}
					}
					request := httptest.NewRequest(http.MethodPatch, "/api/items/"+item.ID, bytes.NewReader(body)).WithContext(identity.WithPrincipal(ctx, principal))
					request.Header.Set("Content-Type", "application/json")
					recorder := httptest.NewRecorder()
					fixture.handler.ServeHTTP(recorder, request)
					var payload map[string]any
					_ = json.NewDecoder(recorder.Body).Decode(&payload)
					category, _ := payload["error"].(string)
					return revisionRaceResult{success: recorder.Code == http.StatusOK, detail: category}
				case "gRPC":
					client := loserGRPC
					if strings.HasSuffix(principal.Subject, "/"+revisionRaceWinner) {
						client = winnerGRPC
					}
					_, callErr := client.UpdateItem(ctx, &deliveryv1.UpdateItemRequest{Id: item.ID, ExpectedRevision: item.Revision, UpdateMask: []string{"title", "progress_percent"}, Title: title, ProgressPercent: progress})
					converted := status.Convert(callErr)
					return revisionRaceResult{success: converted.Code() == codes.OK, detail: converted.Message()}
				case "MCP":
					caller := loserMCP
					if strings.HasSuffix(principal.Subject, "/"+revisionRaceWinner) {
						caller = winnerMCP
					}
					result, callErr := caller(ctx, map[string]any{"id": item.ID, "expectedRevision": item.Revision, "title": title, "progressPercent": progress})
					if callErr != nil {
						return revisionRaceResult{detail: callErr.Error()}
					}
					if result.IsError {
						return revisionRaceResult{detail: matrixMCPError(result)}
					}
					return revisionRaceResult{success: true}
				default:
					return revisionRaceResult{detail: "unknown transport"}
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			winnerResult, loserResult := make(chan revisionRaceResult, 1), make(chan revisionRaceResult, 1)
			go func() { winnerResult <- call(ctx, scenario.winner, winnerPrincipal) }()
			go func() { loserResult <- call(ctx, scenario.loser, loserPrincipal) }()
			for _, entered := range []<-chan struct{}{control.winnerEntered, control.loserEntered} {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatalf("revision race calls did not overlap at operation boundary: %v", ctx.Err())
				}
			}
			control.releaseWinnerOnce.Do(func() { close(control.releaseWinner) })
			var winner revisionRaceResult
			select {
			case winner = <-winnerResult:
			case <-ctx.Done():
				t.Fatalf("winner did not complete: %v", ctx.Err())
			}
			control.releaseLoserOnce.Do(func() { close(control.releaseLoser) })
			var loser revisionRaceResult
			select {
			case loser = <-loserResult:
			case <-ctx.Done():
				t.Fatalf("loser did not complete: %v", ctx.Err())
			}
			if !winner.success || winner.detail != "" {
				t.Fatalf("%s winner result = success=%t detail=%q", scenario.winner, winner.success, winner.detail)
			}
			if loser.success || loser.detail != "revision_conflict" {
				t.Fatalf("%s loser result = success=%t detail=%q, want revision_conflict", scenario.loser, loser.success, loser.detail)
			}

			wantProgress := map[string]int{"REST": 31, "gRPC": 47, "MCP": 63}[scenario.winner]
			afterItem, err := fixture.repository.Get(t.Context(), item.ID)
			if err != nil || afterItem.Revision != item.Revision+1 || afterItem.Title != scenario.winner+" payload" || afterItem.ProgressPercent != wantProgress {
				t.Fatalf("revision contest item = %#v err=%v; want winner payload only", afterItem, err)
			}
			afterOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil || afterOutbox.Pending != beforeOutbox.Pending+1 {
				t.Fatalf("revision contest Outbox = before=%#v after=%#v err=%v; want one entry", beforeOutbox, afterOutbox, err)
			}
			var afterSuccessAudit int
			if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE result = 'success'`).Scan(&afterSuccessAudit); err != nil || afterSuccessAudit != beforeSuccessAudit+1 {
				t.Fatalf("revision contest successful audit = before=%d after=%d err=%v; want one entry", beforeSuccessAudit, afterSuccessAudit, err)
			}
		})
	}
}

func TestMCPRegistrationContainsExactlyTenDictionaryPublicToolsAndSevenExcludedExtensions(t *testing.T) {
	server := mcpserver.New(nil, identity.Principal{})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP registration server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "authorization-registration", Version: "v1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect MCP registration client: %v", err)
	}
	defer clientSession.Close()
	result, err := clientSession.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	public := map[string]bool{
		"delivery.list_work_items": true, "delivery.create_work_item": true, "delivery.update_work_item": true, "delivery.add_comment": true, "delivery.advance_gate": true,
		"delivery.close_work_item": true, "delivery.create_project": true, "delivery.create_release": true, "delivery.create_sprint": true, "delivery.create_milestone": true,
	}
	excluded := map[string]bool{
		"delivery.list_projects": true, "delivery.find_similar": true, "delivery.get_member_week": true, "delivery.get_project_progress": true,
		"delivery.get_project_schedule": true, "delivery.save_view": true, "delivery.list_saved_views": true,
	}
	seen := map[string]bool{}
	for _, tool := range result.Tools {
		seen[tool.Name] = true
		if !public[tool.Name] && !excluded[tool.Name] {
			t.Fatalf("MCP registered unclassified tool %q", tool.Name)
		}
	}
	for name := range public {
		if !seen[name] {
			t.Fatalf("MCP public dictionary tool %q is not registered", name)
		}
	}
	for name := range excluded {
		if !seen[name] {
			t.Fatalf("MCP excluded extension tool %q is not registered", name)
		}
	}
	if len(result.Tools) != len(public)+len(excluded) {
		t.Fatalf("MCP tool registrations = %d, want %d public plus excluded", len(result.Tools), len(public)+len(excluded))
	}
}

func TestProductionAuthorizationMatrixRejectsRESTAndMCPWithoutSideEffects(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		principal    identity.Principal
		wantHTTP     int
		wantCategory string
		gate         delivery.Gate
		prepare      func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) string
	}{
		{name: "unauthenticated", wantHTTP: http.StatusUnauthorized, wantCategory: "unauthenticated", gate: delivery.GateSolutionReviewed},
		{name: "no role binding", principal: matrixPrincipal("unbound"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateSolutionReviewed},
		{name: "viewer write", principal: matrixPrincipal("viewer"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateSolutionReviewed},
		{name: "cross project scope", principal: matrixPrincipal("scoped"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateSolutionReviewed, prepare: func(t *testing.T, fixture *authorizationMatrixFixture, _ delivery.WorkItem) string {
			t.Helper()
			admin := identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
			project, err := fixture.operations.CreateProject(admin, delivery.ProjectInput{Name: "Other project", Board: delivery.BoardResearchDelivery, Owner: "admin"})
			if err != nil {
				t.Fatalf("create cross-project target: %v", err)
			}
			item, err := fixture.operations.Create(admin, delivery.CreateInput{Title: "Other item", Board: delivery.BoardResearchDelivery, Owner: "admin", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
			if err != nil {
				t.Fatalf("create cross-project item: %v", err)
			}
			return item.ID
		}},
		{name: "missing object is not disclosed", principal: matrixPrincipal("scoped"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateSolutionReviewed, prepare: func(_ *testing.T, _ *authorizationMatrixFixture, _ delivery.WorkItem) string { return "missing-object" }},
		{name: "role binding revocation is immediate", principal: matrixPrincipal("admin"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateSolutionReviewed, prepare: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) string {
			t.Helper()
			if _, err := fixture.repository.Database().Exec(`DELETE FROM role_bindings WHERE id = 'binding-admin'`); err != nil {
				t.Fatalf("revoke administrator role binding: %v", err)
			}
			return item.ID
		}},
		{name: "implementer production validation", principal: matrixPrincipal("admin"), wantHTTP: http.StatusForbidden, wantCategory: "permission_denied", gate: delivery.GateProductionValidated, prepare: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) string {
			t.Helper()
			admin := identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
			for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
				if _, err := fixture.operations.AdvanceGate(admin, item.ID, fixture.revision(t, item.ID), gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}}); err != nil {
					t.Fatalf("prepare self-production-validation gate %s: %v", gate, err)
				}
			}
			return item.ID
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newAuthorizationMatrixFixture(t)
			_, item := fixture.createProtectedItem(t)
			itemID := item.ID
			if scenario.prepare != nil {
				itemID = scenario.prepare(t, fixture, item)
			}
			trackedID := item.ID
			if itemID != "missing-object" {
				trackedID = itemID
			}
			beforeItem, err := fixture.repository.Get(t.Context(), trackedID)
			if err != nil {
				t.Fatalf("read protected item before denial: %v", err)
			}
			beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("snapshot outbox before denial: %v", err)
			}
			if status, category := fixture.restGate(t, scenario.principal, itemID, scenario.gate); status != scenario.wantHTTP || category != scenario.wantCategory {
				t.Fatalf("REST advance gate = status %d category %q, want status %d category %q", status, category, scenario.wantHTTP, scenario.wantCategory)
			}
			wantGRPC := codes.PermissionDenied
			if scenario.wantHTTP == http.StatusUnauthorized {
				wantGRPC = codes.Unauthenticated
			}
			if got := fixture.grpcGate(t, scenario.principal, itemID, scenario.gate); got != wantGRPC {
				t.Fatalf("gRPC advance gate = %s, want %s", got, wantGRPC)
			}
			mcpResult := callMatrixMCP(t, fixture.operations, scenario.principal, itemID, beforeItem.Revision, scenario.gate)
			if !mcpResult.IsError || matrixMCPError(mcpResult) != scenario.wantCategory {
				t.Fatalf("MCP advance gate = %#v text=%q, want stable %q", mcpResult, matrixMCPError(mcpResult), scenario.wantCategory)
			}
			afterItem, itemErr := fixture.repository.Get(t.Context(), trackedID)
			afterOutbox, outboxErr := fixture.outbox.Snapshot(t.Context())
			if itemErr != nil || !reflect.DeepEqual(afterItem, beforeItem) || outboxErr != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
				t.Fatalf("%s denial changed SQLite or Outbox: item=%#v itemErr=%v outbox=%#v outboxErr=%v", scenario.name, afterItem, itemErr, afterOutbox, outboxErr)
			}
		})
	}
}

func TestProductionAuthorizationMatrixAllowsRegisteredOperationClasses(t *testing.T) {
	fixture := newAuthorizationMatrixFixture(t)
	admin := identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
	if _, err := fixture.operations.List(admin); err != nil {
		t.Fatalf("allow read delivery.items.list: %v", err)
	}
	project, err := fixture.operations.CreateProject(admin, delivery.ProjectInput{Name: "Allowed project", Board: delivery.BoardResearchDelivery, Owner: "admin"})
	if err != nil {
		t.Fatalf("allow organization project create: %v", err)
	}
	item, err := fixture.operations.Create(admin, delivery.CreateInput{Title: "Allowed item", Board: delivery.BoardResearchDelivery, Owner: "admin", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
	if err != nil {
		t.Fatalf("allow project planning write: %v", err)
	}
	title := "Allowed item updated"
	if _, err := fixture.operations.UpdateWorkItem(admin, item.ID, fixture.revision(t, item.ID), delivery.WorkItemUpdate{Title: &title}); err != nil {
		t.Fatalf("allow object update: %v", err)
	}
	if _, err := fixture.operations.CreateRelease(admin, delivery.ReleaseInput{ProjectID: project.ID, Name: "R1", Version: "1.0.0"}); err != nil {
		t.Fatalf("allow project release write: %v", err)
	}
	if _, err := fixture.operations.AdvanceGate(admin, item.ID, fixture.revision(t, item.ID), delivery.GateSolutionReviewed, []delivery.Evidence{{Kind: "review", Title: "approved"}}); err != nil {
		t.Fatalf("allow high-risk gate advance: %v", err)
	}
	closeItem, err := fixture.operations.Create(admin, delivery.CreateInput{Title: "Closable item", Board: delivery.BoardResearchDelivery, Owner: "admin", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
	if err != nil {
		t.Fatalf("create closable item: %v", err)
	}
	for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
		if _, err := fixture.operations.AdvanceGate(admin, closeItem.ID, fixture.revision(t, closeItem.ID), gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}}); err != nil {
			t.Fatalf("prepare close gate %s: %v", gate, err)
		}
	}
	reviewer := identity.WithPrincipal(t.Context(), matrixPrincipal("reviewer"))
	if _, err := fixture.operations.AdvanceGate(reviewer, closeItem.ID, fixture.revision(t, closeItem.ID), delivery.GateProductionValidated, []delivery.Evidence{{Kind: "validation", Title: "independent"}}); err != nil {
		t.Fatalf("allow independent production validation: %v", err)
	}
	if _, err := fixture.operations.Close(reviewer, closeItem.ID, fixture.revision(t, closeItem.ID), "independent retrospective"); err != nil {
		t.Fatalf("allow high-risk close: %v", err)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending < 10 {
		t.Fatalf("allow operations did not commit expected SQLite/Outbox effects: snapshot=%#v err=%v", snapshot, err)
	}
}

func TestPostAuthTransportMatrixAllowsGateWithEquivalentSQLiteAndOutboxEffects(t *testing.T) {
	for _, transport := range []struct {
		name string
		call func(*testing.T, *authorizationMatrixFixture, delivery.WorkItem) codes.Code
	}{
		{name: "REST", call: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) codes.Code {
			status, category := fixture.restGate(t, matrixPrincipal("scoped"), item.ID, delivery.GateSolutionReviewed)
			if status != http.StatusOK || category != "" {
				t.Fatalf("REST gate allow = status %d category %q, want 200/no error", status, category)
			}
			return codes.OK
		}},
		{name: "gRPC", call: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) codes.Code {
			return fixture.grpcGate(t, matrixPrincipal("scoped"), item.ID, delivery.GateSolutionReviewed)
		}},
		{name: "MCP", call: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) codes.Code {
			result := callMatrixMCP(t, fixture.operations, matrixPrincipal("scoped"), item.ID, fixture.revision(t, item.ID), delivery.GateSolutionReviewed)
			if result.IsError {
				t.Fatalf("MCP gate allow = IsError true text=%q", matrixMCPError(result))
			}
			return codes.OK
		}},
	} {
		t.Run(transport.name, func(t *testing.T) {
			fixture := newAuthorizationMatrixFixture(t)
			_, item := fixture.createProtectedItem(t)
			before, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("snapshot before %s allow: %v", transport.name, err)
			}
			if got := transport.call(t, fixture, item); got != codes.OK {
				t.Fatalf("%s gate allow = %s, want OK", transport.name, got)
			}
			afterItem, err := fixture.repository.Get(t.Context(), item.ID)
			if err != nil || afterItem.Gate != delivery.GateSolutionReviewed {
				t.Fatalf("%s gate SQLite result = %#v err=%v, want solution_reviewed", transport.name, afterItem, err)
			}
			after, err := fixture.outbox.Snapshot(t.Context())
			if err != nil || after.Pending <= before.Pending {
				t.Fatalf("%s gate Outbox effect = before=%#v after=%#v err=%v", transport.name, before, after, err)
			}
		})
	}
}

func TestProductionServiceGrantResolverAllowsOnlyItsGrantedProjectOverGRPC(t *testing.T) {
	fixture := newAuthorizationMatrixFixture(t)
	admin := identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
	grantedProject, err := fixture.operations.CreateProject(admin, delivery.ProjectInput{Name: "Service granted", Board: delivery.BoardResearchDelivery, Owner: "admin"})
	if err != nil {
		t.Fatalf("create granted project: %v", err)
	}
	otherProject, err := fixture.operations.CreateProject(admin, delivery.ProjectInput{Name: "Service ungranted", Board: delivery.BoardResearchDelivery, Owner: "admin"})
	if err != nil {
		t.Fatalf("create ungranted project: %v", err)
	}
	if _, err := fixture.repository.Database().Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-matrix', 'org-a', 'Matrix service')`); err != nil {
		t.Fatalf("create service account: %v", err)
	}
	manager, err := serviceauthz.NewManager(fixture.repository.Database(), fixture.repository)
	if err != nil {
		t.Fatalf("create production service grant manager: %v", err)
	}
	if err := manager.Grant(t.Context(), serviceauthz.GrantInput{ID: "service-matrix-create", ServiceAccountID: "service-matrix", OperationID: "delivery.items.create", Permission: "delivery.work-items.create", ProjectID: grantedProject.ID}); err != nil {
		t.Fatalf("grant service project create: %v", err)
	}
	principal := identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/service-matrix"}
	beforeAllowed, err := fixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("snapshot before service allow: %v", err)
	}
	if got := fixture.grpcCreate(t, principal, grantedProject.ID); got != codes.OK {
		t.Fatalf("gRPC service granted create = %s, want OK", got)
	}
	afterAllowed, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || afterAllowed.Pending <= beforeAllowed.Pending {
		t.Fatalf("service grant allow did not commit Outbox: before=%#v after=%#v err=%v", beforeAllowed, afterAllowed, err)
	}
	beforeDenied := afterAllowed
	itemsBeforeDenied, err := fixture.repository.List(t.Context())
	if err != nil {
		t.Fatalf("list items before service scope denial: %v", err)
	}
	if got := fixture.grpcCreate(t, principal, otherProject.ID); got != codes.PermissionDenied {
		t.Fatalf("gRPC service wrong project scope = %s, want PermissionDenied", got)
	}
	afterDenied, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(afterDenied, beforeDenied) {
		t.Fatalf("service wrong project scope changed Outbox: before=%#v after=%#v err=%v", beforeDenied, afterDenied, err)
	}
	itemsAfterDenied, err := fixture.repository.List(t.Context())
	if err != nil || !reflect.DeepEqual(itemsAfterDenied, itemsBeforeDenied) {
		t.Fatalf("service wrong project scope created a work item: before=%#v after=%#v err=%v", itemsBeforeDenied, itemsAfterDenied, err)
	}
}
