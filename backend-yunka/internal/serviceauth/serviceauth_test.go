package serviceauth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	yunkagrpc "github.com/hvritual/yunka.io/gateway/rpc/transport/grpc"

	_ "modernc.org/sqlite"
)

func TestIssueAndAuthenticateCreatesServicePrincipalWithoutPersistingCredential(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})

	issued, err := manager.Issue(context.Background(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential: %v", err)
	}
	if issued.CredentialID != "credential-a" || !strings.HasPrefix(issued.Credential, CredentialPrefix) {
		t.Fatalf("issued credential metadata = %#v, want credential identifier and service-token prefix", issued)
	}
	principal, err := manager.Authenticate(context.Background(), issued.Credential)
	if err != nil {
		t.Fatalf("authenticate issued service credential: %v", err)
	}
	if principal.Subject != "service-account/service-a" || principal.TenantID != "org-a" || principal.UserID != "" || principal.AuthMethod != identity.AuthMethodServiceToken || !principal.Authenticated || len(principal.Roles) != 0 {
		t.Fatalf("service principal = %#v, want distinguishable non-human service principal", principal)
	}
	var storedHash []byte
	if err := database.QueryRow(`SELECT credential_hash FROM service_account_credentials WHERE id = ?`, issued.CredentialID).Scan(&storedHash); err != nil {
		t.Fatalf("read stored credential digest: %v", err)
	}
	if len(storedHash) != 32 || strings.Contains(string(storedHash), issued.Credential) {
		t.Fatal("persistent service credential must be a SHA-256 digest, not the issued credential")
	}
}

func TestCredentialRevocationCommitsWithItsAuditEntry(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	createServiceAccount(t, database, "org-a", "service-a")
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	manager, err := NewManager(database, Config{
		Now:           func() time.Time { return now },
		NewID:         func() (string, error) { return "credential-revoke-a", nil },
		NewSecret:     func() ([]byte, error) { return []byte("test-service-secret-alpha-000000000000"), nil },
		AuditRecorder: recorder,
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a"})
	ctx = runtimecontext.WithTraceID(ctx, "credential-revoke-trace")
	ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Attributes: map[string]string{"correlation_id": "credential-correlation-a", "authorization": "secret"}})
	if err := manager.Revoke(ctx, issued.CredentialID); err != nil {
		t.Fatalf("revoke credential: %v", err)
	}
	if _, err := manager.Authenticate(t.Context(), issued.Credential); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked credential authentication error = %v, want ErrUnauthorized", err)
	}
	var revokedAt sql.NullString
	if err := database.QueryRow(`SELECT revoked_at FROM service_account_credentials WHERE id = ?`, issued.CredentialID).Scan(&revokedAt); err != nil || !revokedAt.Valid {
		t.Fatalf("credential revocation = %q error=%v, want committed revocation", revokedAt.String, err)
	}
	var category, targetType, targetID, result, correlationID, metadata string
	if err := database.QueryRow(`SELECT event_category, target_type, target_id, result, correlation_id, metadata FROM iotd_audit_entries`).Scan(&category, &targetType, &targetID, &result, &correlationID, &metadata); err != nil {
		t.Fatalf("read credential revocation audit: %v", err)
	}
	if category != "configuration" || targetType != "service.credential" || targetID != issued.CredentialID || result != "success" {
		t.Fatalf("credential revocation audit = category=%q target=%q/%q result=%q", category, targetType, targetID, result)
	}
	if correlationID != "credential-correlation-a" || strings.Contains(metadata, "secret") {
		t.Fatalf("credential revocation correlation/metadata = %q/%s", correlationID, metadata)
	}
}

