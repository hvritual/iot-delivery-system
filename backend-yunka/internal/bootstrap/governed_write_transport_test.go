package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryrpc "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/transport/rpc"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func governedWriteGRPCClient(t *testing.T, fixture *authorizationMatrixFixture, principal identity.Principal) deliveryv1.DeliveryServiceClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(identity.WithPrincipal(ctx, principal), request)
	}))
	if err := deliveryrpc.RegisterOperationExecutor(server, fixture.application, fixture.executor); err != nil {
		t.Fatalf("register governed-write gRPC executor: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.DialContext(t.Context(), "passthrough:///governed-write-matrix", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial governed-write gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return deliveryv1.NewDeliveryServiceClient(connection)
}

func governedWriteREST(t *testing.T, fixture *authorizationMatrixFixture, principal identity.Principal, method, path string, payload any) (int, string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(identity.WithPrincipal(t.Context(), principal))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	var response map[string]any
	_ = json.NewDecoder(recorder.Body).Decode(&response)
	category, _ := response["error"].(string)
	return recorder.Code, category
}

func assertGovernedWriteUnchanged(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem, beforeOutbox any) {
	t.Helper()
	afterItem, err := fixture.repository.Get(t.Context(), item.ID)
	if err != nil || !reflect.DeepEqual(afterItem, item) {
		t.Fatalf("rejected governed write changed item: before=%#v after=%#v err=%v", item, afterItem, err)
	}
	afterOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
		t.Fatalf("rejected governed write changed Outbox: before=%#v after=%#v err=%v", beforeOutbox, afterOutbox, err)
	}
}

func TestGovernedCommentCASConflictIsStableAcrossTransports(t *testing.T) {
	for _, transport := range []string{"REST", "gRPC", "MCP"} {
		t.Run(transport, func(t *testing.T) {
			fixture := newAuthorizationMatrixFixture(t)
			_, item := fixture.createProtectedItem(t)
			principal := matrixPrincipal("scoped")
			seeded, err := fixture.operations.AddComment(identity.WithPrincipal(t.Context(), principal), item.ID, item.Revision, delivery.CommentInput{Body: "accepted"})
			if err != nil {
				t.Fatalf("seed accepted comment: %v", err)
			}
			item, err = fixture.repository.Get(t.Context(), item.ID)
			if err != nil || item.Revision != seeded.WorkItemRevision {
				t.Fatalf("read seeded comment revision: item=%#v comment=%#v err=%v", item, seeded, err)
			}
			beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			stale := item.Revision - 1
			switch transport {
			case "REST":
				code, category := governedWriteREST(t, fixture, principal, http.MethodPost, "/api/items/"+item.ID+"/comments", map[string]any{"expectedRevision": stale, "body": "stale"})
				if code != http.StatusConflict || category != "revision_conflict" {
					t.Fatalf("REST stale comment = %d/%q, want 409/revision_conflict", code, category)
				}
			case "gRPC":
				_, callErr := governedWriteGRPCClient(t, fixture, principal).CreateItemComment(t.Context(), &deliveryv1.CreateItemCommentRequest{Id: item.ID, ExpectedRevision: stale, Body: "stale"})
				if result := status.Convert(callErr); result.Code() != codes.Aborted || result.Message() != "revision_conflict" {
					t.Fatalf("gRPC stale comment = %s/%q, want Aborted/revision_conflict", result.Code(), result.Message())
				}
			case "MCP":
				result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, "delivery.add_comment", map[string]any{"id": item.ID, "expectedRevision": stale, "body": "stale"})
				if !result.IsError || matrixMCPError(result) != "revision_conflict" {
					t.Fatalf("MCP stale comment = %#v text=%q, want revision_conflict", result, matrixMCPError(result))
				}
			}
			assertGovernedWriteUnchanged(t, fixture, item, beforeOutbox)
		})
	}
}

