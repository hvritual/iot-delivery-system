package bootstrap

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	generatedassembly "github.com/hvritual/iot-delivery-system/backend-yunka/internal/assembly"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffassertion"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bffhttp"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configapplication"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configrevision"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	deliveryrpc "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/transport/rpc"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryruntime"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitybinding"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localbootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/obsidian"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/principalauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	"github.com/hvritual/yunka.io/framework/core"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/event"
	frameworkoutbox "github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/kernel"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/framework/platform"
	"github.com/hvritual/yunka.io/framework/runtimehost"
	"github.com/hvritual/yunka.io/gateway/authz"
	"google.golang.org/grpc"
)

type BootstrapMode string

type RuntimeEnvironment string

const (
	BootstrapModeDisabled BootstrapMode = "disabled"
	BootstrapModeExample  BootstrapMode = "example"

	RuntimeEnvironmentDevelopment RuntimeEnvironment = "development"
	RuntimeEnvironmentProduction  RuntimeEnvironment = "production"
)

type Config struct {
	HTTPAddress              string
	GRPCAddress              string
	DatabasePath             string
	ObsidianVault            string
	NotificationChannels     []notification.Channel
	DueReminder              delivery.DueReminderConfig
	BFFOrganizationID        string
	BFFAssertionKey          string
	// LocalAuthJWTSigningKey is a dedicated canonical base64url HMAC key for
	// YU-21 internal JWTs. It is intentionally distinct from BFFAssertionKey.
	// Until YU-26 exposes local-auth routes, an empty value leaves the in-process
	// LocalAuthentication capability disabled while still applying session schema.
	LocalAuthJWTSigningKey   string
	RuntimeEnvironment       RuntimeEnvironment
	BootstrapMode            BootstrapMode
	LegacyLocalAPIKeyEnabled bool
	// AllowInsecureServiceCredentialsForDevelopment permits service credentials
	// only on a loopback gRPC listener for hermetic tests or local development.
	// Its zero value is false; deployed callers require transport privacy and
	// integrity through Yunka's service credential verifier.
	AllowInsecureServiceCredentialsForDevelopment bool
}

type StartupPolicy struct {
	RuntimeEnvironment                            RuntimeEnvironment
	BootstrapMode                                 BootstrapMode
	LegacyLocalAPIKeyEnabled                      bool
	AllowInsecureServiceCredentialsForDevelopment bool
}

func StartupPolicyFromEnvironment(getenv func(string) string) (StartupPolicy, error) {
	if getenv == nil {
		return StartupPolicy{}, errors.New("startup environment reader is required")
	}
	policy := StartupPolicy{
		RuntimeEnvironment:       RuntimeEnvironment(strings.TrimSpace(getenv("IOT_DELIVERY_RUNTIME_ENVIRONMENT"))),
		BootstrapMode:            BootstrapMode(strings.TrimSpace(getenv("IOT_DELIVERY_BOOTSTRAP_MODE"))),
		LegacyLocalAPIKeyEnabled: legacyLocalAPIKeyConfigured(getenv),
	}
	if policy.BootstrapMode == "" {
		policy.BootstrapMode = BootstrapModeDisabled
	}
	switch strings.TrimSpace(getenv("IOT_DELIVERY_ALLOW_INSECURE_SERVICE_CREDENTIALS_FOR_DEVELOPMENT")) {
	case "", "false":
	case "true":
		policy.AllowInsecureServiceCredentialsForDevelopment = true
	default:
		return StartupPolicy{}, errors.New("insecure service credential development flag must be true or false")
	}
	if err := validateStartupPolicy(Config{
		RuntimeEnvironment:       policy.RuntimeEnvironment,
		BootstrapMode:            policy.BootstrapMode,
		LegacyLocalAPIKeyEnabled: policy.LegacyLocalAPIKeyEnabled,
		AllowInsecureServiceCredentialsForDevelopment: policy.AllowInsecureServiceCredentialsForDevelopment,
	}); err != nil {
		return StartupPolicy{}, err
	}
	return policy, nil
}

func legacyLocalAPIKeyConfigured(getenv func(string) string) bool {
	for _, name := range []string{
		localauth.APIKeyEnvironment,
		localauth.ViewerAPIKeyEnvironment,
		localauth.ContributorAPIKeyEnvironment,
		localauth.ReleaseManagerAPIKeyEnvironment,
	} {
		if strings.TrimSpace(getenv(name)) != "" {
			return true
		}
	}
	return false
}