func TestCredentialRevocationRollsBackWhenAuditAppendFails(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	createServiceAccount(t, database, "org-a", "service-a")
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	manager, err := NewManager(database, Config{Now: func() time.Time { return now }, NewID: func() (string, error) { return "credential-fault-a", nil }, NewSecret: func() ([]byte, error) { return []byte("test-service-secret-alpha-000000000000"), nil }, AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_credential_audit BEFORE INSERT ON iotd_audit_entries BEGIN SELECT RAISE(ABORT, 'audit fault'); END`); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a"})
	if err := manager.Revoke(ctx, issued.CredentialID); err == nil {
		t.Fatal("revoke succeeded despite audit insertion fault")
	}
	if _, err := manager.Authenticate(t.Context(), issued.Credential); err != nil {
		t.Fatalf("credential was revoked despite audit fault: %v", err)
	}
	var revokedAt sql.NullString
	if err := database.QueryRow(`SELECT revoked_at FROM service_account_credentials WHERE id = ?`, issued.CredentialID).Scan(&revokedAt); err != nil || revokedAt.Valid {
		t.Fatalf("revocation state = %q error=%v, want active", revokedAt.String, err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("audit rows = %d error=%v, want 0", count, err)
	}
}

func TestCredentialRevocationRejectsDifferentTargetOrganization(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	createServiceAccount(t, database, "org-a", "service-a")
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manager, err := NewManager(database, Config{Now: func() time.Time { return now }, NewID: func() (string, error) { return "credential-cross-org", nil }, NewSecret: func() ([]byte, error) { return []byte("test-service-secret-alpha-000000000000"), nil }, AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-b"})
	if err := manager.Revoke(ctx, issued.CredentialID); err == nil {
		t.Fatal("cross-organization revocation succeeded")
	}
	if _, err := manager.Authenticate(t.Context(), issued.Credential); err != nil {
		t.Fatalf("cross-organization revocation changed credential: %v", err)
	}
}

func TestAuthenticateFailsClosedForExpiredRevokedDisabledAndMalformedCredentials(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a", "credential-b", "credential-c"}, [][]byte{
		[]byte("test-service-secret-alpha-000000000000"),
		[]byte("test-service-secret-bravo-000000000000"),
		[]byte("test-service-secret-charlie-000000000"),
	})

	expired, err := manager.Issue(context.Background(), "service-a", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("issue expired test credential: %v", err)
	}
	now = now.Add(time.Hour)
	revoked, err := manager.Issue(context.Background(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue revocable test credential: %v", err)
	}
	if err := manager.Revoke(context.Background(), revoked.CredentialID); err != nil {
		t.Fatalf("revoke service credential: %v", err)
	}
	disabled, err := manager.Issue(context.Background(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue disable test credential: %v", err)
	}
	if err := manager.DisableServiceAccount(context.Background(), "service-a"); err != nil {
		t.Fatalf("disable service account: %v", err)
	}

	for name, credential := range map[string]string{
		"expired":   expired.Credential,
		"revoked":   revoked.Credential,
		"disabled":  disabled.Credential,
		"malformed": "not-a-service-credential",
		"unknown":   CredentialPrefix + "credential-unknown.test-service-secret-does-not-exist",
	} {
		t.Run(name, func(t *testing.T) {
			if _, authenticateErr := manager.Authenticate(context.Background(), credential); authenticateErr != ErrUnauthorized {
				t.Fatalf("authenticate rejected credential error = %v, want ErrUnauthorized", authenticateErr)
			}
		})
	}
}

func TestRotateAtomicallyRevokesOldCredentialBeforeReturningNewCredential(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-old", "credential-new"}, [][]byte{
		[]byte("test-service-secret-old-00000000000000"),
		[]byte("test-service-secret-new-00000000000000"),
	})
	oldCredential, err := manager.Issue(context.Background(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue old service credential: %v", err)
	}
	rotated, err := manager.Rotate(context.Background(), "service-a", oldCredential.Credential, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("rotate service credential: %v", err)
	}
	if _, authenticateErr := manager.Authenticate(context.Background(), oldCredential.Credential); authenticateErr != ErrUnauthorized {
		t.Fatalf("old credential authentication error = %v, want ErrUnauthorized", authenticateErr)
	}
	if _, authenticateErr := manager.Authenticate(context.Background(), rotated.Credential); authenticateErr != nil {
		t.Fatalf("new credential must authenticate immediately: %v", authenticateErr)
	}
	var revokedAt sql.NullString
	if err := database.QueryRow(`SELECT revoked_at FROM service_account_credentials WHERE id = ?`, oldCredential.CredentialID).Scan(&revokedAt); err != nil || !revokedAt.Valid {
		t.Fatalf("old credential must be revoked atomically, value=%q error=%v", revokedAt.String, err)
	}
}

func TestGRPCServiceCredentialAdapterAttachesServicePrincipalAndTrace(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential: %v", err)
	}
	interceptor := manager.GRPCUnaryServerInterceptor(func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		t.Fatal("service credential must not use the legacy API-key fallback")
		return nil, nil
	})
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
		yunkagrpc.ServiceAuthorizationMetadata, "Bearer "+issued.Credential,
		"traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	))
	_, err = interceptor(ctx, "request", &grpc.UnaryServerInfo{FullMethod: "/delivery.v1.DeliveryService/ListItems"}, func(ctx context.Context, request any) (any, error) {
		principal, ok := identity.FromContext(ctx)
		if !ok || principal.Subject != "service-account/service-a" || principal.UserID != "" || principal.AuthMethod != identity.AuthMethodServiceToken {
			t.Fatalf("gRPC service principal = %#v, want non-human service principal", principal)
		}
		runtimeMetadata, metadataOK := runtimecontext.MetadataFrom(ctx)
		traceID := trace.SpanContextFromContext(ctx).TraceID().String()
		if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || !metadataOK || runtimeMetadata.Transport != "rpc" || runtimeMetadata.Protocol != "grpc" || runtimeMetadata.Operation != "/delivery.v1.DeliveryService/ListItems" || runtimeMetadata.Method != "/delivery.v1.DeliveryService/ListItems" || runtimeMetadata.Attributes["rpc.direction"] != "server" {
			t.Fatalf("gRPC standard trace context = trace=%q metadata=%#v", traceID, runtimeMetadata)
		}
		return request, nil
	})
	if err != nil {
		t.Fatalf("intercept valid service credential: %v", err)
	}
}

func TestGRPCServiceCredentialAdapterFailsClosedForDuplicateCredentialMetadata(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential: %v", err)
	}
	interceptor := manager.GRPCUnaryServerInterceptor(func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		t.Fatal("duplicate service credential must not use the legacy API-key fallback")
		return nil, nil
	})
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(yunkagrpc.ServiceAuthorizationMetadata, "Bearer "+issued.Credential, yunkagrpc.ServiceAuthorizationMetadata, "Bearer "+issued.Credential))
	_, err = interceptor(ctx, "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		t.Fatal("duplicate service credential must not invoke handler")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "unauthenticated" {
		t.Fatalf("duplicate service credential error = %v, want generic unauthenticated", err)
	}
}

func TestGRPCServiceCredentialMissingWithoutFallbackPersistsAnonymousFailure(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, Config{AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	interceptor := manager.GRPCUnaryServerInterceptor(nil)
	called := false
	_, err = interceptor(t.Context(), "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { called = true; return nil, nil })
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("missing service token = error=%v called=%t, want unauthenticated/no handler", err, called)
	}
	var actorType, result, reason string
	if err := database.QueryRow(`SELECT actor_type, result, reason_code FROM iotd_audit_entries`).Scan(&actorType, &result, &reason); err != nil {
		t.Fatal(err)
	}
	if actorType != "anonymous" || result != "failure" || reason != "authentication.missing_credential" {
		t.Fatalf("missing credential audit = %q/%q/%q", actorType, result, reason)
	}
}

func TestGRPCAPIFallbackUnauthenticatedPersistsOneAnonymousFailure(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, Config{AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	fallback := func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	_, err = manager.GRPCUnaryServerInterceptor(fallback)(t.Context(), "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { called = true; return nil, nil })
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "unauthenticated" || called {
		t.Fatalf("fallback unauthenticated = error=%v called=%t", err, called)
	}
	var count int
	var actorType, operation, reasonCode string
	if err := database.QueryRow(`SELECT COUNT(*), COALESCE(MAX(actor_type), ''), COALESCE(MAX(operation), ''), COALESCE(MAX(reason_code), '') FROM iotd_audit_entries`).Scan(&count, &actorType, &operation, &reasonCode); err != nil {
		t.Fatal(err)
	}
	if count != 1 || actorType != "anonymous" || operation != "authentication.development_api_key" || reasonCode != "authentication.invalid_credential" {
		t.Fatalf("fallback audit = count=%d actor=%q operation=%q reason=%q", count, actorType, operation, reasonCode)
	}
}

func TestGRPCAPIFallbackRecordsOnlyUnauthenticatedFailures(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, Config{AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	for _, fallback := range []grpc.UnaryServerInterceptor{
		func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
			return nil, status.Error(codes.Internal, "internal")
		},
		func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) { return "ok", nil },
	} {
		if _, err := manager.GRPCUnaryServerInterceptor(fallback)(t.Context(), "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { return nil, nil }); status.Code(err) == codes.Unauthenticated {
			t.Fatalf("unexpected unauthenticated fallback result: %v", err)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("non-auth/success fallback audit count = %d error=%v, want 0", count, err)
	}
}

func TestGRPCAPIFallbackDoesNotAuditUnauthenticatedHandlerFailure(t *testing.T) {
	database := migratedDatabase(t)
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, Config{AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	handlerCalled := false
	fallback := func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, request)
	}
	_, err = manager.GRPCUnaryServerInterceptor(fallback)(t.Context(), "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		handlerCalled = true
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	})
	if status.Code(err) != codes.Unauthenticated || !handlerCalled {
		t.Fatalf("handler unauthenticated = error=%v called=%t", err, handlerCalled)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("handler unauthenticated audit count = %d error=%v, want 0", count, err)
	}
}

func TestGRPCAPIFallbackAuditFailureKeepsUnauthenticated(t *testing.T) {
	database := migratedDatabase(t)
	recorder, err := audit.NewSecurityRecorder(unavailableAuditStore{})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(database, Config{AuditRecorder: recorder})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	fallback := func(context.Context, any, *grpc.UnaryServerInfo, grpc.UnaryHandler) (any, error) {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	_, err = manager.GRPCUnaryServerInterceptor(fallback)(t.Context(), "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { called = true; return nil, nil })
	if status.Code(err) != codes.Unauthenticated || status.Convert(err).Message() != "unauthenticated" || called {
		t.Fatalf("fallback audit fault = error=%v called=%t", err, called)
	}
}

type unavailableAuditStore struct{}

func (unavailableAuditStore) Append(context.Context, audit.Entry) (audit.Entry, error) {
	return audit.Entry{}, errors.New("audit unavailable")
}
func (unavailableAuditStore) ByID(context.Context, string) (audit.Entry, error) {
	return audit.Entry{}, audit.ErrNotFound
}

func TestVerifyRejectsInsecureTransportByDefault(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager, err := NewManager(database, Config{
		Now:       func() time.Time { return now },
		NewID:     func() (string, error) { return "credential-a", nil },
		NewSecret: func() ([]byte, error) { return []byte("test-service-secret-alpha-000000000000"), nil },
	})
	if err != nil {
		t.Fatalf("construct service credential manager: %v", err)
	}
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential: %v", err)
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(yunkagrpc.ServiceAuthorizationMetadata, "Bearer "+issued.Credential))
	if _, err := manager.Verify(ctx); !errors.Is(err, yunkagrpc.ErrServiceCredentialInsecureTransport) {
		t.Fatalf("insecure service verification error = %v, want ErrServiceCredentialInsecureTransport", err)
	}
}

func TestAuthenticateRejectsOversizedAndControlCharacterCredentialInput(t *testing.T) {
	database := migratedDatabase(t)
	createServiceAccount(t, database, "org-a", "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential: %v", err)
	}
	for name, credential := range map[string]string{
		"total token limit":    strings.Repeat("a", yunkagrpc.MaxServiceTokenBytes+1),
		"control character":    issued.Credential + "\n",
		"credential ID limit":  CredentialPrefix + strings.Repeat("a", maxCredentialIDBytes+1) + "." + strings.Repeat("A", 43),
		"encoded secret limit": CredentialPrefix + "credential-a." + strings.Repeat("A", maxEncodedCredentialSecret+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, authenticateErr := manager.Authenticate(t.Context(), credential); authenticateErr != ErrUnauthorized {
				t.Fatalf("credential bounds error = %v, want ErrUnauthorized", authenticateErr)
			}
		})
	}
}

func TestIssueRejectsServiceAccountIDThatCannotFitYunkaServicePrincipal(t *testing.T) {
	database := migratedDatabase(t)
	serviceAccountID := strings.Repeat("s", yunkagrpc.MaxServiceIdentityBytes-len("service-account/")+1)
	createServiceAccount(t, database, "org-a", serviceAccountID)
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
	if _, err := manager.Issue(t.Context(), serviceAccountID, now.Add(time.Hour)); err != ErrInvalidManagementRequest {
		t.Fatalf("oversized service account issuance error = %v, want ErrInvalidManagementRequest", err)
	}
}

func TestAuthenticateRejectsServiceRecordThatCannotFitYunkaIdentityBounds(t *testing.T) {
	database := migratedDatabase(t)
	organizationID := strings.Repeat("o", yunkagrpc.MaxServiceIdentityBytes+1)
	createServiceAccount(t, database, organizationID, "service-a")
	now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
	manager := newTestManager(t, database, &now, []string{"credential-a"}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
	issued, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue credential for existing organization record: %v", err)
	}
	if _, err := manager.Authenticate(t.Context(), issued.Credential); err != ErrUnauthorized {
		t.Fatalf("oversized organization authentication error = %v, want ErrUnauthorized", err)
	}
}

func TestIssueAndRevokeRejectNonCanonicalCredentialIDs(t *testing.T) {
	for name, credentialID := range map[string]string{
		"dot":       "credential.test",
		"space":     "credential test",
		"non ASCII": "credential-é",
		"too long":  strings.Repeat("c", maxCredentialIDBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			database := migratedDatabase(t)
			createServiceAccount(t, database, "org-a", "service-a")
			now := time.Date(2026, time.September, 3, 8, 0, 0, 0, time.UTC)
			manager := newTestManager(t, database, &now, []string{credentialID}, [][]byte{[]byte("test-service-secret-alpha-000000000000")})
			if _, err := manager.Issue(t.Context(), "service-a", now.Add(time.Hour)); err == nil {
				t.Fatalf("issued credential with noncanonical ID %q", credentialID)
			}
			if err := manager.Revoke(t.Context(), credentialID); err != ErrInvalidManagementRequest {
				t.Fatalf("revoke noncanonical credential ID error = %v, want ErrInvalidManagementRequest", err)
			}
		})
	}
}

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	return database
}

func createServiceAccount(t *testing.T, database *sql.DB, organizationID, serviceAccountID string) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES (?, ?, ?)`, organizationID, organizationID, organizationID); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES (?, ?, ?)`, serviceAccountID, organizationID, serviceAccountID); err != nil {
		t.Fatalf("create service account: %v", err)
	}
}

func newTestManager(t *testing.T, database *sql.DB, now *time.Time, ids []string, secrets [][]byte) *Manager {
	t.Helper()
	manager, err := NewManager(database, Config{
		AllowInsecureTransportForDevelopment: true,
		Now:                                  func() time.Time { return *now },
		NewID: func() (string, error) {
			if len(ids) == 0 {
				t.Fatal("test identifier sequence exhausted")
			}
			value := ids[0]
			ids = ids[1:]
			return value, nil
		},
		NewSecret: func() ([]byte, error) {
			if len(secrets) == 0 {
				t.Fatal("test secret sequence exhausted")
			}
			value := secrets[0]
			secrets = secrets[1:]
			return value, nil
		},
	})
	if err != nil {
		t.Fatalf("construct service credential manager: %v", err)
	}
	return manager
}