func TestGovernedContextGateAndCloseCASConflictIsStableAcrossTransports(t *testing.T) {
	type scenario struct {
		name       string
		transports []string
		prepare    func(*testing.T, *authorizationMatrixFixture, delivery.WorkItem) (delivery.WorkItem, int64)
		restPath   func(string) string
		restBody   func(int64) any
		grpcCall   func(context.Context, deliveryv1.DeliveryServiceClient, string, int64) error
		mcpTool    string
		mcpArgs    func(string, int64) map[string]any
	}
	adminContext := func(t *testing.T) context.Context {
		return identity.WithPrincipal(t.Context(), matrixPrincipal("admin"))
	}
	scenarios := []scenario{
		{
			name:       "context",
			transports: []string{"REST", "gRPC"},
			prepare: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) (delivery.WorkItem, int64) {
				plan := "accepted context"
				updated, err := fixture.operations.UpdateContext(adminContext(t), item.ID, item.Revision, delivery.ContextUpdate{Plan: &plan})
				if err != nil {
					t.Fatalf("seed context update: %v", err)
				}
				return updated, item.Revision
			},
			restPath: func(id string) string { return "/api/items/" + id },
			restBody: func(revision int64) any { return map[string]any{"expectedRevision": revision, "plan": "stale context"} },
			grpcCall: func(ctx context.Context, client deliveryv1.DeliveryServiceClient, id string, revision int64) error {
				plan := "stale context"
				_, err := client.UpdateItemContext(ctx, &deliveryv1.UpdateItemContextRequest{Id: id, ExpectedRevision: revision, Plan: &plan})
				return err
			},
		},
		{
			name:       "gate",
			transports: []string{"REST", "gRPC", "MCP"},
			prepare: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) (delivery.WorkItem, int64) {
				updated, err := fixture.operations.AdvanceGate(adminContext(t), item.ID, item.Revision, delivery.GateSolutionReviewed, []delivery.Evidence{{Kind: "review", Title: "accepted"}})
				if err != nil {
					t.Fatalf("seed gate advance: %v", err)
				}
				return updated, item.Revision
			},
			restPath: func(id string) string { return "/api/items/" + id + "/gates/development_completed" },
			restBody: func(revision int64) any {
				return map[string]any{"expectedRevision": revision, "evidence": []map[string]string{{"kind": "test", "title": "stale"}}}
			},
			grpcCall: func(ctx context.Context, client deliveryv1.DeliveryServiceClient, id string, revision int64) error {
				_, err := client.AdvanceGate(ctx, &deliveryv1.AdvanceGateRequest{Id: id, ExpectedRevision: revision, Gate: string(delivery.GateDevelopmentCompleted), Evidence: []*deliveryv1.Evidence{{Kind: "test", Title: "stale"}}})
				return err
			},
			mcpTool: "delivery.advance_gate",
			mcpArgs: func(id string, revision int64) map[string]any {
				return map[string]any{"id": id, "expectedRevision": revision, "gate": string(delivery.GateDevelopmentCompleted), "evidence": []map[string]string{{"kind": "test", "title": "stale"}}}
			},
		},
		{
			name:       "close",
			transports: []string{"REST", "gRPC", "MCP"},
			prepare: func(t *testing.T, fixture *authorizationMatrixFixture, item delivery.WorkItem) (delivery.WorkItem, int64) {
				var err error
				for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
					item, err = fixture.operations.AdvanceGate(adminContext(t), item.ID, item.Revision, gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}})
					if err != nil {
						t.Fatalf("prepare close at %s: %v", gate, err)
					}
				}
				reviewer := identity.WithPrincipal(t.Context(), matrixPrincipal("reviewer"))
				item, err = fixture.operations.AdvanceGate(reviewer, item.ID, item.Revision, delivery.GateProductionValidated, []delivery.Evidence{{Kind: "validation", Title: "independent"}})
				if err != nil {
					t.Fatalf("prepare production validation: %v", err)
				}
				return item, item.Revision - 1
			},
			restPath: func(id string) string { return "/api/items/" + id + "/close" },
			restBody: func(revision int64) any {
				return map[string]any{"expectedRevision": revision, "retrospective": "stale"}
			},
			grpcCall: func(ctx context.Context, client deliveryv1.DeliveryServiceClient, id string, revision int64) error {
				_, err := client.CloseItem(ctx, &deliveryv1.CloseItemRequest{Id: id, ExpectedRevision: revision, Retrospective: "stale"})
				return err
			},
			mcpTool: "delivery.close_work_item",
			mcpArgs: func(id string, revision int64) map[string]any {
				return map[string]any{"id": id, "expectedRevision": revision, "retrospective": "stale"}
			},
		},
	}

	for _, scenario := range scenarios {
		for _, transport := range scenario.transports {
			t.Run(scenario.name+"/"+transport, func(t *testing.T) {
				fixture := newAuthorizationMatrixFixture(t)
				_, item := fixture.createProtectedItem(t)
				item, stale := scenario.prepare(t, fixture, item)
				item, err := fixture.repository.Get(t.Context(), item.ID)
				if err != nil {
					t.Fatalf("read governed item after preparation: %v", err)
				}
				beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				principal := matrixPrincipal("reviewer")
				if scenario.name != "close" {
					principal = matrixPrincipal("admin")
				}
				switch transport {
				case "REST":
					method := http.MethodPost
					if scenario.name == "context" {
						method = http.MethodPatch
					}
					code, category := governedWriteREST(t, fixture, principal, method, scenario.restPath(item.ID), scenario.restBody(stale))
					if code != http.StatusConflict || category != "revision_conflict" {
						t.Fatalf("REST stale %s = %d/%q, want 409/revision_conflict", scenario.name, code, category)
					}
				case "gRPC":
					callErr := scenario.grpcCall(t.Context(), governedWriteGRPCClient(t, fixture, principal), item.ID, stale)
					if result := status.Convert(callErr); result.Code() != codes.Aborted || result.Message() != "revision_conflict" {
						t.Fatalf("gRPC stale %s = %s/%q, want Aborted/revision_conflict", scenario.name, result.Code(), result.Message())
					}
				case "MCP":
					result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, scenario.mcpTool, scenario.mcpArgs(item.ID, stale))
					if !result.IsError || matrixMCPError(result) != "revision_conflict" {
						t.Fatalf("MCP stale %s = %#v text=%q, want revision_conflict", scenario.name, result, matrixMCPError(result))
					}
				}
				assertGovernedWriteUnchanged(t, fixture, item, beforeOutbox)
			})
		}
	}
}