func (policy StartupPolicy) Apply(configuration Config) Config {
	configuration.RuntimeEnvironment = policy.RuntimeEnvironment
	configuration.BootstrapMode = policy.BootstrapMode
	configuration.LegacyLocalAPIKeyEnabled = policy.LegacyLocalAPIKeyEnabled
	configuration.AllowInsecureServiceCredentialsForDevelopment = policy.AllowInsecureServiceCredentialsForDevelopment
	return configuration
}

func (policy StartupPolicy) ValidateLocalStdio() error {
	if policy.RuntimeEnvironment != RuntimeEnvironmentDevelopment {
		return errors.New("iot-delivery-mcp is development-only")
	}
	return nil
}

type Application struct {
	operations     *deliveryapplication.Operations
	configOps      *configapplication.Operations
	serviceAuth    *serviceauth.Manager
	serviceGrants  *serviceauthz.Manager
	adminBootstrap *localbootstrap.Manager
	memberAdmin    *localmemberadmin.Manager
	localLogin     *locallogin.Manager
	app            *core.App

	httpAddress string
	grpcAddress string
}

func New(ctx context.Context, configuration Config) (*Application, error) {
	if err := validateStartupPolicy(configuration); err != nil {
		return nil, err
	}
	if err := validateBFFConfiguration(configuration); err != nil {
		return nil, err
	}
	if err := validateLocalAuthConfiguration(configuration); err != nil {
		return nil, err
	}
	configuration.HTTPAddress = strings.TrimSpace(configuration.HTTPAddress)
	configuration.GRPCAddress = strings.TrimSpace(configuration.GRPCAddress)
	configuration.DatabasePath = strings.TrimSpace(configuration.DatabasePath)
	configuration.ObsidianVault = strings.TrimSpace(configuration.ObsidianVault)
	if configuration.HTTPAddress == "" || configuration.GRPCAddress == "" || configuration.DatabasePath == "" || configuration.ObsidianVault == "" {
		return nil, errors.New("HTTP address, gRPC address, database path, and Obsidian vault are required")
	}
	if configuration.AllowInsecureServiceCredentialsForDevelopment && !loopbackAddress(configuration.GRPCAddress) {
		return nil, errors.New("insecure service credentials require a loopback gRPC listener")
	}

	repository, err := delivery.NewSQLiteRepository(configuration.DatabasePath)
	if err != nil {
		return nil, err
	}
	if err := identitycore.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize identity core schema: %w", err)
	}
	if err := localcredential.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize local credential schema: %w", err)
	}
	if err := localbootstrap.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize local administrator bootstrap schema: %w", err)
	}
	if err := localmemberadmin.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize local member administration schema: %w", err)
	}
	if err := locallogin.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize local login session schema: %w", err)
	}
	if err := configrevision.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize config revision schema: %w", err)
	}
	if err := audit.ApplyMigrations(ctx, repository.Database()); err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("initialize audit schema: %w", err)
	}
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure audit store: %w", err)
	}
	securityRecorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure security audit recorder: %w", err)
	}
	serviceCredentialManager, err := serviceauth.NewManager(repository.Database(), serviceauth.Config{AllowInsecureTransportForDevelopment: configuration.AllowInsecureServiceCredentialsForDevelopment, AuditRecorder: securityRecorder})
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure service credential manager: %w", err)
	}
	serviceGrantManager, err := serviceauthz.NewManager(repository.Database(), repository, serviceauthz.WithAuditRecorder(securityRecorder))
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure service grant manager: %w", err)
	}
	identityResolver, err := identitybinding.NewSQLiteResolver(repository.Database(), identitybinding.Config{})
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure identity binding resolver: %w", err)
	}
	exporter := obsidian.NewExporter(configuration.ObsidianVault)
	outboxStore, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	notificationStore, err := notification.NewSQLiteStore(repository.Database())
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	var authenticator *localauth.Authenticator
	if configuration.RuntimeEnvironment == RuntimeEnvironmentDevelopment {
		authenticator, err = localauth.FromEnvironment()
		if err != nil {
			_ = repository.Close()
			return nil, err
		}
	}
	httpMiddleware, err := configuredHTTPMiddleware(authenticator, identityResolver, securityRecorder, configuration)
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	authorizer, guards, err := configuredAuthorization(ctx, configuration, repository)
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	security, err := authz.NewExecutionSecurity(authorizer, guards)
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("create operation security: %w", err)
	}
	transactionFactory := localtx.NewSQLiteFactory(repository.Database())
	localCredentialRepository, err := localcredential.NewSQLiteRepository(repository.Database())
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure local credential repository: %w", err)
	}
	adminBootstrap, err := localbootstrap.NewManager(repository.Database(), localCredentialRepository, auditStore, operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: transactionFactory}))
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure local administrator bootstrap: %w", err)
	}
	localLogin, err := configuredLocalLogin(configuration, repository.Database(), localCredentialRepository, auditStore, transactionFactory)
	if err != nil {
		_ = repository.Close()
		return nil, err
	}
	broker := event.NewLocalBroker(nil)
	dispatcher, err := frameworkoutbox.NewDispatcher(outboxStore, broker, frameworkoutbox.DispatcherConfig{
		WorkerID: "iot-delivery-local-outbox", PollInterval: 50 * time.Millisecond, BatchSize: 10, Concurrency: 1,
		LeaseDuration: 30 * time.Second, PublishTimeout: 2 * time.Second, RetryBase: 100 * time.Millisecond, RetryMax: 2 * time.Second,
	})
	if err != nil {
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("create local outbox dispatcher: %w", err)
	}
	eventRuntime, err := deliveryruntime.New(deliveryruntime.Dependencies{
		Database: repository, Transactions: transactionFactory, Outbox: outboxStore, Notifications: notificationStore, Projection: exporter,
		Dispatcher: dispatcher, Broker: broker,
	})
	if err != nil {
		_ = dispatcher.Shutdown(context.Background())
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("configure delivery event runtime: %w", err)
	}
	runtimePlatform, err := platform.New(platform.Options{})
	if err != nil {
		_ = eventRuntime.Shutdown(context.Background())
		return nil, fmt.Errorf("configure Yunka Platform provider: %w", err)
	}
	application := &Application{serviceAuth: serviceCredentialManager, serviceGrants: serviceGrantManager, adminBootstrap: adminBootstrap, localLogin: localLogin}
	binder := &applicationRuntimeBinder{
		repository: repository, auditStore: auditStore, securityRecorder: securityRecorder, security: security, eventRuntime: eventRuntime, broker: broker,
		notificationChannels: append([]notification.Channel(nil), configuration.NotificationChannels...), dueReminder: configuration.DueReminder,
		bootstrapMode: configuration.BootstrapMode, application: application,
	}
	var legacyGRPCFallback grpc.UnaryServerInterceptor
	if authenticator != nil {
		legacyGRPCFallback = authenticator.GRPCUnaryServerInterceptor()
	}
	started, err := runtimehost.Bootstrap(ctx, runtimehost.Options[generatedassembly.Applications]{
		HTTPListenAddress: configuration.HTTPAddress, GRPCListenAddress: configuration.GRPCAddress, HTTPMiddleware: httpMiddleware,
		GRPCServerOptions: []grpc.ServerOption{grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, serviceCredentialManager.GRPCUnaryServerInterceptor(legacyGRPCFallback))},
		HealthPath: "/health", DiagnosticsPath: "/__yunka/diagnostics",
		Bootstrap: func(bootstrapCtx context.Context, runtime runtimehost.Runtime) (kernel.BootstrapResult[generatedassembly.Applications], error) {
			return generatedassembly.Bootstrap(bootstrapCtx, generatedassembly.BootstrapOptions{
				Platform: runtimePlatform, AdditionalModules: []modulecatalog.Descriptor{eventRuntime.Descriptor()},
				BindRuntimeWithCapabilities: func(bindCtx context.Context, provider *platform.Provider, capabilities modulecatalog.CapabilitySet) (generatedassembly.RuntimeBindings, error) {
					return binder.Bind(bindCtx, provider, capabilities, runtime.HTTP)
				},
				Transports: generatedassembly.TransportBindings{RPC: runtime.RPC}, RuntimeComponents: runtime.RuntimeComponents,
			})
		},
	})
	if err != nil {
		_ = eventRuntime.Shutdown(context.Background())
		return nil, fmt.Errorf("bootstrap Yunka runtime host: %w", err)
	}
	application.app = started.App
	application.httpAddress = started.HTTPAddress
	application.grpcAddress = started.GRPCAddress
	return application, nil
}

