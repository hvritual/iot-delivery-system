package localtransportauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestYU25CredentialRevisionInvalidationIsIdenticalAcrossHTTPGRPCAndMCPSession(t *testing.T) {
	fixture := newTransportFixture(t)
	credentials, err := localcredential.NewSQLiteRepository(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("YU25-rotated-password"), 1); err != nil {
		t.Fatal(err)
	}
	assertYU25OldCredentialRejectedAcrossTransports(t, fixture)
}

func TestYU25DisabledUserInvalidationIsIdenticalAcrossHTTPGRPCAndMCPSession(t *testing.T) {
	fixture := newTransportFixture(t)
	if _, err := fixture.database.Exec(`UPDATE users SET status = 'disabled', revision = revision + 1 WHERE organization_id = 'org-a' AND id = 'user-a' AND status = 'active'`); err != nil {
		t.Fatal(err)
	}
	assertYU25OldCredentialRejectedAcrossTransports(t, fixture)
}

func assertYU25OldCredentialRejectedAcrossTransports(t *testing.T, fixture *transportFixture) {
	t.Helper()
	httpInvoked := false
	handler := fixture.verifier.HTTPMiddleware(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		httpInvoked = true
	}))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/api/items", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.result.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || httpInvoked {
		t.Fatalf("HTTP stale credential status=%d invoked=%v", response.Code, httpInvoked)
	}

	grpcInvoked := false
	interceptor := fixture.verifier.GRPCUnaryServerInterceptor(nil)
	ctx := grpcmetadata.NewIncomingContext(t.Context(), grpcmetadata.Pairs(GRPCAuthorizationMetadata, "Bearer "+fixture.result.AccessToken))
	_, err := interceptor(ctx, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, func(context.Context, any) (any, error) {
		grpcInvoked = true
		return "unexpected", nil
	})
	if status.Code(err) != codes.Unauthenticated || grpcInvoked {
		t.Fatalf("gRPC stale credential error=%v invoked=%v", err, grpcInvoked)
	}

	// The stdio MCP resolver calls this exact opaque-session verifier for every
	// tool invocation; therefore this is the MCP credential verdict, not a
	// separate cached authorization snapshot.
	if principal, err := fixture.verifier.VerifySessionToken(t.Context(), fixture.result.SessionToken); err == nil || principal.Authenticated {
		t.Fatalf("MCP opaque-session stale credential principal=%#v error=%v", principal, err)
	}

	var rejected int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries
WHERE operation = 'authentication.local_access_token'
  AND reason_code = 'authentication.invalid_credential'
  AND result = 'failure'`).Scan(&rejected); err != nil || rejected != 2 {
		t.Fatalf("generic invalid-credential audit count=%d error=%v", rejected, err)
	}
	var leaked int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries
WHERE instr(COALESCE(metadata, '') || COALESCE(diff_summary, '') || COALESCE(reason_code, ''), ?) > 0`, fixture.result.AccessToken).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("stale access token leaked into audit count=%d error=%v", leaked, err)
	}
}
