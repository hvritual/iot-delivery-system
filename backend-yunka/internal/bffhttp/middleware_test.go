package bffhttp_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffhttp"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitybinding"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
)

func TestBFFHTTPBindsDifferentExternalUsersToStableActorsAndTrace(t *testing.T) {
	fixture := newFixture(t)
	first := fixture.call(t, "subject-1", "nonce-000000000001", `{"title":"first actor","board":"研发交付效能","owner":"owner"}`)
	second := fixture.call(t, "subject-2", "nonce-000000000002", `{"title":"second actor","board":"研发交付效能","owner":"owner"}`)
	again := fixture.call(t, "subject-1", "nonce-000000000003", `{"title":"stable actor","board":"研发交付效能","owner":"owner"}`)

	if first.Actor == "" || second.Actor == "" || first.Actor == second.Actor || first.Actor != again.Actor {
		t.Fatalf("actors first=%q second=%q again=%q, want two distinct stable internal actors", first.Actor, second.Actor, again.Actor)
	}
	for _, result := range []callResult{first, second, again} {
		if result.TraceID != "00000000000000000000000000000001" {
			t.Fatalf("response trace = %#v, want the signed assertion trace", result)
		}
	}
	seen := fixture.observer.snapshots()
	if len(seen) < 3 {
		t.Fatalf("executor observed %d calls, want at least 3", len(seen))
	}
	for _, snapshot := range seen[:3] {
		if !snapshot.Principal.Authenticated || snapshot.Principal.AuthMethod != identity.AuthMethodJWT || snapshot.Principal.UserID == "" || snapshot.TraceID != "00000000000000000000000000000001" || snapshot.RequestID != snapshot.TraceID {
			t.Fatalf("executor context = %#v, want bound JWT principal and assertion trace", snapshot)
		}
	}
}

func TestBFFHTTPRejectsBadSignatureAndReplayWithoutMutation(t *testing.T) {
	fixture := newFixture(t)
	body := []byte(`{"title":"must not mutate","board":"研发交付效能","owner":"owner"}`)
	request := fixture.signedRequest(t, "subject-1", "nonce-000000000010", body)
	request.Header.Set(bffassertion.SignatureHeader, "invalid")
	assertRejectedWithoutMutation(t, fixture, request)

	request = fixture.signedRequest(t, "subject-1", "nonce-000000000011", body)
	if response := fixture.serve(request); response.Code != http.StatusCreated {
		t.Fatalf("first request = %d body=%s", response.Code, response.Body.String())
	}
	assertRejectedWithoutMutation(t, fixture, request)
}

func TestBFFHTTPRejectsExpiredAndRequestTamperedAssertionsWithoutMutation(t *testing.T) {
	fixture := newFixture(t)
	body := []byte(`{"title":"must not mutate","board":"研发交付效能","owner":"owner"}`)
	tests := []struct {
		name    string
		request func() *http.Request
	}{
		{
			name: "expired",
			request: func() *http.Request {
				request := fixture.signedRequest(t, "subject-1", "nonce-expired-0001", body)
				fixture.rewriteClaims(t, request, func(claims *bffassertion.Claims) { claims.Exp = fixture.now.Add(-time.Second).Unix() })
				return request
			},
		},
		{
			name: "method",
			request: func() *http.Request {
				request := fixture.signedRequest(t, "subject-1", "nonce-method-00001", body)
				request.Method = http.MethodGet
				return request
			},
		},
		{
			name: "path",
			request: func() *http.Request {
				request := fixture.signedRequest(t, "subject-1", "nonce-path-000001", body)
				request.URL.RawQuery = "projectId=unexpected"
				return request
			},
		},
		{
			name: "body",
			request: func() *http.Request {
				request := fixture.signedRequest(t, "subject-1", "nonce-body-000001", body)
				request.Body = io.NopCloser(bytes.NewReader([]byte(`{"title":"tampered","board":"研发交付效能","owner":"owner"}`)))
				return request
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertRejectedWithoutMutation(t, fixture, test.request()) })
	}
}

