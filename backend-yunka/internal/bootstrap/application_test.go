package bootstrap_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	_ "modernc.org/sqlite"
	yunkagrpc "yunka.io/gateway/rpc/transport/grpc"
)

func TestApplicationUsesYunkaRuntimeHostForDeliveryAPI(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "host-test-key")
	ctx := context.Background()
	vault := t.TempDir()
	databasePath := filepath.Join(t.TempDir(), "iot-delivery-yunka.db")
	application, err := bootstrap.New(ctx, bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      vault,
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		BootstrapMode:      bootstrap.BootstrapModeExample,
	})
	if err != nil {
		t.Fatalf("bootstrap Yunka application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown Yunka application: %v", err)
		}
	})

	if application.HTTPAddress() == "" || application.GRPCAddress() == "" {
		t.Fatalf("resolved runtime addresses are required: http=%q grpc=%q", application.HTTPAddress(), application.GRPCAddress())
	}

	health := get(t, "http://"+application.HTTPAddress()+"/health")
	if health.StatusCode != http.StatusOK || !strings.Contains(health.Body, `"status":"ok"`) {
		t.Fatalf("host health = status %d body %q", health.StatusCode, health.Body)
	}

	diagnostics := get(t, "http://"+application.HTTPAddress()+"/__yunka/diagnostics")
	if diagnostics.StatusCode != http.StatusOK || !strings.Contains(diagnostics.Body, `"schemaVersion":1`) {
		t.Fatalf("host diagnostics = status %d body %q", diagnostics.StatusCode, diagnostics.Body)
	}

	dashboardResponse := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/dashboard", "host-test-key")
	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%q", dashboardResponse.StatusCode, dashboardResponse.Body)
	}
	var dashboard httpapi.Dashboard
	if err := json.Unmarshal([]byte(dashboardResponse.Body), &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if len(dashboard.Boards) != 5 || len(dashboard.Items) != 1 || !dashboard.Items[0].IsSample {
		t.Fatalf("dashboard = %#v, want five boards and one sample item", dashboard)
	}

	grpcCtx, grpcCancel := context.WithTimeout(ctx, 5*time.Second)
	defer grpcCancel()
	connection, err := grpc.DialContext(grpcCtx, application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial generated gRPC contract: %v", err)
	}
	defer connection.Close()
	grpcDashboard, err := deliveryv1.NewDeliveryServiceClient(connection).GetDashboard(
		metadata.AppendToOutgoingContext(ctx, strings.ToLower(localauth.APIKeyHeader), "host-test-key"),
		&deliveryv1.GetDashboardRequest{},
	)
	if err != nil {
		t.Fatalf("call generated gRPC dashboard: %v", err)
	}
	if grpcDashboard.GetDashboard() == nil || len(grpcDashboard.GetDashboard().GetBoards()) != 5 {
		t.Fatalf("gRPC dashboard = %#v, want five boards", grpcDashboard)
	}

	createRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", bytes.NewBufferString(`{"title":"Yunka 运行时验收","board":"研发交付效能","owner":"研发负责人"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set(localauth.APIKeyHeader, "host-test-key")
	createResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	createBody, err := io.ReadAll(createResponse.Body)
	_ = createResponse.Body.Close()
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create through hosted application status = %d body=%q, want %d", createResponse.StatusCode, createBody, http.StatusCreated)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil || created.ID == "" {
		t.Fatalf("decode created HTTP item: %#v, %v", created, err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open hosted database for audit assertion: %v", err)
	}
	defer database.Close()
	var actorType, actorID, targetType, targetID, result, traceID, requestID, metadata string
	err = database.QueryRowContext(t.Context(), `SELECT actor_type, actor_id, target_type, target_id, result, trace_id, request_id, metadata
FROM iotd_audit_entries WHERE operation = 'delivery.items.create' ORDER BY sequence DESC LIMIT 1`).Scan(&actorType, &actorID, &targetType, &targetID, &result, &traceID, &requestID, &metadata)
	if err != nil {
		t.Fatalf("read REST audit entry: %v", err)
	}
	if actorType != "system" || actorID != "development-api-key" || targetType != "delivery.work-item" || targetID != created.ID || result != "success" || traceID != createResponse.Header.Get(bffassertion.TraceHeader) || requestID != traceID || !strings.Contains(metadata, `"transport":"http"`) {
		t.Fatalf("REST audit entry = actor=%s/%s target=%s/%s result=%s trace=%s request=%s metadata=%s", actorType, actorID, targetType, targetID, result, traceID, requestID, metadata)
	}
	overviewPath := filepath.Join(vault, "10-交付管理", "00-交付总览.md")
	deadline := time.Now().Add(2 * time.Second)
	for {
		contents, readErr := os.ReadFile(overviewPath)
		if readErr == nil && strings.Contains(string(contents), "Yunka 运行时验收") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox projection did not reach %s before deadline: %v", overviewPath, readErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationAcceptsServiceCredentialOnlyThroughGRPCAndKeepsItUnauthorizedForRoles(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "service-runtime-local-key")
	databasePath := filepath.Join(t.TempDir(), "service-runtime.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open service credential database: %v", err)
	}
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		_ = database.Close()
		t.Fatalf("apply identity migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-service', 'org-service', 'Service organization')`); err != nil {
		_ = database.Close()
		t.Fatalf("create service organization: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-ci', 'org-service', 'ci')`); err != nil {
		_ = database.Close()
		t.Fatalf("create service account: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close prepared service credential database: %v", err)
	}
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		AllowInsecureServiceCredentialsForDevelopment: true,
	})
	if err != nil {
		t.Fatalf("bootstrap service credential application: %v", err)
	}
	t.Cleanup(func() { closeApplication(t, application) })
	issued, err := application.ServiceCredentials().Issue(t.Context(), "service-ci", time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("issue service credential through in-process management port: %v", err)
	}
	connection, err := grpc.NewClient(application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect generated gRPC contract: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	serviceContext := metadata.AppendToOutgoingContext(t.Context(), yunkagrpc.ServiceAuthorizationMetadata, "Bearer "+issued.Credential)
	_, err = deliveryv1.NewDeliveryServiceClient(connection).GetDashboard(serviceContext, &deliveryv1.GetDashboardRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service credential call error = %v, want PermissionDenied after authentication without S0-03 roles", err)
	}
}

