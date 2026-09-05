package localbffhttp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	repository *delivery.SQLiteRepository
	database   *sql.DB
	handler    *Handler
	mux        *http.ServeMux
	login      *locallogin.Manager
	credentials *localcredential.SQLiteRepository
	now        time.Time
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
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localmemberadmin.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localprojectroleadmin.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := locallogin.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
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
		ID: "project-a", OrganizationID: "org-a", Name: "Project A", Board: delivery.BoardResearchDelivery,
		Owner: "admin-a", CreatedAt: now, UpdatedAt: now,
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
	return &bffFixture{repository: repository, database: database, handler: handler, mux: mux, login: loginManager, credentials: credentials, now: now}
}

func TestYU26LocalLoginCurrentLogoutCookieAndCSRFContract(t *testing.T) {
	fixture := newBFFFixture(t)
	response := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"admin-a","password":"YU26-admin-password"}`, nil, true)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("login cache-control=%q", response.Header().Get("Cache-Control"))
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("login cookie count=%d headers=%v", len(cookies), response.Header().Values("Set-Cookie"))
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case SessionCookieName:
			sessionCookie = cookie
		case CSRFCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.Secure || !sessionCookie.HttpOnly || sessionCookie.Path != "/" || sessionCookie.Domain != "" || sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.MaxAge <= 0 {
		t.Fatalf("session cookie=%#v", sessionCookie)
	}
	if csrfCookie == nil || !csrfCookie.Secure || csrfCookie.HttpOnly || csrfCookie.Path != "/" || csrfCookie.Domain != "" || csrfCookie.SameSite != http.SameSiteStrictMode || csrfCookie.MaxAge <= 0 {
		t.Fatalf("csrf cookie=%#v", csrfCookie)
	}
	var loginBody map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &loginBody); err != nil {
		t.Fatal(err)
	}
	if loginBody["accessToken"] == "" || loginBody["csrfToken"] != csrfCookie.Value {
		t.Fatalf("login response=%#v", loginBody)
	}
	if _, leaked := loginBody["sessionToken"]; leaked {
		t.Fatalf("opaque session bearer leaked into JSON: %#v", loginBody)
	}

	cookieHeader := cookiePair(sessionCookie, csrfCookie)
	current := fixture.request(t, http.MethodGet, "/auth/local/current", "", map[string]string{"Cookie": cookieHeader}, false)
	if current.Code != http.StatusOK {
		t.Fatalf("current status=%d body=%s", current.Code, current.Body.String())
	}
	var currentBody map[string]any
	if err := json.Unmarshal(current.Body.Bytes(), &currentBody); err != nil {
		t.Fatal(err)
	}
	if currentBody["userId"] != "admin-a" || currentBody["organizationId"] != "org-a" || currentBody["accessToken"] == "" || currentBody["csrfToken"] != csrfCookie.Value {
		t.Fatalf("current response=%#v", currentBody)
	}

	missingCSRF := fixture.request(t, http.MethodPost, "/auth/local/logout", `{}`, map[string]string{"Cookie": cookieHeader}, true)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("logout without csrf status=%d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}
	if _, err := fixture.login.VerifySessionToken(t.Context(), sessionCookie.Value); err != nil {
		t.Fatalf("failed CSRF must not revoke session: %v", err)
	}

	wrongOrigin := fixture.request(t, http.MethodPost, "/auth/local/logout", `{}`, map[string]string{"Cookie": cookieHeader, CSRFHeader: csrfCookie.Value, "Origin": "https://evil.example"}, false)
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("logout wrong origin status=%d body=%s", wrongOrigin.Code, wrongOrigin.Body.String())
	}
	if _, err := fixture.login.VerifySessionToken(t.Context(), sessionCookie.Value); err != nil {
		t.Fatalf("failed Origin must not revoke session: %v", err)
	}

	logout := fixture.request(t, http.MethodPost, "/auth/local/logout", `{}`, map[string]string{"Cookie": cookieHeader, CSRFHeader: csrfCookie.Value}, true)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	if _, err := fixture.login.VerifySessionToken(t.Context(), sessionCookie.Value); !errors.Is(err, locallogin.ErrSessionInvalid) {
		t.Fatalf("logged out session error=%v", err)
	}
	if len(logout.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("logout did not clear both cookies: %v", logout.Header().Values("Set-Cookie"))
	}
}

func TestYU26AdminRoutesUseDurableManagerGuardCASAuditAndOutbox(t *testing.T) {
	fixture := newBFFFixture(t)
	adminCookie, csrf := fixture.loginCookies(t, "admin-a", "YU26-admin-password")
	headers := map[string]string{"Cookie": adminCookie, CSRFHeader: csrf}
	create := fixture.request(t, http.MethodPost, "/auth/local/admin/members", `{"displayName":"Managed User","email":"managed@example.test","password":"YU26-managed-password"}`, headers, true)
	if create.Code != http.StatusCreated {
		t.Fatalf("create member status=%d body=%s", create.Code, create.Body.String())
	}
	var created localmemberadmin.MemberResult
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.UserID == "" || created.OrganizationID != "org-a" || created.UserRevision != 1 || created.CredentialRevision != 1 {
		t.Fatalf("created member=%#v", created)
	}
	var userStatus string
	if err := fixture.database.QueryRow(`SELECT status FROM users WHERE organization_id = 'org-a' AND id = ?`, created.UserID).Scan(&userStatus); err != nil || userStatus != "active" {
		t.Fatalf("created user status=%q error=%v", userStatus, err)
	}

	assign := fixture.request(t, http.MethodPost, "/auth/local/admin/project-role-bindings", `{"projectId":"project-a","userId":"`+created.UserID+`","roleId":"contributor"}`, headers, true)
	if assign.Code != http.StatusCreated {
		t.Fatalf("assign role status=%d body=%s", assign.Code, assign.Body.String())
	}
	var binding localprojectroleadmin.BindingResult
	if err := json.Unmarshal(assign.Body.Bytes(), &binding); err != nil {
		t.Fatal(err)
	}
	if binding.BindingID == "" || binding.ProjectID != "project-a" || binding.UserID != created.UserID || binding.RoleID != "contributor" || binding.Revision != 1 {
		t.Fatalf("binding=%#v", binding)
	}
	revoke := fixture.request(t, http.MethodPost, "/auth/local/admin/project-role-bindings/"+binding.BindingID+"/revoke", `{"expectedRevision":1}`, headers, true)
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke role status=%d body=%s", revoke.Code, revoke.Body.String())
	}
	var bindingStatus string
	var bindingRevision int64
	if err := fixture.database.QueryRow(`SELECT status, revision FROM role_bindings WHERE id = ?`, binding.BindingID).Scan(&bindingStatus, &bindingRevision); err != nil || bindingStatus != "disabled" || bindingRevision != 2 {
		t.Fatalf("revoked binding status=%s revision=%d error=%v", bindingStatus, bindingRevision, err)
	}

	reset := fixture.request(t, http.MethodPost, "/auth/local/admin/members/"+created.UserID+"/reset-credential", `{"expectedUserRevision":1,"expectedCredentialRevision":1,"password":"YU26-reset-password"}`, headers, true)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset credential status=%d body=%s", reset.Code, reset.Body.String())
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", created.UserID, []byte("YU26-reset-password"))
	if err != nil || !verification.Match || verification.Revision != 2 {
		t.Fatalf("reset credential verification=%#v error=%v", verification, err)
	}
	disable := fixture.request(t, http.MethodPost, "/auth/local/admin/members/"+created.UserID+"/disable", `{"expectedRevision":2}`, headers, true)
	if disable.Code != http.StatusOK {
		t.Fatalf("disable member status=%d body=%s", disable.Code, disable.Body.String())
	}
	if err := fixture.database.QueryRow(`SELECT status FROM users WHERE organization_id = 'org-a' AND id = ?`, created.UserID).Scan(&userStatus); err != nil || userStatus != "disabled" {
		t.Fatalf("disabled user status=%q error=%v", userStatus, err)
	}

	var outboxCount, auditCount int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&outboxCount); err != nil || outboxCount < 4 {
		t.Fatalf("outbox count=%d error=%v", outboxCount, err)
	}
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE actor_id = 'admin-a' AND result = 'success'`).Scan(&auditCount); err != nil || auditCount < 4 {
		t.Fatalf("admin success audit count=%d error=%v", auditCount, err)
	}

	userCookie, userCSRF := fixture.loginCookies(t, "user-a", "YU26-user-password")
	forged := fixture.request(t, http.MethodPost, "/auth/local/admin/members", `{"displayName":"Must Fail","password":"YU26-must-fail"}`, map[string]string{"Cookie": userCookie, CSRFHeader: userCSRF}, true)
	if forged.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status=%d body=%s", forged.Code, forged.Body.String())
	}
}

