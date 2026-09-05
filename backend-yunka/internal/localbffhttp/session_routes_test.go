package localbffhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
)

func TestYU26LocalLoginCurrentLogoutCookieAndCSRFContract(t *testing.T) {
	fixture := newBFFFixture(t)
	response := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"admin-a","password":"YU26-admin-password"}`, nil, true)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("login cache headers=%v", response.Header())
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

	cookieHeader := SessionCookieName + "=" + sessionCookie.Value + "; " + CSRFCookieName + "=" + csrfCookie.Value
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
	wrongOrigin := fixture.request(t, http.MethodPost, "/auth/local/logout", `{}`, map[string]string{
		"Cookie": cookieHeader, CSRFHeader: csrfCookie.Value, "Origin": "https://evil.example",
	}, false)
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

func TestYU26LoginFailuresAreStableAndDoNotDependOnOIDC(t *testing.T) {
	fixture := newBFFFixture(t)
	wrong := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"admin-a","password":"wrong-password"}`, nil, true)
	missing := fixture.request(t, http.MethodPost, "/auth/local/login", `{"organizationId":"org-a","userId":"missing-user","password":"wrong-password"}`, nil, true)
	if wrong.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized {
		t.Fatalf("login failure status wrong=%d missing=%d", wrong.Code, missing.Code)
	}
	for label, response := range map[string]*httptest.ResponseRecorder{"wrong": wrong, "missing": missing} {
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s error JSON: %v", label, err)
		}
		if body["error"] != "unauthenticated" || body["traceId"] == "" {
			t.Fatalf("%s stable error=%#v", label, body)
		}
		if strings.Contains(response.Body.String(), "admin-a") || strings.Contains(response.Body.String(), "password") {
			t.Fatalf("%s login error leaked credential detail: %s", label, response.Body.String())
		}
	}
}