func TestApplicationDoesNotSeedExampleByDefault(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "bootstrap-default-disabled-test-key")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "bootstrap-default-disabled.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application with default-disabled example seed: %v", err)
	}
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownContext); err != nil {
			t.Errorf("shutdown bootstrap application: %v", err)
		}
	})

	dashboard := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/dashboard", "bootstrap-default-disabled-test-key")
	if dashboard.StatusCode != http.StatusOK {
		t.Fatalf("default-disabled dashboard status = %d body=%q, want %d", dashboard.StatusCode, dashboard.Body, http.StatusOK)
	}
	var value httpapi.Dashboard
	if err := json.Unmarshal([]byte(dashboard.Body), &value); err != nil {
		t.Fatalf("decode default-disabled dashboard: %v", err)
	}
	if len(value.Items) != 0 {
		t.Fatalf("default-disabled bootstrap items = %#v, want no sample data", value.Items)
	}
}

func TestApplicationRejectsUnknownBootstrapModeBeforePersistentSideEffects(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "unknown-bootstrap-mode.db")
	vault := filepath.Join(t.TempDir(), "unknown-bootstrap-mode-vault")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      vault,
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		BootstrapMode:      "surprise",
	})
	if application != nil {
		t.Fatal("unknown bootstrap mode must not construct an application")
	}
	if err == nil || !strings.Contains(err.Error(), "bootstrap mode") {
		t.Fatalf("unknown bootstrap mode error = %v, want a generic bootstrap mode error", err)
	}
	if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
		t.Fatalf("unknown bootstrap mode created database side effect: %v", statErr)
	}
	if _, statErr := os.Stat(vault); !os.IsNotExist(statErr) {
		t.Fatalf("unknown bootstrap mode created Vault side effect: %v", statErr)
	}
}

func TestExampleBootstrapRequiresExplicitDevelopmentEnvironment(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "example-bootstrap-environment-test-key")
	databasePath := filepath.Join(t.TempDir(), "example-bootstrap-requires-development.db")
	vault := filepath.Join(t.TempDir(), "example-bootstrap-requires-development-vault")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  databasePath,
		ObsidianVault: vault,
		BootstrapMode: bootstrap.BootstrapModeExample,
	})
	if application != nil {
		t.Cleanup(func() {
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if closeErr := application.Close(shutdownContext); closeErr != nil {
				t.Errorf("shutdown unexpectedly constructed example bootstrap: %v", closeErr)
			}
		})
	}
	if err == nil || !strings.Contains(err.Error(), "development") {
		t.Errorf("example bootstrap without explicit development environment error = %v, want a generic development environment error", err)
	}
	if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
		t.Errorf("example bootstrap without development created database side effect: %v", statErr)
	}
	if _, statErr := os.Stat(vault); !os.IsNotExist(statErr) {
		t.Errorf("example bootstrap without development created Vault side effect: %v", statErr)
	}
}

func TestProductionStartupPolicyRejectsBeforeDatabaseVaultOrListenerSideEffects(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		configuration func() bootstrap.Config
		wantError     string
	}{
		{
			name: "example bootstrap",
			configuration: func() bootstrap.Config {
				return bootstrap.Config{RuntimeEnvironment: bootstrap.RuntimeEnvironmentProduction, BootstrapMode: bootstrap.BootstrapModeExample}
			},
			wantError: "example bootstrap",
		},
		{
			name: "legacy local API key",
			configuration: func() bootstrap.Config {
				return bootstrap.Config{RuntimeEnvironment: bootstrap.RuntimeEnvironmentProduction, BootstrapMode: bootstrap.BootstrapModeDisabled, LegacyLocalAPIKeyEnabled: true}
			},
			wantError: "legacy local API-key",
		},
		{
			name: "insecure service credential flag",
			configuration: func() bootstrap.Config {
				return bootstrap.Config{RuntimeEnvironment: bootstrap.RuntimeEnvironmentProduction, BootstrapMode: bootstrap.BootstrapModeDisabled, AllowInsecureServiceCredentialsForDevelopment: true}
			},
			wantError: "insecure service credentials",
		},
		{
			name: "unknown runtime environment",
			configuration: func() bootstrap.Config {
				return bootstrap.Config{RuntimeEnvironment: "unknown", BootstrapMode: bootstrap.BootstrapModeDisabled}
			},
			wantError: "runtime environment",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			configuration := testCase.configuration()
			configuration.HTTPAddress = freeLoopbackAddress(t)
			configuration.GRPCAddress = freeLoopbackAddress(t)
			configuration.DatabasePath = filepath.Join(t.TempDir(), "production-rejection.db")
			configuration.ObsidianVault = filepath.Join(t.TempDir(), "production-rejection-vault")

			application, err := bootstrap.New(t.Context(), configuration)
			if application != nil {
				t.Fatal("production policy rejection must not construct an application")
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("production %s error = %v, want generic %q rejection", testCase.name, err, testCase.wantError)
			}
			if _, statErr := os.Stat(configuration.DatabasePath); !os.IsNotExist(statErr) {
				t.Fatalf("production %s created database side effect: %v", testCase.name, statErr)
			}
			if _, statErr := os.Stat(configuration.ObsidianVault); !os.IsNotExist(statErr) {
				t.Fatalf("production %s created Vault side effect: %v", testCase.name, statErr)
			}
			assertLoopbackAddressAvailable(t, configuration.HTTPAddress)
			assertLoopbackAddressAvailable(t, configuration.GRPCAddress)
		})
	}
}