func TestGovernedGateEvidenceRejectionLeavesBusinessAndOutboxUnchanged(t *testing.T) {
	for _, transport := range []string{"REST", "gRPC", "MCP"} {
		t.Run(transport, func(t *testing.T) {
			fixture := newAuthorizationMatrixFixture(t)
			_, item := fixture.createProtectedItem(t)
			beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			principal := matrixPrincipal("scoped")
			switch transport {
			case "REST":
				code, category := governedWriteREST(t, fixture, principal, http.MethodPost, "/api/items/"+item.ID+"/gates/solution_reviewed", map[string]any{"expectedRevision": item.Revision, "evidence": []any{}})
				if code != http.StatusUnprocessableEntity || category != delivery.ErrEvidenceRequired.Error() {
					t.Fatalf("REST empty evidence = %d/%q, want 422/%q", code, category, delivery.ErrEvidenceRequired)
				}
			case "gRPC":
				_, callErr := governedWriteGRPCClient(t, fixture, principal).AdvanceGate(t.Context(), &deliveryv1.AdvanceGateRequest{Id: item.ID, ExpectedRevision: item.Revision, Gate: string(delivery.GateSolutionReviewed)})
				if result := status.Convert(callErr); result.Code() != codes.Unknown || result.Message() != delivery.ErrEvidenceRequired.Error() {
					t.Fatalf("gRPC empty evidence = %s/%q, want Unknown/%q", result.Code(), result.Message(), delivery.ErrEvidenceRequired)
				}
			case "MCP":
				result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, "delivery.advance_gate", map[string]any{"id": item.ID, "expectedRevision": item.Revision, "gate": string(delivery.GateSolutionReviewed), "evidence": []any{}})
				if !result.IsError || matrixMCPError(result) != delivery.ErrEvidenceRequired.Error() {
					t.Fatalf("MCP empty evidence = %#v text=%q, want %q", result, matrixMCPError(result), delivery.ErrEvidenceRequired)
				}
			}
			assertGovernedWriteUnchanged(t, fixture, item, beforeOutbox)
		})
	}
}

