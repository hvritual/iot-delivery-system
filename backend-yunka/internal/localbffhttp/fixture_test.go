package localbffhttp

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtransportauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type bffFixture struct {
	repository  *delivery.SQLiteRepository
	database    *sql.DB
	mux         *http.ServeMux
	login       *locallogin.Manager
	credentials *localcredential.SQLiteRepository
	now         time.Time
}

type guardMux []authz.GuardResolver

func (mux guardMux) ResolveGuard(operationID authz.OperationID) (authz.OperationGuard, bool) {
	for _, resolver := range mux {
		if resolver == nil {
			continue
		}
		if guard, ok := resolver.ResolveGuard(operationID); ok {
			return guard, true
		}
	}
	return nil, false
}

func newBFFFixture(t *testing.T) *bffFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu26.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	database := repository.Database()
	for _, migrate := range []func() error{
		func() error { return identitycore.ApplyMigrations(t.Context(), database) },
		func() error { return localcredential.ApplyMigrations(t.Context(), database) },
		func() error { return localmemberadmin.ApplyMigrations(t.Context(), database) },
		func() error { return localprojectroleadmin.ApplyMigrations(t.Context(), database) },
		func() error { return locallogin.ApplyMigrations(t.Context(), database) },
		func() error { return audit.ApplyMigrations(t.Context(), database) },
	} {
		if err := migrate(); err != nil {
			t.Fatal(err)
		}
	}
	outboxStore, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 10, 30, 0, 0, time.UTC)
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Organization A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('admin-a', 'org-a', 'Admin A', 'active', 1)`,
		`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('user-a', 'org-a', 'User A', 'active', 1)`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status, revision) VALUES ('binding-admin-a', 'org-a', 'system-administrator', 'organization', 'org-a', 'admin-a', 'active', 1)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.SetPassword(t.Context(), "org-a", "admin-a", []byte("YU26-admin-password"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("YU26-user-password"), 0); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateProject(t.Context(), delivery.Project{
		ID: "project-a", OrganizationID: "org-a", Name: "Project A",
		Board: delivery.BoardResearchDelivery, Owner: "admin-a", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		t.Fatal(err)
	}
	humanResolver, err := humanauthz.NewGrantResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(humanResolver)
	if err != nil {
		t.Fatal(err)
	}
	memberGuard, err := localmemberadmin.NewOperationGuard(database)
	if err != nil {
		t.Fatal(err)
	}
	projectGuard, err := localprojectroleadmin.NewOperationGuard(database)
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guardMux{memberGuard.GuardResolver(), projectGuard.GuardResolver()})
	if err != nil {
		t.Fatal(err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(database),
	}), recorder)
	if err != nil {
		t.Fatal(err)
	}
	memberAdmin, err := localmemberadmin.NewManager(database, credentials, auditStore, outboxStore, executor)
	if err != nil {
		t.Fatal(err)
	}
	projectAdmin, err := localprojectroleadmin.NewManager(database, repository, auditStore, outboxStore, executor)
	if err != nil {
		t.Fatal(err)
	}
	loginManager, err := locallogin.NewManager(
		database, credentials, auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}),
		locallogin.DefaultConfig(bytes.Repeat([]byte{0x26}, 32)),
		locallogin.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := localtransportauth.New(loginManager, recorder)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{
		Login: loginManager, Verifier: verifier, Members: memberAdmin, ProjectRoles: projectAdmin,
		Clock: func() time.Time { return now }, Random: bytes.NewReader(bytes.Repeat([]byte{0x6a}, 4096)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := handler.Register(mux); err != nil {
		t.Fatal(err)
	}
	return &bffFixture{repository: repository, database: database, mux: mux, login: loginManager, credentials: credentials, now: now}
}

func (fixture *bffFixture) request(t *testing.T, method, path, body string, headers map[string]string, defaultOrigin bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://example.test"+path, strings.NewReader(body))
	request.Host = "example.test"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if defaultOrigin {
		request.Header.Set("Origin", "https://example.test")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	fixture.mux.ServeHTTP(response, request)
	return response
}

func (fixture *bffFixture) loginCookies(t *testing.T, userID, password string) (string, string) {
	t.Helper()
	response := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"`+userID+`","password":"`+password+`"}`, nil, true)
	if response.Code != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", userID, response.Code, response.Body.String())
	}
	values := map[string]string{}
	for _, cookie := range response.Result().Cookies() {
		values[cookie.Name] = cookie.Value
	}
	if values[SessionCookieName] == "" || values[CSRFCookieName] == "" {
		t.Fatalf("login %s cookies=%v", userID, response.Header().Values("Set-Cookie"))
	}
	return SessionCookieName + "=" + values[SessionCookieName] + "; " + CSRFCookieName + "=" + values[CSRFCookieName], values[CSRFCookieName]
}

func parseCookieHeader(header string) map[string]string {
	result := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			result[name] = value
		}
	}
	return result
}