func configuredAuthorization(ctx context.Context, configuration Config, repository *delivery.SQLiteRepository) (authz.Authorizer, authz.GuardResolver, error) {
	if repository == nil || repository.Database() == nil {
		return nil, nil, errors.New("delivery authorization repository is required")
	}
	if configuration.RuntimeEnvironment == RuntimeEnvironmentDevelopment {
		authorizer, err := localauth.NewAuthorizer()
		if err != nil {
			return nil, nil, fmt.Errorf("create local authorizer: %w", err)
		}
		return authorizer, nil, nil
	}
	humanResolver, err := humanauthz.NewGrantResolver(repository.Database())
	if err != nil {
		return nil, nil, fmt.Errorf("create human grant resolver: %w", err)
	}
	serviceResolver, err := serviceauthz.NewGrantResolver(repository.Database())
	if err != nil {
		return nil, nil, fmt.Errorf("create service grant resolver: %w", err)
	}
	resolver, err := principalauthz.New(humanResolver, serviceResolver)
	if err != nil {
		return nil, nil, fmt.Errorf("compose production grant resolver: %w", err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(resolver)
	if err != nil {
		return nil, nil, fmt.Errorf("create human grant authorizer: %w", err)
	}
	memberGuard, err := localmemberadmin.NewOperationGuard(repository.Database())
	if err != nil {
		return nil, nil, fmt.Errorf("create local member admin operation guard: %w", err)
	}
	deliveryGuard, err := deliveryauthz.NewOperationGuard(repository, repository.Database())
	if err != nil {
		return nil, nil, fmt.Errorf("create delivery operation guard: %w", err)
	}
	return authorizer, guardResolverMux{memberGuard.GuardResolver(), deliveryGuard.GuardResolver()}, nil
}

func configuredLocalLogin(configuration Config, database *sql.DB, credentials *localcredential.SQLiteRepository, auditStore *audit.SQLiteStore, transactions *localtx.SQLiteFactory) (*locallogin.Manager, error) {
	encodedKey := strings.TrimSpace(configuration.LocalAuthJWTSigningKey)
	if encodedKey == "" {
		return nil, nil
	}
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil || base64.RawURLEncoding.EncodeToString(key) != encodedKey {
		return nil, errors.New("local auth JWT signing key must be canonical base64url")
	}
	manager, err := locallogin.NewManager(database, credentials, auditStore,
		operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: transactions}), locallogin.DefaultConfig(key))
	if err != nil {
		return nil, fmt.Errorf("configure local authentication manager: %w", err)
	}
	return manager, nil
}

