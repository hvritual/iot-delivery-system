package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"yunka.io/gateway/authz"
)

func TestWriteErrorUsesStableAuthorizationCategory(t *testing.T) {
	for name, err := range map[string]error{
		"unauthenticated":   authz.Denied(authz.Decision{Reason: authz.ReasonUnauthenticated}),
		"permission denied": authz.Denied(authz.Decision{Reason: authz.ReasonPermissionDenied}),
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeError(recorder, err)
			wantStatus := http.StatusForbidden
			wantCategory := "permission_denied"
			if name == "unauthenticated" {
				wantStatus = http.StatusUnauthorized
				wantCategory = "unauthenticated"
			}
			if recorder.Code != wantStatus {
				t.Fatalf("authorization response status = %d, want %d: %s", recorder.Code, wantStatus, recorder.Body.String())
			}
			var payload map[string]string
			if err := json.NewDecoder(recorder.Body).Decode(&payload); err != nil {
				t.Fatalf("decode authorization response: %v", err)
			}
			if payload["error"] != wantCategory {
				t.Fatalf("authorization response = %#v, want error %q", payload, wantCategory)
			}
		})
	}
}