func TestProductionRequiresValidBFFBeforePersistentSideEffects(t *testing.T) {
	for _, testCase := range []struct {
		name           string
		organizationID string
		assertionKey   string
	}{
		{name: "missing organization", assertionKey: validProductionBFFAssertionKey()},
		{name: "missing assertion key", organizationID: "org-production"},
		{name: "invalid assertion key", organizationID: "org-production", assertionKey: "not-base64url"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "production-bff-validation.db")
			vault := filepath.Join(t.TempDir(), "production-bff-validation-vault")
			application, err := bootstrap.New(t.Context(), bootstrap.Config{
				HTTPAddress:        freeLoopbackAddress(t),
				GRPCAddress:        freeLoopbackAddress(t),
				DatabasePath:       databasePath,
				ObsidianVault:      vault,
				RuntimeEnvironment: bootstrap.RuntimeEnvironmentProduction,
				BootstrapMode:      bootstrap.BootstrapModeDisabled,
				BFFOrganizationID:  testCase.organizationID,
				BFFAssertionKey:    testCase.assertionKey,
			})
			if application != nil {
				t.Fatal("invalid production BFF configuration must not construct an application")
			}
			if err == nil || !strings.Contains(err.Error(), "BFF") {
				t.Fatalf("invalid production BFF configuration error = %v, want generic BFF rejection", err)
			}
			if _, statErr := os.Stat(databasePath); !os.IsNotExist(statErr) {
				t.Fatalf("invalid production BFF configuration created database side effect: %v", statErr)
			}
			if _, statErr := os.Stat(vault); !os.IsNotExist(statErr) {
				t.Fatalf("invalid production BFF configuration created Vault side effect: %v", statErr)
			}
		})
	}
}

func TestProductionBFFOnlyRuntimeStartsWithoutSeedOrLocalAPIKeyAccess(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "")
	databasePath := filepath.Join(t.TempDir(), "production-bff-only.db")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentProduction,
		BootstrapMode:      bootstrap.BootstrapModeDisabled,
		BFFOrganizationID:  "org-production",
		BFFAssertionKey:    validProductionBFFAssertionKey(),
	})
	if err != nil {
		t.Fatalf("start BFF-only production runtime: %v", err)
	}
	t.Cleanup(func() { closeApplication(t, application) })

	response := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/dashboard", "local-key-must-not-work")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("production local API-key request status=%d body=%q, want %d", response.StatusCode, response.Body, http.StatusUnauthorized)
	}
	connection, err := grpc.NewClient(application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect production gRPC runtime: %v", err)
	}
	defer connection.Close()
	grpcContext := metadata.AppendToOutgoingContext(t.Context(), strings.ToLower(localauth.APIKeyHeader), "local-key-must-not-work")
	_, err = deliveryv1.NewDeliveryServiceClient(connection).GetDashboard(grpcContext, &deliveryv1.GetDashboardRequest{})
	if err == nil {
		t.Fatal("production gRPC local API-key fallback unexpectedly succeeded")
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open production database for seed readback: %v", err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_delivery_items`).Scan(&count); err != nil {
		t.Fatalf("count production work items: %v", err)
	}
	if count != 0 {
		t.Fatalf("production BFF-only runtime seeded %d work items, want 0", count)
	}
}

func validProductionBFFAssertionKey() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x61}, 32))
}

func TestStartupPolicyRejectsEveryLegacyLocalAPIKeyEnvironmentInProduction(t *testing.T) {
	for _, environment := range []string{
		localauth.APIKeyEnvironment,
		localauth.ViewerAPIKeyEnvironment,
		localauth.ContributorAPIKeyEnvironment,
		localauth.ReleaseManagerAPIKeyEnvironment,
	} {
		t.Run(environment, func(t *testing.T) {
			const sentinelCredential = "S0_02_08_ROLE_SENTINEL_DO_NOT_LOG"
			policy, err := bootstrap.StartupPolicyFromEnvironment(func(name string) string {
				switch name {
				case "IOT_DELIVERY_RUNTIME_ENVIRONMENT":
					return "production"
				case "IOT_DELIVERY_BOOTSTRAP_MODE":
					return "disabled"
				case environment:
					return sentinelCredential
				default:
					return ""
				}
			})
			if policy != (bootstrap.StartupPolicy{}) {
				t.Fatalf("production policy with %s = %#v, want no accepted policy", environment, policy)
			}
			if err == nil || !strings.Contains(err.Error(), "legacy local API-key") {
				t.Fatalf("production %s error = %v, want generic legacy local API-key rejection", environment, err)
			}
			if strings.Contains(err.Error(), sentinelCredential) {
				t.Fatalf("production %s error leaked sentinel credential: %q", environment, err)
			}
		})
	}
}