func TestYU26ChangePasswordUsesVerifiedSessionFactsAndClearsSession(t *testing.T) {
	fixture := newBFFFixture(t)
	cookieHeader, csrf := fixture.loginCookies(t, "user-a", "YU26-user-password")
	response := fixture.request(t, http.MethodPost, "/auth/local/change-password", `{"currentPassword":"YU26-user-password","newPassword":"YU26-user-password-new"}`, map[string]string{"Cookie": cookieHeader, CSRFHeader: csrf}, true)
	if response.Code != http.StatusOK {
		t.Fatalf("change password status=%d body=%s", response.Code, response.Body.String())
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU26-user-password-new"))
	if err != nil || !verification.Match || verification.Revision != 2 {
		t.Fatalf("new password verification=%#v error=%v", verification, err)
	}
	cookies := parseCookieHeader(cookieHeader)
	if _, err := fixture.login.VerifySessionToken(t.Context(), cookies[SessionCookieName]); !errors.Is(err, locallogin.ErrSessionInvalid) {
		t.Fatalf("password-change session error=%v", err)
	}
	if len(response.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("password change did not clear cookies: %v", response.Header().Values("Set-Cookie"))
	}
}

func TestYU26LoginFailureIsStableAndDoesNotRequireOIDCConfiguration(t *testing.T) {
	fixture := newBFFFixture(t)
	wrong := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"admin-a","password":"wrong-password"}`, nil, true)
	missing := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"missing-user","password":"wrong-password"}`, nil, true)
	if wrong.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized || wrong.Body.String() != missing.Body.String() {
		t.Fatalf("login failure drift wrong=(%d,%s) missing=(%d,%s)", wrong.Code, wrong.Body.String(), missing.Code, missing.Body.String())
	}
	if strings.Contains(wrong.Body.String(), "admin-a") || strings.Contains(wrong.Body.String(), "password") {
		t.Fatalf("login error leaked credential detail: %s", wrong.Body.String())
	}
}

func (fixture *bffFixture) request(t *testing.T, method, path, body string, headers map[string]string, defaultOrigin bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, "https://example.test"+path, reader)
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
	cookies := response.Result().Cookies()
	values := map[string]string{}
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	if values[SessionCookieName] == "" || values[CSRFCookieName] == "" {
		t.Fatalf("login %s cookies=%#v", userID, cookies)
	}
	return SessionCookieName + "=" + values[SessionCookieName] + "; " + CSRFCookieName + "=" + values[CSRFCookieName], values[CSRFCookieName]
}

func cookiePair(cookies ...*http.Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
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