func TestGovernedCommentAndContextADRPreserveCanonicalEvidenceAcrossTransports(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		transports []string
		call       func(*testing.T, string, *authorizationMatrixFixture, identity.Principal, delivery.WorkItem)
		assert     func(*testing.T, delivery.WorkItem)
	}{
		{
			name:       "comment",
			transports: []string{"REST", "gRPC", "MCP"},
			call: func(t *testing.T, transport string, fixture *authorizationMatrixFixture, principal identity.Principal, item delivery.WorkItem) {
				switch transport {
				case "REST":
					if code, category := governedWriteREST(t, fixture, principal, http.MethodPost, "/api/items/"+item.ID+"/comments", map[string]any{"expectedRevision": item.Revision, "body": "canonical comment"}); code != http.StatusCreated || category != "" {
						t.Fatalf("REST comment = %d/%q, want 201", code, category)
					}
				case "gRPC":
					if _, err := governedWriteGRPCClient(t, fixture, principal).CreateItemComment(t.Context(), &deliveryv1.CreateItemCommentRequest{Id: item.ID, ExpectedRevision: item.Revision, Body: "canonical comment"}); err != nil {
						t.Fatalf("gRPC comment: %v", err)
					}
				case "MCP":
					result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, "delivery.add_comment", map[string]any{"id": item.ID, "expectedRevision": item.Revision, "body": "canonical comment"})
					if result.IsError {
						t.Fatalf("MCP comment: %s", matrixMCPError(result))
					}
				}
			},
			assert: func(t *testing.T, item delivery.WorkItem) {
				if len(item.Comments) != 1 || item.Comments[0].Body != "canonical comment" || item.Comments[0].Author != "scoped" || item.Comments[0].CreatedAt.IsZero() {
					t.Fatalf("canonical comment evidence = %#v", item.Comments)
				}
			},
		},
		{
			name:       "context-adr",
			transports: []string{"REST", "gRPC"},
			call: func(t *testing.T, transport string, fixture *authorizationMatrixFixture, principal identity.Principal, item delivery.WorkItem) {
				decision := map[string]any{"title": "Canonical ADR", "context": "transport matrix", "outcome": "preserve evidence", "consequences": "stable behavior"}
				switch transport {
				case "REST":
					if code, category := governedWriteREST(t, fixture, principal, http.MethodPatch, "/api/items/"+item.ID, map[string]any{"expectedRevision": item.Revision, "plan": "canonical plan", "decision": decision}); code != http.StatusOK || category != "" {
						t.Fatalf("REST context/ADR = %d/%q, want 200", code, category)
					}
				case "gRPC":
					plan := "canonical plan"
					if _, err := governedWriteGRPCClient(t, fixture, principal).UpdateItemContext(t.Context(), &deliveryv1.UpdateItemContextRequest{Id: item.ID, ExpectedRevision: item.Revision, Plan: &plan, Decision: &deliveryv1.Decision{Title: "Canonical ADR", Context: "transport matrix", Outcome: "preserve evidence", Consequences: "stable behavior"}}); err != nil {
						t.Fatalf("gRPC context/ADR: %v", err)
					}
				}
			},
			assert: func(t *testing.T, item delivery.WorkItem) {
				if item.Plan != "canonical plan" || len(item.Decisions) != 1 || item.Decisions[0].ID == "" || item.Decisions[0].Title != "Canonical ADR" || item.Decisions[0].Outcome != "preserve evidence" || item.Decisions[0].CreatedAt.IsZero() {
					t.Fatalf("canonical context/ADR evidence = plan %q decisions %#v", item.Plan, item.Decisions)
				}
			},
		},
	} {
		for _, transport := range scenario.transports {
			t.Run(scenario.name+"/"+transport, func(t *testing.T) {
				fixture := newAuthorizationMatrixFixture(t)
				_, item := fixture.createProtectedItem(t)
				beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				scenario.call(t, transport, fixture, matrixPrincipal("scoped"), item)
				stored, err := fixture.repository.Get(t.Context(), item.ID)
				if err != nil || stored.Revision != item.Revision+1 {
					t.Fatalf("%s %s revision = %d err=%v, want %d", transport, scenario.name, stored.Revision, err, item.Revision+1)
				}
				scenario.assert(t, stored)
				afterOutbox, err := fixture.outbox.Snapshot(t.Context())
				if err != nil || afterOutbox.Pending <= beforeOutbox.Pending {
					t.Fatalf("%s %s Outbox = before %#v after %#v err=%v", transport, scenario.name, beforeOutbox, afterOutbox, err)
				}
			})
		}
	}
}

