package localtransportauth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	_ "modernc.org/sqlite"
)

type transportFixture struct {
	database *sql.DB
	login    *locallogin.Manager
	verifier *Verifier
	result   locallogin.LoginResult
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "yu23-transport.db"))
	if err != nil { t.Fatal(err) }
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil { t.Fatal(err) }
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil { t.Fatal(err) }
	if err := localmemberadmin.ApplyMigrations(t.Context(), database); err != nil { t.Fatal(err) }
	if err := locallogin.ApplyMigrations(t.Context(), database); err != nil { t.Fatal(err) }
	if err := audit.ApplyMigrations(t.Context(), database); err != nil { t.Fatal(err) }
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Organization A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('user-a', 'org-a', 'User A', 'active', 1)`,
	} {
		if _, err := database.Exec(statement); err != nil { t.Fatal(err) }
	}
	now := time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC)
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithClock(func() time.Time { return now }))
	if err != nil { t.Fatal(err) }
	if _, err := credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("YU23-password-secret"), 0); err != nil { t.Fatal(err) }
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(func() time.Time { return now }))
	if err != nil { t.Fatal(err) }
	recorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil { t.Fatal(err) }
	manager, err := locallogin.NewManager(database, credentials, auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}),
		locallogin.DefaultConfig(bytes.Repeat([]byte{0x23}, 32)), locallogin.WithClock(func() time.Time { return now }))
	if err != nil { t.Fatal(err) }
	result, err := manager.Login(t.Context(), locallogin.LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU23-password-secret")})
	if err != nil { t.Fatal(err) }
	verifier, err := New(manager, recorder)
	if err != nil { t.Fatal(err) }
	return &transportFixture{database: database, login: manager, verifier: verifier, result: result}
}

func TestYU23HTTPRevalidatesLocalJWTAndRejectsMixedCredentials(t *testing.T) {
	fixture := newTransportFixture(t)
	invoked := 0
	var got identity.Principal
	handler := fixture.verifier.HTTPMiddleware(nil)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		invoked++
		got, _ = identity.FromContext(request.Context())
		writer.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/api/items", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.result.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || invoked != 1 || !got.Authenticated || got.AuthMethod != identity.AuthMethodJWT || got.TenantID != "org-a" || got.UserID != "user-a" || len(got.Roles) != 0 {
		t.Fatalf("HTTP local principal=%#v status=%d invoked=%d", got, response.Code, invoked)
	}
	mixed := httptest.NewRequest(http.MethodGet, "http://example.test/api/items", nil)
	mixed.Header.Set("Authorization", "Bearer "+fixture.result.AccessToken)
	mixed.Header.Set(localauth.APIKeyHeader, "legacy-must-not-win")
	mixedResponse := httptest.NewRecorder()
	handler.ServeHTTP(mixedResponse, mixed)
	if mixedResponse.Code != http.StatusUnauthorized || invoked != 1 {
		t.Fatalf("mixed HTTP credentials status=%d invoked=%d", mixedResponse.Code, invoked)
	}
	if _, err := fixture.login.Logout(t.Context(), locallogin.LogoutInput{SessionToken: fixture.result.SessionToken, ExpectedSessionRevision: 1}); err != nil { t.Fatal(err) }
	revoked := httptest.NewRequest(http.MethodGet, "http://example.test/api/items", nil)
	revoked.Header.Set("Authorization", "Bearer "+fixture.result.AccessToken)
	revokedResponse := httptest.NewRecorder()
	handler.ServeHTTP(revokedResponse, revoked)
	if revokedResponse.Code != http.StatusUnauthorized || invoked != 1 {
		t.Fatalf("revoked HTTP token status=%d invoked=%d", revokedResponse.Code, invoked)
	}
}

func TestYU23GRPCRevalidatesLocalJWTAndRejectsMixedCredentials(t *testing.T) {
	fixture := newTransportFixture(t)
	invoked := 0
	var got identity.Principal
	interceptor := fixture.verifier.GRPCUnaryServerInterceptor(nil)
	handler := func(ctx context.Context, _ any) (any, error) {
		invoked++
		got, _ = identity.FromContext(ctx)
		return "ok", nil
	}
	ctx := grpcmetadata.NewIncomingContext(t.Context(), grpcmetadata.Pairs(GRPCAuthorizationMetadata, "Bearer "+fixture.result.AccessToken))
	if _, err := interceptor(ctx, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, handler); err != nil {
		t.Fatal(err)
	}
	if invoked != 1 || !got.Authenticated || got.UserID != "user-a" || got.TenantID != "org-a" || len(got.Roles) != 0 {
		t.Fatalf("gRPC local principal=%#v invoked=%d", got, invoked)
	}
	mixed := grpcmetadata.NewIncomingContext(t.Context(), grpcmetadata.Pairs(GRPCAuthorizationMetadata, "Bearer "+fixture.result.AccessToken, strings.ToLower(localauth.APIKeyHeader), "legacy-must-not-win"))
	if _, err := interceptor(mixed, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, handler); status.Code(err) != codes.Unauthenticated || invoked != 1 {
		t.Fatalf("mixed gRPC error=%v invoked=%d", err, invoked)
	}
	if _, err := fixture.login.Logout(t.Context(), locallogin.LogoutInput{SessionToken: fixture.result.SessionToken, ExpectedSessionRevision: 1}); err != nil { t.Fatal(err) }
	if _, err := interceptor(ctx, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, handler); status.Code(err) != codes.Unauthenticated || invoked != 1 {
		t.Fatalf("revoked gRPC error=%v invoked=%d", err, invoked)
	}
}

func TestYU23OpaqueSessionVerifierReReadsRevocationAndNeverAddsRoles(t *testing.T) {
	fixture := newTransportFixture(t)
	principal, err := fixture.verifier.VerifySessionToken(t.Context(), fixture.result.SessionToken)
	if err != nil || !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || principal.UserID != "user-a" || principal.TenantID != "org-a" || len(principal.Roles) != 0 {
		t.Fatalf("session principal=%#v error=%v", principal, err)
	}
	if _, err := fixture.login.Logout(t.Context(), locallogin.LogoutInput{SessionToken: fixture.result.SessionToken, ExpectedSessionRevision: 1}); err != nil { t.Fatal(err) }
	if principal, err := fixture.verifier.VerifySessionToken(t.Context(), fixture.result.SessionToken); !errors.Is(err, ErrUnauthenticated) || principal.Authenticated {
		t.Fatalf("revoked session principal=%#v error=%v", principal, err)
	}
}