func TestBFFHTTPAcceptsSignedExtensionQuery(t *testing.T) {
	fixture := newFixture(t)
	response := fixture.serve(fixture.signedRequestTo(t, http.MethodGet, "/api/projects", "subject-1", "nonce-extension-001", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("signed extension query = %d body=%s", response.Code, response.Body.String())
	}
}

func TestBFFHTTPAddsTheSameTraceToDomainErrors(t *testing.T) {
	fixture := newFixture(t)
	response := fixture.serve(fixture.signedRequest(t, "subject-1", "nonce-000000000020", []byte(`{"title":""}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("domain error = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		TraceID string `json:"traceId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.TraceID == "" || payload.TraceID != response.Header().Get(bffassertion.TraceHeader) {
		t.Fatalf("domain trace payload=%#v err=%v header=%q", payload, err, response.Header().Get(bffassertion.TraceHeader))
	}
}

func TestBFFHTTPRetainsVerifiedTraceWhenBindingFails(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, fixture fixture) http.Handler
	}{
		{
			name: "missing organization",
			setup: func(t *testing.T, fixture fixture) http.Handler {
				return fixture.handlerFor(t, "missing-org")
			},
		},
		{
			name: "disabled organization",
			setup: func(t *testing.T, fixture fixture) http.Handler {
				if err := fixture.resolver.DisableOrganization(t.Context(), "org-1"); err != nil {
					t.Fatal(err)
				}
				return fixture.handlerFor(t, "org-1")
			},
		},
		{
			name: "cross organization identity",
			setup: func(t *testing.T, fixture fixture) http.Handler {
				if _, err := fixture.database.Exec(`INSERT INTO organizations (id, slug, name, status) VALUES ('org-2', 'org-2', 'Other Organization', 'active')`); err != nil {
					t.Fatal(err)
				}
				initial := fixture.serve(fixture.signedRequestTo(t, http.MethodGet, "/api/projects", "subject-cross", "nonce-cross-initial", nil))
				if initial.Code != http.StatusOK {
					t.Fatalf("initial binding = %d body=%s", initial.Code, initial.Body.String())
				}
				return fixture.handlerFor(t, "org-2")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			handler := test.setup(t, fixture)
			request := fixture.signedRequestTo(t, http.MethodGet, "/api/projects", "subject-cross", "nonce-binding-failure", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("binding response = %d body=%s", response.Code, response.Body.String())
			}
			assertResponseTrace(t, response, "00000000000000000000000000000001")
		})
	}
}

func TestBFFHTTPBFFOnlyModeRequiresAssertionAndDoesNotAssignLegacyRoles(t *testing.T) {
	fixture := newFixture(t)
	middleware, err := bffhttp.NewMiddleware(bffhttp.Config{
		Verifier:            fixture.verifier,
		Resolver:            fixture.resolver,
		OrganizationID:      "org-1",
		AllowLegacyFallback: false,
	})
	if err != nil {
		t.Fatalf("construct BFF-only middleware: %v", err)
	}
	var principal identity.Principal
	handler := middleware.HTTPMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, _ = identity.FromContext(request.Context())
		writer.WriteHeader(http.StatusNoContent)
	}))

	localOnly := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	localOnly.Header.Set(localauth.APIKeyHeader, "bff-channel-key")
	localResponse := httptest.NewRecorder()
	handler.ServeHTTP(localResponse, localOnly)
	if localResponse.Code != http.StatusUnauthorized {
		t.Fatalf("BFF-only local API-key response = %d, want %d", localResponse.Code, http.StatusUnauthorized)
	}

	signed := fixture.signedRequestTo(t, http.MethodGet, "/api/items", "subject-production", "nonce-production-bff-only", nil)
	signedResponse := httptest.NewRecorder()
	handler.ServeHTTP(signedResponse, signed)
	if signedResponse.Code != http.StatusNoContent {
		t.Fatalf("BFF-only signed assertion response = %d body=%s, want %d", signedResponse.Code, signedResponse.Body.String(), http.StatusNoContent)
	}
	if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || len(principal.Roles) != 0 {
		t.Fatalf("BFF-only principal = %#v, want authenticated JWT principal without legacy roles", principal)
	}

	deniedHandler := middleware.HTTPMiddleware(fixture.upstream)
	deniedRequest := fixture.signedRequestTo(t, http.MethodGet, "/api/projects", "subject-production", "nonce-production-bff-only-denied", nil)
	deniedResponse := httptest.NewRecorder()
	deniedHandler.ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("BFF-only business request = %d body=%s, want %d without S0-03 role bindings", deniedResponse.Code, deniedResponse.Body.String(), http.StatusForbidden)
	}
}

