package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
)

type restFixture struct {
	handler    http.Handler
	rawHandler http.Handler
	repository *delivery.SQLiteRepository
	outbox     *localoutbox.SQLiteStore
}

var (
	_ func(*deliveryapplication.Operations) http.Handler    = httpapi.NewHandler
	_ func(*http.ServeMux, *deliveryapplication.Operations) = httpapi.Register
)

func newRESTFixture(t *testing.T) restFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
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
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	operations := deliveryapplication.NewOperations(
		deliveryapplication.NewAdapter(service),
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
		service,
	)
	rawHandler := httpapi.NewHandler(operations)
	principal := identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		UserID:        "rest-local-admin",
		Roles:         []string{localauth.RoleLocalAdmin},
	}
	return restFixture{
		handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			rawHandler.ServeHTTP(writer, request.WithContext(identity.WithPrincipal(request.Context(), principal)))
		}),
		rawHandler: rawHandler,
		repository: repository,
		outbox:     outbox,
	}
}

func TestRESTWriteRoutesUseOperationsWithSQLiteExecutorAndOutbox(t *testing.T) {
	fixture := newRESTFixture(t)
	project := requestJSON(t, fixture.handler, http.MethodPost, "/api/projects", `{"name":"REST execution boundary","board":"研发交付效能","owner":"delivery-owner"}`, http.StatusCreated)
	projectID := responseID(t, project)
	release := requestJSON(t, fixture.handler, http.MethodPost, "/api/releases", `{"projectId":"`+projectID+`","name":"R1","version":"1.0.0"}`, http.StatusCreated)
	sprint := requestJSON(t, fixture.handler, http.MethodPost, "/api/sprints", `{"projectId":"`+projectID+`","name":"Sprint 1","startDate":"2026-09-01","endDate":"2026-09-10"}`, http.StatusCreated)
	milestone := requestJSON(t, fixture.handler, http.MethodPost, "/api/milestones", `{"projectId":"`+projectID+`","name":"Gate","targetDate":"2026-09-10"}`, http.StatusCreated)
	item := requestJSON(t, fixture.handler, http.MethodPost, "/api/items", `{"title":"route compatibility","board":"研发交付效能","owner":"delivery-owner","projectId":"`+projectID+`","kind":"task","releaseId":"`+responseID(t, release)+`","sprintId":"`+responseID(t, sprint)+`","milestoneId":"`+responseID(t, milestone)+`"}`, http.StatusCreated)
	itemID := responseID(t, item)
	requestJSON(t, fixture.handler, http.MethodPatch, "/api/items/"+itemID, `{"progressPercent":40,"plan":"record delivery context","decision":{"title":"REST executor","outcome":"keep route compatibility"}}`, http.StatusOK)
	requestJSON(t, fixture.handler, http.MethodPost, "/api/items/"+itemID+"/comments", `{"body":"executor-backed comment"}`, http.StatusCreated)
	requestJSON(t, fixture.handler, http.MethodPost, "/api/items/"+itemID+"/gates/solution_reviewed", `{"evidence":[{"kind":"review","title":"solution approved","reference":"ADR-REST-001"}]}`, http.StatusOK)
	requestJSON(t, fixture.handler, http.MethodPost, "/api/views", `{"name":"REST boundary","filter":{"projectId":"`+projectID+`"}}`, http.StatusCreated)

	for _, gate := range []string{"development_completed", "test_passed", "production_validated"} {
		requestJSON(t, fixture.handler, http.MethodPost, "/api/items/"+itemID+"/gates/"+gate, `{"evidence":[{"kind":"test","title":"`+gate+`"}]}`, http.StatusOK)
	}
	closed := requestJSON(t, fixture.handler, http.MethodPost, "/api/items/"+itemID+"/close", `{"retrospective":"kept REST contract"}`, http.StatusOK)
	if closed["status"] != string(delivery.StatusClosed) {
		t.Fatalf("close response = %#v, want closed item", closed)
	}
	snapshot, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || snapshot.Pending < 11 {
		t.Fatalf("REST writes outbox = %#v, %v; want all writes staged", snapshot, err)
	}
}

func TestRESTRejectsNonPOSTGateAndCloseWithoutSideEffects(t *testing.T) {
	fixture := newRESTFixture(t)
	for _, action := range []struct {
		name   string
		method string
		path   string
		body   string
		setup  func(*testing.T) string
	}{
		{
			name:   "GET gate",
			method: http.MethodGet,
			body:   `{"evidence":[{"kind":"review","title":"approved"}]}`,
			setup: func(t *testing.T) string {
				return createRESTItem(t, fixture.handler)
			},
		},
		{
			name:   "PATCH gate",
			method: http.MethodPatch,
			body:   `{"evidence":[{"kind":"review","title":"approved"}]}`,
			setup: func(t *testing.T) string {
				return createRESTItem(t, fixture.handler)
			},
		},
		{
			name:   "GET close",
			method: http.MethodGet,
			body:   `{"retrospective":"should not close"}`,
			setup: func(t *testing.T) string {
				return createProductionValidatedRESTItem(t, fixture.handler)
			},
		},
		{
			name:   "PATCH close",
			method: http.MethodPatch,
			body:   `{"retrospective":"should not close"}`,
			setup: func(t *testing.T) string {
				return createProductionValidatedRESTItem(t, fixture.handler)
			},
		},
	} {
		t.Run(action.name, func(t *testing.T) {
			itemID := action.setup(t)
			if strings.Contains(action.name, "gate") {
				action.path = "/api/items/" + itemID + "/gates/solution_reviewed"
			} else {
				action.path = "/api/items/" + itemID + "/close"
			}
			beforeItem, err := fixture.repository.Get(t.Context(), itemID)
			if err != nil {
				t.Fatalf("read item before %s: %v", action.name, err)
			}
			beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil {
				t.Fatalf("read outbox before %s: %v", action.name, err)
			}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(action.method, action.path, strings.NewReader(action.body))
			fixture.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s response = %d, want %d: %s", action.name, recorder.Code, http.StatusMethodNotAllowed, recorder.Body.String())
			}
			if got := recorder.Header().Get("Allow"); got != http.MethodPost {
				t.Fatalf("%s Allow = %q, want %q", action.name, got, http.MethodPost)
			}
			afterItem, err := fixture.repository.Get(t.Context(), itemID)
			if err != nil || !reflect.DeepEqual(afterItem, beforeItem) {
				t.Fatalf("%s changed item = %#v, %v; want %#v", action.name, afterItem, err, beforeItem)
			}
			afterOutbox, err := fixture.outbox.Snapshot(t.Context())
			if err != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
				t.Fatalf("%s changed outbox = %#v, %v; want %#v", action.name, afterOutbox, err, beforeOutbox)
			}
		})
	}
}