func TestStartupPolicyErrorDoesNotLeakSentinelCredentialToCapturedLog(t *testing.T) {
	const sentinelCredential = "S0_02_08_LOG_SENTINEL_DO_NOT_LOG"
	_, err := bootstrap.StartupPolicyFromEnvironment(func(name string) string {
		switch name {
		case "IOT_DELIVERY_RUNTIME_ENVIRONMENT":
			return "production"
		case "IOT_DELIVERY_BOOTSTRAP_MODE":
			return "disabled"
		case localauth.APIKeyEnvironment:
			return sentinelCredential
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("production local API key policy must fail")
	}

	var captured bytes.Buffer
	logger := log.New(&captured, "", 0)
	logger.Printf("configure startup: %v", err)
	if strings.Contains(err.Error(), sentinelCredential) || strings.Contains(captured.String(), sentinelCredential) {
		t.Fatalf("startup error or captured log leaked sentinel credential: %q", captured.String())
	}
}

func freeLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback test address: %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func assertLoopbackAddressAvailable(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("startup rejection retained listener side effect at %s: %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close startup side-effect probe at %s: %v", address, err)
	}
}

func TestApplicationRejectsInsecureServiceCredentialModeOnNonLoopbackGRPC(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "service-runtime-local-key")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "0.0.0.0:0",
		DatabasePath:       filepath.Join(t.TempDir(), "service-runtime.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		AllowInsecureServiceCredentialsForDevelopment: true,
	})
	if application != nil || err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback insecure service credential bootstrap = application=%#v error=%v, want loopback rejection", application, err)
	}
}

func TestApplicationInitializesIdentityCoreSchemaInSharedSQLite(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "identity-core-schema-test-key")
	databasePath := filepath.Join(t.TempDir(), "identity-core.db")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	closeApplication(t, application)

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open shared SQLite database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("configure SQLite schema readback: %v", err)
	}
	for _, table := range []string{"organizations", "users", "external_identities", "service_accounts", "iotd_audit_entries", "iotd_schema_migrations"} {
		var name string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("identity table %q is required in shared SQLite: %v", table, err)
		}
	}
	for _, table := range []string{"organizations", "users", "external_identities", "service_accounts"} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count migrated identity table %q: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("migration must not create %s records, got %d", table, count)
		}
	}

	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = 'S0-02-01_identity_core_v1'`).Scan(&migrationCount); err != nil {
		t.Fatalf("read identity migration ledger: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("identity migration ledger rows = %d, want 1", migrationCount)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = 'S0-04-01_audit_entries_v1'`).Scan(&migrationCount); err != nil || migrationCount != 1 {
		t.Fatalf("audit migration ledger rows = %d error=%v, want 1", migrationCount, err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&migrationCount); err != nil || migrationCount != 0 {
		t.Fatalf("bootstrap audit entries = %d error=%v, want 0", migrationCount, err)
	}
}

func TestApplicationInitializesConfigRevisionSchemaExactlyOnce(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "config-revision-schema-test-key")
	databasePath := filepath.Join(t.TempDir(), "config-revisions.db")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	closeApplication(t, application)

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open bootstrapped SQLite database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("configure config revision schema readback: %v", err)
	}
	for _, name := range []string{"iotd_config_revisions"} {
		var found string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found); err != nil {
			t.Fatalf("bootstrap must create %q: %v", name, err)
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = 'S0-04-05_config_revisions_v1'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("config revision migration ledger rows = %d error=%v, want 1", count, err)
	}
	if _, err := database.Exec(`SELECT COUNT(*) FROM iotd_config_revisions`); err != nil {
		t.Fatalf("read config revision table: %v", err)
	}
}