func TestLegacyAPIKeyTraceMiddlewarePreservesFirstSuccessStatus(t *testing.T) {
	authenticator, err := localauth.NewAuthenticator("legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	handler := bffhttp.APIKeyTraceMiddleware(authenticator)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set(localauth.APIKeyHeader, "legacy-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != `{"ok":true}` {
		t.Fatalf("legacy success status/body = %d %q, want 200 unchanged", response.Code, response.Body.String())
	}
	if traceID := response.Header().Get(bffassertion.TraceHeader); len(traceID) != 32 {
		t.Fatalf("legacy trace = %q, want generated trace", traceID)
	}
}

func TestLegacyAPIKeyTraceMiddlewareRejectsPartialAssertion(t *testing.T) {
	authenticator, err := localauth.NewAuthenticator("legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	invoked := false
	handler := bffhttp.APIKeyTraceMiddleware(authenticator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { invoked = true }))
	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set(localauth.APIKeyHeader, "legacy-key")
	request.Header.Set(bffassertion.TraceHeader, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || invoked {
		t.Fatalf("partial assertion status=%d invoked=%t, want 401 and no invocation", response.Code, invoked)
	}
}

func TestTraceResponseWriterHidesInternalErrorAndBoundsBody(t *testing.T) {
	authenticator, err := localauth.NewAuthenticator("legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("credential-profile-secret", 8_192)
	handler := bffhttp.APIKeyTraceMiddleware(authenticator)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(secret))
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set(localauth.APIKeyHeader, "legacy-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "credential-profile-secret") || !strings.Contains(response.Body.String(), `"error":"internal_error"`) {
		t.Fatalf("internal error response leaked or changed: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestTraceResponseWriterSafelyNormalizesNonObjectClientErrors(t *testing.T) {
	for name, body := range map[string]string{
		"empty body":   "",
		"empty object": "{}",
		"null":         "null",
		"array":        "[]",
		"string":       `"not an object"`,
		"malformed":    `{"error":`,
		"truncated":    `{"error":"` + strings.Repeat("x", maxTestErrorBodyBytes),
	} {
		t.Run(name, func(t *testing.T) {
			authenticator, err := localauth.NewAuthenticator("legacy-key")
			if err != nil {
				t.Fatal(err)
			}
			handler := bffhttp.APIKeyTraceMiddleware(authenticator)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(body))
			}))
			request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			request.Header.Set(localauth.APIKeyHeader, "legacy-key")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || response.Body.Len() > 1024 {
				t.Fatalf("normalized client error = status=%d body=%q", response.Code, response.Body.String())
			}
			var payload map[string]string
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload["error"] != "request_failed" {
				t.Fatalf("normalized payload=%#v err=%v", payload, err)
			}
			if payload["traceId"] == "" || response.Header().Get(bffassertion.TraceHeader) != payload["traceId"] {
				t.Fatalf("trace header/body mismatch header=%q body=%#v", response.Header().Get(bffassertion.TraceHeader), payload)
			}
		})
	}
}

const maxTestErrorBodyBytes = 128 << 10

