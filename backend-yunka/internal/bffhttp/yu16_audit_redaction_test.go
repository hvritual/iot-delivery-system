package bffhttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffhttp"
)

func TestYU16BFFAcceptedAndRejectedAuditsNeverPersistRequestSecrets(t *testing.T) {
	fixture := newFixture(t)
	store, err := audit.NewSQLiteStore(fixture.database)
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatalf("create security recorder: %v", err)
	}
	middleware, err := bffhttp.NewMiddleware(bffhttp.Config{
		Authenticator:       fixture.authenticator,
		AuditRecorder:       recorder,
		Verifier:            fixture.verifier,
		Resolver:            fixture.resolver,
		OrganizationID:      "org-1",
		AllowLegacyFallback: true,
	})
	if err != nil {
		t.Fatalf("create BFF middleware: %v", err)
	}
	called := 0
	handler := middleware.HTTPMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called++
		writer.WriteHeader(http.StatusNoContent)
	}))

	body := []byte(`{"password":"YU16-password-secret","token":"YU16-token-secret","session":"YU16-session-secret","csrf":"YU16-csrf-secret"}`)
	accepted := fixture.signedRequestTo(t, http.MethodPost, "/yu16/audit-probe", "subject-yu16", "nonce-yu16-accepted", body)
	accepted.Header.Set("Authorization", "Bearer YU16-authorization-secret")
	accepted.Header.Set("Cookie", "session=YU16-session-secret")
	accepted.Header.Set("X-CSRF-Token", "YU16-csrf-secret")
	acceptedResponse := httptest.NewRecorder()
	handler.ServeHTTP(acceptedResponse, accepted)
	if acceptedResponse.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("accepted assertion = status=%d called=%d, want 204/1", acceptedResponse.Code, called)
	}
	var acceptedActorType, acceptedActorID, acceptedResult, acceptedReason string
	if err := fixture.database.QueryRow(`SELECT actor_type, COALESCE(actor_id, ''), result, reason_code FROM iotd_audit_entries WHERE operation = 'authentication.bff_assertion' AND result = 'success' ORDER BY sequence DESC LIMIT 1`).Scan(&acceptedActorType, &acceptedActorID, &acceptedResult, &acceptedReason); err != nil {
		t.Fatalf("read accepted assertion audit: %v", err)
	}
	if acceptedActorType != "human" || acceptedActorID == "" || acceptedResult != "success" || acceptedReason != "authentication.assertion_accepted" {
		t.Fatalf("accepted assertion audit = actor=%q/%q result=%q reason=%q", acceptedActorType, acceptedActorID, acceptedResult, acceptedReason)
	}
	yu16AssertNoAuditSecrets(t, fixture)

	rejected := fixture.signedRequestTo(t, http.MethodPost, "/yu16/audit-probe", "subject-yu16", "nonce-yu16-rejected", body)
	rejected.Header.Set(bffassertion.SignatureHeader, "YU16-signature-secret")
	rejected.Header.Set("Authorization", "Bearer YU16-authorization-secret")
	rejected.Header.Set("Cookie", "session=YU16-session-secret")
	rejected.Header.Set("X-CSRF-Token", "YU16-csrf-secret")
	rejectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(rejectedResponse, rejected)
	if rejectedResponse.Code != http.StatusUnauthorized || called != 1 {
		t.Fatalf("rejected assertion = status=%d called=%d, want 401/1", rejectedResponse.Code, called)
	}
	var failureActorType, failureActorID, failureDecision, failureResult, failureReason string
	if err := fixture.database.QueryRow(`SELECT actor_type, COALESCE(actor_id, ''), authorization_decision, result, reason_code FROM iotd_audit_entries WHERE operation = 'authentication.bff_assertion' AND result = 'failure' ORDER BY sequence DESC LIMIT 1`).Scan(&failureActorType, &failureActorID, &failureDecision, &failureResult, &failureReason); err != nil {
		t.Fatalf("read rejected assertion audit: %v", err)
	}
	if failureActorType != "anonymous" || failureActorID != "" || failureDecision != "not_evaluated" || failureResult != "failure" || failureReason != "authentication.assertion_rejected" {
		t.Fatalf("rejected assertion audit = actor=%q/%q decision=%q result=%q reason=%q", failureActorType, failureActorID, failureDecision, failureResult, failureReason)
	}
	yu16AssertNoAuditSecrets(t, fixture)
}

func yu16AssertNoAuditSecrets(t *testing.T, fixture fixture) {
	t.Helper()
	rows, err := fixture.database.Query(`SELECT
COALESCE(id, ''), COALESCE(organization_id, ''), COALESCE(project_id, ''), COALESCE(actor_id, ''),
COALESCE(operation, ''), COALESCE(scope_id, ''), COALESCE(target_type, ''), COALESCE(target_id, ''),
COALESCE(reason_code, ''), COALESCE(trace_id, ''), COALESCE(request_id, ''), COALESCE(correlation_id, ''),
COALESCE(diff_summary, ''), COALESCE(metadata, '')
FROM iotd_audit_entries`)
	if err != nil {
		t.Fatalf("read audit text surface: %v", err)
	}
	defer rows.Close()
	var persisted strings.Builder
	for rows.Next() {
		columns := make([]string, 14)
		destinations := make([]any, len(columns))
		for index := range columns {
			destinations[index] = &columns[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatalf("scan audit text surface: %v", err)
		}
		persisted.WriteString(strings.Join(columns, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit text surface: %v", err)
	}
	text := persisted.String()
	for _, sentinel := range []string{
		"YU16-password-secret",
		"YU16-token-secret",
		"YU16-session-secret",
		"YU16-csrf-secret",
		"YU16-authorization-secret",
		"YU16-signature-secret",
	} {
		if strings.Contains(text, sentinel) {
			t.Fatalf("audit persisted sensitive sentinel %q in %q", sentinel, text)
		}
	}
}