func TestApplicationIdentityCoreMigrationPreservesDeliveryDataAndEnforcesDatabaseInvariants(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "identity-core-invariants-test-key")
	databasePath := filepath.Join(t.TempDir(), "identity-core-invariants.db")
	repository, err := delivery.NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatalf("create delivery SQLite database: %v", err)
	}
	if _, err := repository.Database().Exec(`INSERT INTO iotd_delivery_items (id, payload, updated_at, revision) VALUES ('preexisting-delivery', '{}', '2026-09-03T00:00:00Z', 1)`); err != nil {
		t.Fatalf("insert preexisting delivery row: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close delivery SQLite database: %v", err)
	}

	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	closeApplication(t, application)

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open shared SQLite database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("configure SQLite schema readback: %v", err)
	}
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys for schema readback: %v", err)
	}
	var preserved int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_delivery_items WHERE id = 'preexisting-delivery'`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("preexisting delivery row count = %d, error=%v, want 1 without loss", preserved, err)
	}

	for _, table := range []string{"organizations", "users", "external_identities", "service_accounts"} {
		rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatalf("read table info for %s: %v", table, err)
		}
		columns := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, columnType string
			var notNull, primaryKey int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
				t.Fatalf("scan table info for %s: %v", table, err)
			}
			columns[name] = true
			if strings.Contains(strings.ToLower(name), "secret") || strings.Contains(strings.ToLower(name), "token") || strings.Contains(strings.ToLower(name), "password") || strings.Contains(strings.ToLower(name), "api_key") {
				t.Fatalf("identity table %s must not contain credential column %q", table, name)
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close table info for %s: %v", table, err)
		}
		for _, required := range []string{"id", "status", "created_at", "updated_at"} {
			if !columns[required] {
				t.Fatalf("identity table %s missing required column %q", table, required)
			}
		}
		if table == "external_identities" {
			if columns["profile_snapshot"] {
				t.Fatal("external identities must not expose opaque profile_snapshot storage")
			}
			for _, required := range []string{"email_snapshot", "display_name_snapshot"} {
				if !columns[required] {
					t.Fatalf("external identities missing explicit non-sensitive snapshot column %q", required)
				}
			}
		}
	}

	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A'), ('org-b', 'org-b', 'Organization B')`); err != nil {
		t.Fatalf("insert organizations with active default: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name, email) VALUES ('user-a', 'org-a', 'Alice', 'shared@example.test'), ('user-b', 'org-a', 'Bob', 'shared@example.test'), ('user-c', 'org-b', 'Carol', 'other@example.test')`); err != nil {
		t.Fatalf("duplicate user emails in the same organization must be allowed: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'ci')`); err != nil {
		t.Fatalf("insert service account with active default: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO external_identities (id, organization_id, user_id, issuer, subject) VALUES ('identity-a', 'org-a', 'user-a', 'https://issuer.example.test', 'subject-a')`); err != nil {
		t.Fatalf("insert external identity: %v", err)
	}
	var organizationStatus, userStatus, identityStatus, serviceAccountStatus string
	if err := database.QueryRow(`SELECT status FROM organizations WHERE id = 'org-a'`).Scan(&organizationStatus); err != nil {
		t.Fatalf("read organization default status: %v", err)
	}
	if err := database.QueryRow(`SELECT status FROM users WHERE id = 'user-a'`).Scan(&userStatus); err != nil {
		t.Fatalf("read user default status: %v", err)
	}
	if err := database.QueryRow(`SELECT status FROM external_identities WHERE id = 'identity-a'`).Scan(&identityStatus); err != nil {
		t.Fatalf("read external identity default status: %v", err)
	}
	if err := database.QueryRow(`SELECT status FROM service_accounts WHERE id = 'service-a'`).Scan(&serviceAccountStatus); err != nil {
		t.Fatalf("read service-account default status: %v", err)
	}
	if organizationStatus != "active" || userStatus != "active" || identityStatus != "active" || serviceAccountStatus != "active" {
		t.Fatalf("identity default statuses = organization=%q user=%q externalIdentity=%q serviceAccount=%q, want active", organizationStatus, userStatus, identityStatus, serviceAccountStatus)
	}
	var users, serviceAccounts int
	if err := database.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count human users: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM service_accounts`).Scan(&serviceAccounts); err != nil {
		t.Fatalf("count service accounts: %v", err)
	}
	if users != 3 || serviceAccounts != 1 {
		t.Fatalf("human and service records must remain physically separate: users=%d service_accounts=%d", users, serviceAccounts)
	}
	for _, statement := range []string{
		`INSERT INTO external_identities (id, organization_id, user_id, issuer, subject) VALUES ('identity-duplicate', 'org-a', 'user-a', 'https://issuer.example.test', 'subject-a')`,
		`INSERT INTO external_identities (id, organization_id, user_id, issuer, subject) VALUES ('identity-cross-org', 'org-b', 'user-a', 'https://issuer.example.test', 'subject-b')`,
		`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-duplicate', 'org-a', 'ci')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('user-invalid-status', 'org-a', 'Invalid', 'pending')`,
		`DELETE FROM users WHERE id = 'user-a'`,
		`DELETE FROM organizations WHERE id = 'org-a'`,
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Fatalf("identity invariant statement unexpectedly succeeded: %s", statement)
		}
	}

	application, err = bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application second time: %v", err)
	}
	closeApplication(t, application)
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = 'S0-02-01_identity_core_v1'`).Scan(&preserved); err != nil || preserved != 1 {
		t.Fatalf("idempotent identity migration ledger rows = %d, error=%v, want 1", preserved, err)
	}
	t.Log("schema readback: four identity tables are empty after bootstrap; migration ledger=1; delivery row preserved; same-organization duplicate email allowed; explicit non-sensitive snapshots present; duplicate issuer+subject, cross-organization identity, invalid status, duplicate service name, and user/organization restricted deletes rejected")
}

func TestBootstrapSeedIsAnAuthorizedTransactionalOutboxOperation(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate bootstrap test source")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(sourcePath), "application.go"))
	if err != nil {
		t.Fatalf("read bootstrap source: %v", err)
	}
	text := string(source)
	for _, required := range []string{
		"service := delivery.NewService(repository, exporter, delivery.NewTransactionalOutboxStager(outboxStore))",
		"if err := seedExample(ctx, operations); err != nil",
		"func seedExample(ctx context.Context, operations *deliveryapplication.Operations) error",
		"operations.List(bootstrapContext)",
		"operations.Create(bootstrapContext, delivery.CreateInput{",
		"operations.UpdateContext(bootstrapContext, item.ID, item.Revision, delivery.ContextUpdate{",
		"Authenticated: true",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("bootstrap seed must use the authorized Operations/Executor path; missing %q", required)
		}
	}
	if strings.Contains(text, "seedService.Sync(ctx)") || strings.Contains(text, "service.Sync(ctx)") {
		t.Error("bootstrap must not synchronously project seed data around the committed Outbox/dispatcher chain")
	}
}

func TestBootstrapSeedStagesCommittedOutboxEventsThenProjectsExactlyOnce(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "bootstrap-seed-key")
	ctx := context.Background()
	vault := t.TempDir()
	databasePath := filepath.Join(t.TempDir(), "bootstrap-seed.db")
	application := newSeedApplication(t, ctx, databasePath, vault)

	assertSeededOutboxProjection(t, ctx, databasePath, vault)
	closeApplication(t, application)

	application = newSeedApplication(t, ctx, databasePath, vault)
	defer closeApplication(t, application)
	assertSeededOutboxProjection(t, ctx, databasePath, vault)

	dashboardResponse := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/dashboard", "bootstrap-seed-key")
	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("read seed dashboard after restart = %d body=%q", dashboardResponse.StatusCode, dashboardResponse.Body)
	}
	var dashboard httpapi.Dashboard
	if err := json.Unmarshal([]byte(dashboardResponse.Body), &dashboard); err != nil {
		t.Fatalf("decode seed dashboard after restart: %v", err)
	}
	if len(dashboard.Items) != 1 || !dashboard.Items[0].IsSample || len(dashboard.Items[0].Decisions) != 1 {
		t.Fatalf("seed dashboard after restart = %#v, want one decision-bearing sample", dashboard)
	}
}

func newSeedApplication(t *testing.T, ctx context.Context, databasePath, vault string) *bootstrap.Application {
	t.Helper()
	application, err := bootstrap.New(ctx, bootstrap.Config{HTTPAddress: "127.0.0.1:0", GRPCAddress: "127.0.0.1:0", DatabasePath: databasePath, ObsidianVault: vault, RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment, BootstrapMode: bootstrap.BootstrapModeExample})
	if err != nil {
		t.Fatalf("bootstrap seed application: %v", err)
	}
	return application
}

func closeApplication(t *testing.T, application *bootstrap.Application) {
	t.Helper()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.Close(shutdownCtx); err != nil {
		t.Fatalf("shutdown seed application: %v", err)
	}
}

func assertSeededOutboxProjection(t *testing.T, ctx context.Context, databasePath, vault string) {
	t.Helper()
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open seed database for readback: %v", err)
	}
	defer database.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var total, published int
		err = database.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(CASE WHEN status = 'published' THEN 1 END) FROM iotd_outbox`).Scan(&total, &published)
		if err == nil && total == 2 && published == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("seed outbox total=%d published=%d err=%v; want exactly two committed and published events", total, published, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	rows, err := database.QueryContext(ctx, `SELECT envelope_json FROM iotd_outbox ORDER BY created_at, id`)
	if err != nil {
		t.Fatalf("read seed outbox envelopes: %v", err)
	}
	defer rows.Close()
	var subjects []string
	eventTypes := map[string]bool{}
	for rows.Next() {
		var envelope struct {
			Subject string `json:"subject"`
			Type    string `json:"type"`
		}
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan seed outbox envelope: %v", err)
		}
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			t.Fatalf("decode seed outbox envelope: %v", err)
		}
		subjects = append(subjects, envelope.Subject)
		eventTypes[envelope.Type] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seed outbox envelopes: %v", err)
	}
	if len(subjects) != 2 || subjects[0] == "" || subjects[0] != subjects[1] || !eventTypes["delivery.work-item.created"] || !eventTypes["delivery.work-item.context-updated"] {
		t.Fatalf("seed outbox subjects=%#v eventTypes=%#v, want one subject with create and context-update", subjects, eventTypes)
	}
	overview, err := os.ReadFile(filepath.Join(vault, "10-交付管理", "00-交付总览.md"))
	if err != nil {
		t.Fatalf("read projected seed overview: %v", err)
	}
	if got := strings.Count(string(overview), "样例：设备 OTA 发布验收"); got != 1 {
		t.Fatalf("projected seed occurrences = %d, want 1", got)
	}
}