type fixture struct {
	handler       http.Handler
	upstream      http.Handler
	service       *delivery.Service
	observer      *captureObserver
	database      *sql.DB
	resolver      *identitybinding.Resolver
	verifier      *bffassertion.Verifier
	authenticator *localauth.Authenticator
	key           []byte
	now           time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(context.Background(), repository.Database()); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Database().Exec(`INSERT INTO organizations (id, slug, name, status) VALUES ('org-1', 'org-1', 'Test Organization', 'active')`); err != nil {
		t.Fatal(err)
	}
	resolver, err := identitybinding.NewSQLiteResolver(repository.Database(), identitybinding.Config{})
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	observer := &captureObserver{}
	operations := deliveryapplication.NewOperations(deliveryapplication.NewAdapter(service), operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}, observer), service)
	key := []byte("01234567890123456789012345678901")
	verifier, err := bffassertion.NewVerifier(bffassertion.Config{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := localauth.NewAuthenticator("bff-channel-key")
	if err != nil {
		t.Fatal(err)
	}
	middleware, err := bffhttp.NewMiddleware(bffhttp.Config{Authenticator: authenticator, Verifier: verifier, Resolver: resolver, OrganizationID: "org-1", AllowLegacyFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	upstream := httpapi.NewHandler(operations)
	return fixture{handler: middleware.HTTPMiddleware(upstream), upstream: upstream, service: service, observer: observer, database: repository.Database(), resolver: resolver, verifier: verifier, authenticator: authenticator, key: key, now: time.Now().UTC()}
}

func (fixture fixture) handlerFor(t *testing.T, organizationID string) http.Handler {
	t.Helper()
	middleware, err := bffhttp.NewMiddleware(bffhttp.Config{Authenticator: fixture.authenticator, Verifier: fixture.verifier, Resolver: fixture.resolver, OrganizationID: organizationID, AllowLegacyFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	return middleware.HTTPMiddleware(fixture.upstream)
}

type callResult struct{ Actor, TraceID string }

func (fixture fixture) call(t *testing.T, subject, nonce, body string) callResult {
	t.Helper()
	response := fixture.serve(fixture.signedRequest(t, subject, nonce, []byte(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d body=%s", response.Code, response.Body.String())
	}
	var item delivery.WorkItem
	if err := json.NewDecoder(response.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	if len(item.Activities) == 0 {
		t.Fatalf("item has no activity: %#v", item)
	}
	return callResult{Actor: item.Activities[0].Actor, TraceID: response.Header().Get(bffassertion.TraceHeader)}
}

type executionSnapshot struct {
	Principal identity.Principal
	TraceID   string
	RequestID string
}

type captureObserver struct {
	mu   sync.Mutex
	seen []executionSnapshot
}

func (observer *captureObserver) Observe(ctx context.Context, event operation.Event) {
	if event.Kind != operation.InvocationRoot || event.Phase != operation.PhaseApplication || event.Outcome != operation.OutcomeStarted {
		return
	}
	principal, _ := identity.FromContext(ctx)
	metadata, _ := runtimecontext.MetadataFrom(ctx)
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.seen = append(observer.seen, executionSnapshot{Principal: principal, TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: metadata.RequestID})
}

func (observer *captureObserver) snapshots() []executionSnapshot {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]executionSnapshot(nil), observer.seen...)
}

func (fixture fixture) signedRequest(t *testing.T, subject, nonce string, body []byte) *http.Request {
	t.Helper()
	return fixture.signedRequestTo(t, http.MethodPost, "/api/items", subject, nonce, body)
}

func (fixture fixture) signedRequestTo(t *testing.T, method, path, subject, nonce string, body []byte) *http.Request {
	t.Helper()
	digest := sha256.Sum256(body)
	claims := bffassertion.Claims{Version: 1, Issuer: "https://issuer.example/tenant", Subject: subject, Nonce: nonce, TraceID: "00000000000000000000000000000001", Method: method, Path: path, BodySHA256: hex.EncodeToString(digest[:]), Iat: fixture.now.Unix(), Exp: fixture.now.Add(time.Minute).Unix()}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, fixture.key)
	_, _ = mac.Write([]byte(encoded))
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set(localauth.APIKeyHeader, "bff-channel-key")
	request.Header.Set(bffassertion.AssertionHeader, encoded)
	request.Header.Set(bffassertion.SignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set(bffassertion.TraceHeader, claims.TraceID)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func (fixture fixture) rewriteClaims(t *testing.T, request *http.Request, rewrite func(*bffassertion.Claims)) {
	t.Helper()
	payload, err := base64.RawURLEncoding.DecodeString(request.Header.Get(bffassertion.AssertionHeader))
	if err != nil {
		t.Fatal(err)
	}
	var claims bffassertion.Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	rewrite(&claims)
	payload, err = json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, fixture.key)
	_, _ = mac.Write([]byte(encoded))
	request.Header.Set(bffassertion.AssertionHeader, encoded)
	request.Header.Set(bffassertion.SignatureHeader, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}

func (fixture fixture) serve(request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	return response
}

func assertRejectedWithoutMutation(t *testing.T, fixture fixture, request *http.Request) {
	t.Helper()
	before, err := fixture.service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response := fixture.serve(request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("rejected = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		TraceID string `json:"traceId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.TraceID == "" || response.Header().Get(bffassertion.TraceHeader) != payload.TraceID {
		t.Fatalf("trace error payload=%#v error=%v headers=%v", payload, err, response.Header())
	}
	after, err := fixture.service.List(context.Background())
	if err != nil || len(after) != len(before) {
		t.Fatalf("rejected request mutated items before=%d after=%d error=%v", len(before), len(after), err)
	}
}

func assertResponseTrace(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	var payload struct {
		TraceID string `json:"traceId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil || payload.TraceID != want || response.Header().Get(bffassertion.TraceHeader) != want {
		t.Fatalf("trace error payload=%#v error=%v headers=%v, want %q", payload, err, response.Header(), want)
	}
}