func TestRESTUnauthorizedAndViewerWritesLeaveSQLiteAndOutboxUnchanged(t *testing.T) {
	fixture := newRESTFixture(t)
	beforeOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("read initial outbox: %v", err)
	}
	unauthenticated := httptest.NewRecorder()
	fixture.rawHandler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(`{"title":"denied","board":"研发交付效能","owner":"viewer"}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create = %d, want %d: %s", unauthenticated.Code, http.StatusUnauthorized, unauthenticated.Body.String())
	}
	items, err := fixture.repository.List(t.Context())
	if err != nil || len(items) != 0 {
		t.Fatalf("unauthenticated create changed items = %#v, %v", items, err)
	}

	itemID := createRESTItem(t, fixture.handler)
	beforeItem, err := fixture.repository.Get(t.Context(), itemID)
	if err != nil {
		t.Fatalf("read item before viewer gate: %v", err)
	}
	beforeOutbox, err = fixture.outbox.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("read outbox before viewer gate: %v", err)
	}
	viewer := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, Roles: []string{localauth.RoleViewer}})
	viewerRequest := httptest.NewRequest(http.MethodPost, "/api/items/"+itemID+"/gates/solution_reviewed", strings.NewReader(`{"evidence":[{"kind":"review","title":"denied"}]}`)).WithContext(viewer)
	viewerRecorder := httptest.NewRecorder()
	fixture.rawHandler.ServeHTTP(viewerRecorder, viewerRequest)
	if viewerRecorder.Code != http.StatusForbidden {
		t.Fatalf("viewer gate = %d, want %d: %s", viewerRecorder.Code, http.StatusForbidden, viewerRecorder.Body.String())
	}
	afterItem, err := fixture.repository.Get(t.Context(), itemID)
	if err != nil || !reflect.DeepEqual(afterItem, beforeItem) {
		t.Fatalf("viewer gate changed item = %#v, %v; want %#v", afterItem, err, beforeItem)
	}
	afterOutbox, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
		t.Fatalf("viewer gate changed outbox = %#v, %v; want %#v", afterOutbox, err, beforeOutbox)
	}
}

func TestHTTPAPISourceRequiresOperationsBoundary(t *testing.T) {
	for _, name := range []string{"handler.go", "r2.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(source)
		for _, forbidden := range []string{"type Service interface", "type ProjectService interface", "type SimilarityService interface", "type NotificationService interface", "type R2Service interface", "*delivery.Service", "repository.", "api.service", "api.r2Service", ".(ProjectService)", ".(R2Service)"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s retains forbidden REST bypass %q", name, forbidden)
			}
		}
	}
	source, err := os.ReadFile("handler.go")
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}
	for _, required := range []string{"func NewHandler(operations *application.Operations)", "func Register(mux *http.ServeMux, operations *application.Operations)", "operations *application.Operations"} {
		if !strings.Contains(string(source), required) {
			t.Errorf("handler.go does not require the Operations boundary %q", required)
		}
	}
}

func createRESTItem(t *testing.T, handler http.Handler) string {
	t.Helper()
	title := strings.ReplaceAll(t.Name(), "/", "-")
	item := requestJSON(t, handler, http.MethodPost, "/api/items", `{"title":"method boundary `+title+`","board":"研发交付效能","owner":"delivery-owner"}`, http.StatusCreated)
	return responseID(t, item)
}

func createProductionValidatedRESTItem(t *testing.T, handler http.Handler) string {
	t.Helper()
	itemID := createRESTItem(t, handler)
	for _, gate := range []string{"solution_reviewed", "development_completed", "test_passed", "production_validated"} {
		requestJSON(t, handler, http.MethodPost, "/api/items/"+itemID+"/gates/"+gate, `{"evidence":[{"kind":"test","title":"`+gate+`"}]}`, http.StatusOK)
	}
	return itemID
}

func requestJSON(t *testing.T, handler http.Handler, method, path, body string, wantStatus int) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, recorder.Code, wantStatus, recorder.Body.String())
	}
	var value map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&value); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return value
}

func responseID(t *testing.T, value map[string]any) string {
	t.Helper()
	id, _ := value["id"].(string)
	if id == "" {
		t.Fatalf("response ID = %#v, want non-empty", value["id"])
	}
	return id
}