func TestApplicationProtectsBusinessHTTPAndGeneratedGRPCWithLocalAPIKey(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "bootstrap-test-key")
	t.Setenv(localauth.ViewerAPIKeyEnvironment, "bootstrap-viewer-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap protected application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown protected application: %v", err)
		}
	})

	if health := get(t, "http://"+application.HTTPAddress()+"/health"); health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", health.StatusCode, http.StatusOK)
	}
	if dashboard := get(t, "http://"+application.HTTPAddress()+"/api/dashboard"); dashboard.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard status = %d, want %d", dashboard.StatusCode, http.StatusUnauthorized)
	}
	request, err := http.NewRequest(http.MethodGet, "http://"+application.HTTPAddress()+"/api/dashboard", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(localauth.APIKeyHeader, "bootstrap-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated dashboard status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	viewerCreate, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{"title":"viewer cannot create","board":"研发交付效能","owner":"viewer"}`))
	if err != nil {
		t.Fatal(err)
	}
	viewerCreate.Header.Set("Content-Type", "application/json")
	viewerCreate.Header.Set(localauth.APIKeyHeader, "bootstrap-viewer-key")
	viewerResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(viewerCreate)
	if err != nil {
		t.Fatal(err)
	}
	_ = viewerResponse.Body.Close()
	if viewerResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want %d", viewerResponse.StatusCode, http.StatusForbidden)
	}

	grpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := grpc.DialContext(grpcCtx, application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial generated gRPC contract: %v", err)
	}
	defer connection.Close()
	client := deliveryv1.NewDeliveryServiceClient(connection)
	if _, err := client.GetDashboard(context.Background(), &deliveryv1.GetDashboardRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated gRPC dashboard error = %v, want Unauthenticated", err)
	}
	authenticatedCtx := metadata.AppendToOutgoingContext(context.Background(), strings.ToLower(localauth.APIKeyHeader), "bootstrap-test-key")
	if _, err := client.GetDashboard(authenticatedCtx, &deliveryv1.GetDashboardRequest{}); err != nil {
		t.Fatalf("authenticated gRPC dashboard: %v", err)
	}
}

func TestApplicationPersistsDevelopmentAPIKeyAuthenticationFailure(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "audit-authentication-test-key")
	databasePath := filepath.Join(t.TempDir(), "audit-authentication.db")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	response := get(t, "http://"+application.HTTPAddress()+"/api/dashboard")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open audit database: %v", err)
	}
	defer database.Close()
	var eventCategory, actorType, actorID, operation, decision, result, reasonCode, metadata string
	err = database.QueryRow(`SELECT event_category, actor_type, COALESCE(actor_id, ''), operation, authorization_decision, result, reason_code, metadata
FROM iotd_audit_entries`).Scan(&eventCategory, &actorType, &actorID, &operation, &decision, &result, &reasonCode, &metadata)
	if err != nil {
		t.Fatalf("read development API-key authentication audit: %v", err)
	}
	if eventCategory != "authentication" || actorType != "anonymous" || actorID != "" || operation != "authentication.development_api_key" || decision != "not_evaluated" || result != "failure" || reasonCode != "authentication.invalid_credential" || metadata != `{"failure_class":"credential","phase":"authentication","transport":"http"}` {
		t.Fatalf("development API-key authentication audit = category=%q actor=%q/%q operation=%q decision=%q result=%q reason=%q metadata=%q", eventCategory, actorType, actorID, operation, decision, result, reasonCode, metadata)
	}
}

