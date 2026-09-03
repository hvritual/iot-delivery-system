package serviceauth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
	yunkagrpc "yunka.io/gateway/rpc/transport/grpc"

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