func validateStartupPolicy(configuration Config) error {
	switch configuration.RuntimeEnvironment {
	case RuntimeEnvironmentDevelopment, RuntimeEnvironmentProduction:
	default:
		return errors.New("runtime environment must be explicitly development or production")
	}
	switch configuration.BootstrapMode {
	case "", BootstrapModeDisabled, BootstrapModeExample:
	default:
		return errors.New("bootstrap mode is invalid")
	}
	if configuration.BootstrapMode == BootstrapModeExample && configuration.RuntimeEnvironment != RuntimeEnvironmentDevelopment {
		return errors.New("example bootstrap requires the development runtime environment")
	}
	if configuration.RuntimeEnvironment == RuntimeEnvironmentProduction && configuration.LegacyLocalAPIKeyEnabled {
		return errors.New("legacy local API-key authentication is disabled in production")
	}
	if configuration.RuntimeEnvironment == RuntimeEnvironmentProduction && configuration.AllowInsecureServiceCredentialsForDevelopment {
		return errors.New("insecure service credentials are disabled in production")
	}
	return nil
}

func validateBFFConfiguration(configuration Config) error {
	organizationID := strings.TrimSpace(configuration.BFFOrganizationID)
	encodedKey := strings.TrimSpace(configuration.BFFAssertionKey)
	if organizationID == "" && encodedKey == "" {
		if configuration.RuntimeEnvironment == RuntimeEnvironmentProduction {
			return errors.New("production BFF organization and assertion key are required")
		}
		return nil
	}
	if organizationID == "" || encodedKey == "" {
		return errors.New("BFF organization and assertion key must be configured together")
	}
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil || len(key) < 32 || base64.RawURLEncoding.EncodeToString(key) != encodedKey {
		return errors.New("BFF assertion key must be base64url and at least 32 bytes")
	}
	return nil
}

func validateLocalAuthConfiguration(configuration Config) error {
	encodedKey := strings.TrimSpace(configuration.LocalAuthJWTSigningKey)
	if encodedKey == "" {
		return nil
	}
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil || len(key) < 32 || base64.RawURLEncoding.EncodeToString(key) != encodedKey {
		return errors.New("local auth JWT signing key must be canonical base64url and at least 32 bytes")
	}
	return nil
}