func TestApplicationPersistsAuthorizationDenialWithoutBusinessSideEffects(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "audit-denial-admin-key")
	t.Setenv(localauth.ViewerAPIKeyEnvironment, "audit-denial-viewer-key")
	databasePath := filepath.Join(t.TempDir(), "audit-authorization-denial.db")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{"title":"denied","board":"研发交付效能","owner":"viewer"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "audit-denial-viewer-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer write status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open audit database: %v", err)
	}
	defer database.Close()
	var auditCount, itemCount, outboxCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE event_category = 'authorization'`).Scan(&auditCount); err != nil {
		t.Fatalf("count authorization audit entries: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_delivery_items`).Scan(&itemCount); err != nil {
		t.Fatalf("count delivery items: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if auditCount != 1 || itemCount != 0 || outboxCount != 0 {
		t.Fatalf("authorization denial side effects = audit=%d items=%d outbox=%d, want 1/0/0", auditCount, itemCount, outboxCount)
	}
}

func TestApplicationPersistsServiceTokenAuthenticationFailure(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "audit-service-token-fallback-key")
	databasePath := filepath.Join(t.TempDir(), "audit-service-token.db")
	application, err := bootstrap.New(t.Context(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       databasePath,
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		AllowInsecureServiceCredentialsForDevelopment: true,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	dialCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connection, err := grpc.DialContext(dialCtx, application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial gRPC endpoint: %v", err)
	}
	defer connection.Close()
	client := deliveryv1.NewDeliveryServiceClient(connection)
	callCtx := metadata.AppendToOutgoingContext(t.Context(), yunkagrpc.ServiceAuthorizationMetadata, "Bearer svc.invalid.not-a-real-secret")
	if _, err := client.GetDashboard(callCtx, &deliveryv1.GetDashboardRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("invalid service token error = %v, want Unauthenticated", err)
	}

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatalf("open audit database: %v", err)
	}
	defer database.Close()
	var eventCategory, actorType, actorID, operation, decision, result, reasonCode, metadataValue string
	err = database.QueryRow(`SELECT event_category, actor_type, COALESCE(actor_id, ''), operation, authorization_decision, result, reason_code, metadata
FROM iotd_audit_entries`).Scan(&eventCategory, &actorType, &actorID, &operation, &decision, &result, &reasonCode, &metadataValue)
	if err != nil {
		t.Fatalf("read service-token authentication audit: %v", err)
	}
	if eventCategory != "authentication" || actorType != "anonymous" || actorID != "" || operation != "authentication.service_token" || decision != "not_evaluated" || result != "failure" || reasonCode != "authentication.invalid_credential" || strings.Contains(metadataValue, "svc.") {
		t.Fatalf("service-token authentication audit = category=%q actor=%q/%q operation=%q decision=%q result=%q reason=%q metadata=%q", eventCategory, actorType, actorID, operation, decision, result, reasonCode, metadataValue)
	}
}