func TestGovernedProductionValidationAndCloseRequireIndependentJWTIdentityAcrossTransports(t *testing.T) {
	for _, transport := range []string{"REST", "gRPC", "MCP"} {
		t.Run(transport, func(t *testing.T) {
			fixture := newAuthorizationMatrixFixture(t)
			_, item := fixture.createProtectedItem(t)
			admin := matrixPrincipal("admin")
			reviewer := matrixPrincipal("reviewer")
			adminContext := identity.WithPrincipal(t.Context(), admin)
			var err error
			for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
				item, err = fixture.operations.AdvanceGate(adminContext, item.ID, item.Revision, gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}})
				if err != nil {
					t.Fatalf("prepare independent validation at %s: %v", gate, err)
				}
			}

			beforeDenied, err := fixture.repository.Get(t.Context(), item.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeDeniedOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			callGate := func(principal identity.Principal) (bool, string) {
				switch transport {
				case "REST":
					code, category := governedWriteREST(t, fixture, principal, http.MethodPost, "/api/items/"+item.ID+"/gates/production_validated", map[string]any{"expectedRevision": item.Revision, "evidence": []map[string]string{{"kind": "validation", "title": "independent"}}})
					return code == http.StatusOK, category
				case "gRPC":
					_, callErr := governedWriteGRPCClient(t, fixture, principal).AdvanceGate(t.Context(), &deliveryv1.AdvanceGateRequest{Id: item.ID, ExpectedRevision: item.Revision, Gate: string(delivery.GateProductionValidated), Evidence: []*deliveryv1.Evidence{{Kind: "validation", Title: "independent"}}})
					if callErr == nil {
						return true, ""
					}
					if status.Code(callErr) == codes.PermissionDenied {
						return false, "permission_denied"
					}
					return false, status.Convert(callErr).Message()
				default:
					result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, "delivery.advance_gate", map[string]any{"id": item.ID, "expectedRevision": item.Revision, "gate": string(delivery.GateProductionValidated), "evidence": []map[string]string{{"kind": "validation", "title": "independent"}}})
					if !result.IsError {
						return true, ""
					}
					return false, matrixMCPError(result)
				}
			}
			if allowed, category := callGate(admin); allowed || category != "permission_denied" {
				t.Fatalf("%s implementer production validation = allowed %v/%q, want denied/permission_denied", transport, allowed, category)
			}
			assertGovernedWriteUnchanged(t, fixture, beforeDenied, beforeDeniedOutbox)
			if allowed, category := callGate(reviewer); !allowed || category != "" {
				t.Fatalf("%s independent production validation = allowed %v/%q, want allowed", transport, allowed, category)
			}

			item, err = fixture.repository.Get(t.Context(), item.ID)
			if err != nil || item.ProductionValidationPrincipal.SubjectID != "reviewer" || item.ImplementationPrincipal.SubjectID != "admin" {
				t.Fatalf("%s canonical production principals = %#v err=%v", transport, item, err)
			}
			beforeDeniedOutbox, err = fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			callClose := func(principal identity.Principal) (bool, string) {
				switch transport {
				case "REST":
					code, category := governedWriteREST(t, fixture, principal, http.MethodPost, "/api/items/"+item.ID+"/close", map[string]any{"expectedRevision": item.Revision, "retrospective": "independent retrospective"})
					return code == http.StatusOK, category
				case "gRPC":
					_, callErr := governedWriteGRPCClient(t, fixture, principal).CloseItem(t.Context(), &deliveryv1.CloseItemRequest{Id: item.ID, ExpectedRevision: item.Revision, Retrospective: "independent retrospective"})
					if callErr == nil {
						return true, ""
					}
					if status.Code(callErr) == codes.PermissionDenied {
						return false, "permission_denied"
					}
					return false, status.Convert(callErr).Message()
				default:
					result := callMatrixMCPContext(t, t.Context(), fixture.operations, principal, "delivery.close_work_item", map[string]any{"id": item.ID, "expectedRevision": item.Revision, "retrospective": "independent retrospective"})
					if !result.IsError {
						return true, ""
					}
					return false, matrixMCPError(result)
				}
			}
			if allowed, category := callClose(admin); allowed || category != "permission_denied" {
				t.Fatalf("%s implementer close = allowed %v/%q, want denied/permission_denied", transport, allowed, category)
			}
			assertGovernedWriteUnchanged(t, fixture, item, beforeDeniedOutbox)
			if allowed, category := callClose(reviewer); !allowed || category != "" {
				t.Fatalf("%s independent close = allowed %v/%q, want allowed", transport, allowed, category)
			}
			closed, err := fixture.repository.Get(t.Context(), item.ID)
			if err != nil || closed.Status != delivery.StatusClosed || closed.Retrospective != "independent retrospective" {
				t.Fatalf("%s independent close result = %#v err=%v", transport, closed, err)
			}
		})
	}
}