func configuredHTTPMiddleware(authenticator *localauth.Authenticator, resolver *identitybinding.Resolver, recorder *audit.SecurityRecorder, configuration Config) (func(http.Handler) http.Handler, error) {
	organizationID := strings.TrimSpace(configuration.BFFOrganizationID)
	encodedKey := strings.TrimSpace(configuration.BFFAssertionKey)
	if organizationID == "" && encodedKey == "" {
		if authenticator == nil {
			return nil, errors.New("legacy local API-key authentication is not configured")
		}
		return bffhttp.APIKeyTraceMiddleware(authenticator, recorder), nil
	}
	key, err := base64.RawURLEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, errors.New("BFF assertion key must be base64url and at least 32 bytes")
	}
	verifier, err := bffassertion.NewVerifier(bffassertion.Config{Key: key})
	if err != nil {
		return nil, fmt.Errorf("configure BFF assertion verifier: %w", err)
	}
	middleware, err := bffhttp.NewMiddleware(bffhttp.Config{
		Authenticator: authenticator, AuditRecorder: recorder, Verifier: verifier, Resolver: resolver,
		OrganizationID: organizationID, AllowLegacyFallback: configuration.RuntimeEnvironment == RuntimeEnvironmentDevelopment,
	})
	if err != nil {
		return nil, fmt.Errorf("configure BFF HTTP middleware: %w", err)
	}
	return middleware.HTTPMiddleware, nil
}

func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	return net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func (application *Application) Operations() *deliveryapplication.Operations {
	if application == nil { return nil }
	return application.operations
}

func (application *Application) AdministratorBootstrap() *localbootstrap.Manager {
	if application == nil { return nil }
	return application.adminBootstrap
}

func (application *Application) MemberAdministration() *localmemberadmin.Manager {
	if application == nil { return nil }
	return application.memberAdmin
}

func (application *Application) LocalAuthentication() *locallogin.Manager {
	if application == nil { return nil }
	return application.localLogin
}

func (application *Application) ServiceCredentials() *serviceauth.Manager {
	if application == nil { return nil }
	return application.serviceAuth
}

func (application *Application) ServiceGrants() *serviceauthz.Manager {
	if application == nil { return nil }
	return application.serviceGrants
}

func (application *Application) HTTPAddress() string {
	if application == nil { return "" }
	return application.httpAddress
}

func (application *Application) GRPCAddress() string {
	if application == nil { return "" }
	return application.grpcAddress
}

func (application *Application) Close(ctx context.Context) error {
	if application == nil || application.app == nil { return nil }
	return application.app.Shutdown(ctx)
}

func seedExample(ctx context.Context, operations *deliveryapplication.Operations) error {
	if operations == nil {
		return errors.New("seed delivery operations are not configured")
	}
	bootstrapContext := identity.WithPrincipal(ctx, identity.Principal{
		Subject: "bootstrap/seed", UserID: "bootstrap/seed", TenantID: localauth.DevelopmentTenantID,
		Roles: []string{localauth.RoleLocalAdmin}, AuthMethod: identity.AuthMethodAPIKey, Authenticated: true,
	})
	items, err := operations.List(bootstrapContext)
	if err != nil { return fmt.Errorf("inspect existing delivery items: %w", err) }
	if len(items) > 0 { return nil }
	item, err := operations.Create(bootstrapContext, delivery.CreateInput{
		Title: "样例：设备 OTA 发布验收", Board: delivery.BoardResearchDelivery, Type: "release", Owner: "待分配", Priority: delivery.PriorityP0,
		Plan: "验证分组灰度、回滚演练和发布验收证据。", Solution: "按设备分组推进灰度发布，并把回滚结果作为发布门禁证据。", IsSample: true,
	})
	if err != nil { return fmt.Errorf("seed sample delivery item: %w", err) }
	_, err = operations.UpdateContext(bootstrapContext, item.ID, item.Revision, delivery.ContextUpdate{Decision: &delivery.Decision{
		Title: "将回滚演练纳入发布门禁", Context: "OTA 发布存在设备型号和网络差异。", Outcome: "发布前必须附上灰度与回滚证据。", Consequences: "发布负责人需要维护证据链接。",
	}})
	if err != nil { return fmt.Errorf("seed sample decision: %w", err) }
	return nil
}