func TestApplicationCreatesProjectThroughAuthorizedRuntime(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "project-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/projects", strings.NewReader(`{
		"name":"OTA 2026.09 发布",
		"board":"研发交付效能",
		"owner":"发布负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read create project response: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create project through hosted application status = %d body=%q, want %d", response.StatusCode, body, http.StatusCreated)
	}
	var project map[string]any
	if err := json.Unmarshal(body, &project); err != nil {
		t.Fatalf("decode hosted project: %v", err)
	}
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("hosted project id = %#v, want non-empty", project["id"])
	}

	itemRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"完成 OTA 发布范围",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"projectId":"`+projectID+`",
		"kind":"epic"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	itemRequest.Header.Set("Content-Type", "application/json")
	itemRequest.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	itemResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(itemRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer itemResponse.Body.Close()
	itemBody, err := io.ReadAll(itemResponse.Body)
	if err != nil {
		t.Fatalf("read hosted item response: %v", err)
	}
	if itemResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create hosted work item status = %d body=%q, want %d", itemResponse.StatusCode, itemBody, http.StatusCreated)
	}
	var item map[string]any
	if err := json.Unmarshal(itemBody, &item); err != nil {
		t.Fatalf("decode hosted work item: %v", err)
	}
	if item["projectId"] != projectID || item["kind"] != "epic" {
		t.Fatalf("hosted hierarchy = %#v, want project=%q kind=epic", item, projectID)
	}

	listRequest, err := http.NewRequest(http.MethodGet, "http://"+application.HTTPAddress()+"/api/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	listRequest.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	listResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(listRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	listBody, err := io.ReadAll(listResponse.Body)
	if err != nil {
		t.Fatalf("read hosted list response: %v", err)
	}
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("list hosted work items status = %d body=%q, want %d", listResponse.StatusCode, listBody, http.StatusOK)
	}
	var items []map[string]any
	if err := json.Unmarshal(listBody, &items); err != nil {
		t.Fatalf("decode hosted work item list: %v", err)
	}
	if len(items) != 1 || items[0]["projectId"] != projectID || items[0]["kind"] != "epic" {
		t.Fatalf("hosted work item list = %#v, want project hierarchy fields", items)
	}
}

func TestApplicationDeliversWorkItemEventsToLocalNotificationInbox(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "notification-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	createRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"通知本地收件箱验收",
		"board":"研发交付效能",
		"owner":"通知负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set(localauth.APIKeyHeader, "notification-runtime-test-key")
	createResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer createResponse.Body.Close()
	createBody, err := io.ReadAll(createResponse.Body)
	if err != nil {
		t.Fatalf("read created work item: %v", err)
	}
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create work item status = %d body=%q, want %d", createResponse.StatusCode, createBody, http.StatusCreated)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatalf("decode created work item: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created work item id = %q, want non-empty", created.ID)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		response := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/notifications", "notification-runtime-test-key")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("list local notifications status = %d body=%q, want %d", response.StatusCode, response.Body, http.StatusOK)
		}
		var notifications []struct {
			Channel   string `json:"channel"`
			EventType string `json:"eventType"`
			Subject   string `json:"subject"`
		}
		if err := json.Unmarshal([]byte(response.Body), &notifications); err != nil {
			t.Fatalf("decode local notifications: %v", err)
		}
		for _, notification := range notifications {
			if notification.Channel == "local-inbox" && notification.EventType == "delivery.work-item.created" && notification.Subject == created.ID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("local notification inbox = %#v, want creation event for %q", notifications, created.ID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationSchedulesDueRemindersThroughTheDurableOutbox(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "due-reminder-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		DueReminder:        delivery.DueReminderConfig{LeadDays: 0, Interval: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("bootstrap application with reminder worker: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"运行时截止提醒验收",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"dueDate":"`+time.Now().UTC().Format("2006-01-02")+`"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "due-reminder-runtime-test-key")
	created, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(created.Body)
		if readErr != nil {
			t.Fatalf("read create response: %v", readErr)
		}
		t.Fatalf("create due item status = %d body=%q", created.StatusCode, body)
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&item); err != nil || item.ID == "" {
		t.Fatalf("decode due item = %#v, error=%v", item, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		response := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/notifications", "due-reminder-runtime-test-key")
		var notifications []struct {
			EventType string `json:"eventType"`
			Subject   string `json:"subject"`
		}
		if response.StatusCode == http.StatusOK && json.Unmarshal([]byte(response.Body), &notifications) == nil {
			for _, notification := range notifications {
				if notification.EventType == delivery.DueReminderEventType && notification.Subject == item.ID {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("due reminder notifications = %#v, want reminder for %q", notifications, item.ID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationRegistersAdditionalNotificationChannelsAtAssembly(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "notification-channel-assembly-test-key")
	delivered := make(chan notification.Notification, 8)
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
		NotificationChannels: []notification.Channel{
			recordingNotificationChannel{deliveries: delivered},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap application with additional channel: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"可插拔通道装配验收",
		"board":"研发交付效能",
		"owner":"通知负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "notification-channel-assembly-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read create response: %v", readErr)
		}
		t.Fatalf("create work item status = %d body=%q, want %d", response.StatusCode, body, http.StatusCreated)
	}
	var created delivery.WorkItem
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode created work item: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created work item must have an ID")
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case deliveredNotification := <-delivered:
			if deliveredNotification.Channel == "recording-test" && deliveredNotification.EventType == "delivery.work-item.created" && deliveredNotification.Subject == created.ID {
				return
			}
		case <-deadline:
			t.Fatal("additional notification channel did not receive the requested work-item creation event")
		}
	}
}

type recordingNotificationChannel struct {
	deliveries chan<- notification.Notification
}

func (channel recordingNotificationChannel) Name() string {
	return "recording-test"
}

func (channel recordingNotificationChannel) Deliver(_ context.Context, value notification.Notification) error {
	channel.deliveries <- value
	return nil
}

func TestApplicationRunsR2PlanningThroughAuthorizedRuntime(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "r2-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:        "127.0.0.1:0",
		GRPCAddress:        "127.0.0.1:0",
		DatabasePath:       filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault:      t.TempDir(),
		RuntimeEnvironment: bootstrap.RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	projectRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/projects", strings.NewReader(`{
		"name":"R2 运行时计划验收", "board":"研发交付效能", "owner":"发布负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	projectRequest.Header.Set("Content-Type", "application/json")
	projectRequest.Header.Set(localauth.APIKeyHeader, "r2-runtime-test-key")
	projectResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(projectRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer projectResponse.Body.Close()
	var project struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if projectResponse.StatusCode != http.StatusCreated || project.ID == "" {
		t.Fatalf("create runtime project status=%d project=%#v", projectResponse.StatusCode, project)
	}

	releaseRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/releases", strings.NewReader(`{
		"projectId":"`+project.ID+`", "name":"R2 版本", "version":"2.9.0", "targetDate":"2026-09-11"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	releaseRequest.Header.Set("Content-Type", "application/json")
	releaseRequest.Header.Set(localauth.APIKeyHeader, "r2-runtime-test-key")
	releaseResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(releaseRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseResponse.Body.Close()
	body, err := io.ReadAll(releaseResponse.Body)
	if err != nil {
		t.Fatalf("read release response: %v", err)
	}
	if releaseResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create runtime release status=%d body=%q, want %d", releaseResponse.StatusCode, body, http.StatusCreated)
	}
}

type response struct {
	StatusCode int
	Body       string
}

func get(t *testing.T, url string) response {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	result, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: result.StatusCode, Body: string(body)}
}

func getWithAPIKey(t *testing.T, url, apiKey string) response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(localauth.APIKeyHeader, apiKey)
	result, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: result.StatusCode, Body: string(body)}
}
