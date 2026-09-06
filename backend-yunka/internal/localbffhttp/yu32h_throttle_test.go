package localbffhttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYU32HHTTPThrottleIgnoresForwardedHeaders(t *testing.T) {
	f := newBFFFixture(t)
	for i := 0; i < 11; i++ {
		r := httptest.NewRequest(http.MethodPost, "https://example.test/auth/local/login", strings.NewReader(`{"organizationId":"org-a","userId":"user-a","password":"wrong-password"}`))
		r.RemoteAddr = fmt.Sprintf("192.0.2.8:%d", 10000+i)
		r.Header.Set("Origin", "https://example.test")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		r.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i+1))
		r.Header.Set("Forwarded", fmt.Sprintf("for=198.51.100.%d", i+1))
		w := httptest.NewRecorder()
		f.mux.ServeHTTP(w, r)
		if i < 10 && w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
		if i == 10 {
			if w.Code != 429 || w.Header().Get("Retry-After") != "900" {
				t.Fatalf("throttle contract: %d %s", w.Code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal("throttled response exposed cookies or caches")
			}
			for _, secret := range []string{"user-a", "wrong-password", "192.0.2.8", "accessToken"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("throttle response leaked identity/secret")
				}
			}
		}
	}
	var count int
	if err := f.database.QueryRow(`SELECT count(*) FROM iotd_local_password_attempts`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("forwarded/port rotation created new source buckets: %d %v", count, err)
	}
}

func TestYU32HWeakPasswordChangeIs400AndLeavesSessionValid(t *testing.T) {
	f := newBFFFixture(t)
	cookies, csrf := f.loginCookies(t, "user-a", "YU26-user-password")
	w := f.request(t, http.MethodPost, "/auth/local/change-password", `{"currentPassword":"YU26-user-password","newPassword":"weak"}`, map[string]string{"Cookie": cookies, CSRFHeader: csrf}, true)
	if w.Code != 400 {
		t.Fatalf("weak password status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := f.login.VerifySessionToken(t.Context(), parseCookieHeader(cookies)[SessionCookieName]); err != nil {
		t.Fatal("rejected password invalidated session", err)
	}
	metadata, err := f.credentials.Metadata(t.Context(), "org-a", "user-a")
	if err != nil || metadata.Revision != 1 {
		t.Fatal("weak password changed credential revision")
	}
}
