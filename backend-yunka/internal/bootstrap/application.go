package bootstrap

import (
	"context"
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
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitybinding"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/obsidian"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/principalauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	"google.golang.org/grpc"
	"github.com/hvritual/yunka.io/framework/core"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/event"
	frameworkoutbox "github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/kernel"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/framework/runtimehost"
	"github.com/hvritual/yunka.io/gateway/authz"
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
	repository    *delivery.SQLiteRepository
	service       *delivery.Service
	adapter       deliveryapplication.DeliveryService
	operations    *deliveryapplication.Operations
	configOps     *configapplication.Operations
	executor      operation.Executor
	outbox        *localoutbox.SQLiteStore
	broker        *event.LocalBroker
	subscriptions []event.Subscription
	dispatcher    *frameworkoutbox.Dispatcher
	reminders     *delivery.DueReminderScheduler
	serviceAuth   *serviceauth.Manager
	serviceGrants *serviceauthz.Manager
	app           *core.App

	httpAddress string
	grpcAddress string
}

// New constructs the business services and then delegates process ownership to
// Yunka runtimehost. The host owns HTTP/gRPC listeners, health, diagnostics and
// lifecycle; this package owns only the delivery assembly and persistence.
func New(ctx context.Context, configuration Config) (*Application, error) {
	if err := validateStartupPolicy(configuration); err != nil {
		return nil, err
	}
	if err := validateBFFConfiguration(configuration); err != nil {
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
	service := delivery.NewService(repository, exporter, delivery.NewTransactionalOutboxStager(outboxStore))
	reminders, err := delivery.NewDueReminderScheduler(service, outboxStore, configuration.DueReminder)
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure due reminder scheduler: %w", err)
	}
	adapter := deliveryapplication.NewAdapter(service)
	auditedAdapter, err := deliveryapplication.NewAuditedDeliveryService(
		adapter,
		auditStore,
		deliveryapplication.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure audited delivery application: %w", err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(repository.Database()),
	}), securityRecorder)
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure security audit executor: %w", err)
	}
	configStore, err := configrevision.NewSQLiteStore(repository.Database())
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure config revision store: %w", err)
	}
	configOps, err := configapplication.New(configStore, auditStore, executor, configapplication.WithOutbox(outboxStore))
	if err != nil {
		_ = repository.Close()
		return nil, fmt.Errorf("configure config revision operations: %w", err)
	}
	operations := deliveryapplication.NewOperations(auditedAdapter, executor, service).WithNotificationReader(notificationStore)
	if configuration.BootstrapMode == BootstrapModeExample {
		if err := seedExample(ctx, operations); err != nil {
			_ = repository.Close()
			return nil, err
		}
	}
	broker := event.NewLocalBroker(nil)
	subscriptions := make([]event.Subscription, 0, 2)
	obsidianSubscription, err := broker.Subscribe(ctx, "delivery.work-item", obsidian.NewProjectionConsumer(service).Handle)
	if err != nil {
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("subscribe local Obsidian projection: %w", err)
	}
	subscriptions = append(subscriptions, obsidianSubscription)
	channels := make([]notification.Channel, 0, 1+len(configuration.NotificationChannels))
	channels = append(channels, notification.NewLocalInboxChannel(notificationStore))
	channels = append(channels, configuration.NotificationChannels...)
	router, err := notification.NewRouter(channels...)
	if err != nil {
		_ = closeSubscriptions(subscriptions)
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("configure local notification router: %w", err)
	}
	notificationSubscription, err := broker.Subscribe(ctx, notification.DeliveryTopic, notification.NewConsumer(router).Handle)
	if err != nil {
		_ = closeSubscriptions(subscriptions)
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("subscribe local notification delivery: %w", err)
	}
	subscriptions = append(subscriptions, notificationSubscription)
	dispatcher, err := frameworkoutbox.NewDispatcher(outboxStore, broker, frameworkoutbox.DispatcherConfig{
		WorkerID:       "iot-delivery-local-outbox",
		PollInterval:   50 * time.Millisecond,
		BatchSize:      10,
		Concurrency:    1,
		LeaseDuration:  30 * time.Second,
		PublishTimeout: 2 * time.Second,
		RetryBase:      100 * time.Millisecond,
		RetryMax:       2 * time.Second,
	})
	if err != nil {
		_ = closeSubscriptions(subscriptions)
		_ = broker.Close()
		_ = repository.Close()
		return nil, fmt.Errorf("create local outbox dispatcher: %w", err)
	}

	application := &Application{
		repository:    repository,
		service:       service,
		adapter:       auditedAdapter,
		operations:    operations,
		configOps:     configOps,
		executor:      executor,
		outbox:        outboxStore,
		broker:        broker,
		subscriptions: subscriptions,
		dispatcher:    dispatcher,
		reminders:     reminders,
		serviceAuth:   serviceCredentialManager,
		serviceGrants: serviceGrantManager,
	}
	var legacyGRPCFallback grpc.UnaryServerInterceptor
	if authenticator != nil {
		legacyGRPCFallback = authenticator.GRPCUnaryServerInterceptor()
	}
	started, err := runtimehost.Bootstrap(ctx, runtimehost.Options[generatedassembly.Applications]{
		HTTPListenAddress: configuration.HTTPAddress,
		GRPCListenAddress: configuration.GRPCAddress,
		HTTPMiddleware:    httpMiddleware,
		GRPCServerOptions: []grpc.ServerOption{grpc.ChainUnaryInterceptor(deliveryrpc.RevisionErrorUnaryServerInterceptor, serviceCredentialManager.GRPCUnaryServerInterceptor(legacyGRPCFallback))},
		HealthPath:        "/health",
		DiagnosticsPath:   "/__yunka/diagnostics",
		Bootstrap: func(bootstrapCtx context.Context, runtime runtimehost.Runtime) (kernel.BootstrapResult[generatedassembly.Applications], error) {
			components := append([]core.RuntimeComponent{
				application.sqliteRuntimeComponent(),
				application.outboxBrokerRuntimeComponent(),
				application.outboxDispatcherRuntimeComponent(),
				application.dueReminderRuntimeComponent(),
			}, runtime.RuntimeComponents...)
			if application == nil || application.adapter == nil || application.operations == nil || application.executor == nil {
				return kernel.BootstrapResult[generatedassembly.Applications]{}, errors.New("delivery application is not configured")
			}
			result, bootstrapErr := generatedassembly.Bootstrap(bootstrapCtx, generatedassembly.BootstrapOptions{
				Factories: applicationFactories{deliveryManagement: application.adapter},
				Executor:  application.executor,
				Transports: generatedassembly.TransportBindings{
					RPC: runtime.RPC,
				},
				RuntimeComponents: components,
			})
			if bootstrapErr != nil {
				return kernel.BootstrapResult[generatedassembly.Applications]{}, bootstrapErr
			}
			httpapi.Register(runtime.HTTP, application.operations)
			return result, nil
		},
	})
	if err != nil {
		_ = closeSubscriptions(subscriptions)
		_ = broker.Close()
		_ = repository.Close()
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
	guard, err := deliveryauthz.NewOperationGuard(repository, repository.Database())
	if err != nil {
		return nil, nil, fmt.Errorf("create delivery operation guard: %w", err)
	}
	return authorizer, guard.GuardResolver(), nil
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
		Authenticator:       authenticator,
		AuditRecorder:       recorder,
		Verifier:            verifier,
		Resolver:            resolver,
		OrganizationID:      organizationID,
		AllowLegacyFallback: configuration.RuntimeEnvironment == RuntimeEnvironmentDevelopment,
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

type applicationFactories struct {
	deliveryManagement deliveryapplication.DeliveryService
}

func (factories applicationFactories) BuildDeliveryManagement(generatedassembly.DeliveryManagementDependencies) (deliveryapplication.DeliveryService, error) {
	if factories.deliveryManagement == nil {
		return nil, errors.New("delivery management application adapter is not configured")
	}
	return factories.deliveryManagement, nil
}

// Operations exposes the application-use-case boundary for local adapters
// such as the stdio MCP server. Callers must still establish an authenticated
// principal; Operations keeps Yunka execution security and transactions.
func (application *Application) Operations() *deliveryapplication.Operations {
	if application == nil {
		return nil
	}
	return application.operations
}

// ServiceCredentials is the intentionally in-process management port for
// issuing, rotating, revoking, and disabling service credentials. S0-02-07
// exposes no service-credential write transport, so such writes cannot bypass
// the generated Operation Plan, Executor, authorization, transaction, and
// Outbox boundary required for future remote management APIs.
func (application *Application) ServiceCredentials() *serviceauth.Manager {
	if application == nil {
		return nil
	}
	return application.serviceAuth
}

// ServiceGrants is the intentionally in-process management port for explicit
// service-account grants. This slice defines no remote grant-management API.
func (application *Application) ServiceGrants() *serviceauthz.Manager {
	if application == nil {
		return nil
	}
	return application.serviceGrants
}

func (application *Application) HTTPAddress() string {
	if application == nil {
		return ""
	}
	return application.httpAddress
}

func (application *Application) GRPCAddress() string {
	if application == nil {
		return ""
	}
	return application.grpcAddress
}

func (application *Application) Close(ctx context.Context) error {
	if application == nil {
		return nil
	}
	if application.app != nil {
		return application.app.Shutdown(ctx)
	}
	if application.repository != nil {
		return application.repository.Close()
	}
	return nil
}

func (application *Application) sqliteRuntimeComponent() core.RuntimeComponent {
	return core.RuntimeComponent{
		Name: "delivery-sqlite",
		StartFunc: func(ctx context.Context) error {
			return application.repository.Ping(ctx)
		},
		HealthFunc: func(ctx context.Context) error {
			return application.repository.Ping(ctx)
		},
		ShutdownFunc: func(context.Context) error {
			return application.repository.Close()
		},
	}
}

func (application *Application) outboxBrokerRuntimeComponent() core.RuntimeComponent {
	return core.RuntimeComponent{
		Name: "delivery-sqlite-outbox-broker",
		StartFunc: func(context.Context) error {
			if application == nil || application.broker == nil {
				return errors.New("local event broker is not configured")
			}
			return nil
		},
		HealthFunc: func(context.Context) error {
			if application == nil || application.broker == nil {
				return errors.New("local event broker is not configured")
			}
			return nil
		},
		ShutdownFunc: func(context.Context) error {
			if application == nil {
				return nil
			}
			var closeErr error
			closeErr = errors.Join(closeErr, closeSubscriptions(application.subscriptions))
			if application.broker != nil {
				closeErr = errors.Join(closeErr, application.broker.Close())
			}
			return closeErr
		},
	}
}

func closeSubscriptions(subscriptions []event.Subscription) error {
	var closeErr error
	for index := len(subscriptions) - 1; index >= 0; index-- {
		if subscriptions[index] != nil {
			closeErr = errors.Join(closeErr, subscriptions[index].Close())
		}
	}
	return closeErr
}

func (application *Application) outboxDispatcherRuntimeComponent() core.RuntimeComponent {
	return core.RuntimeComponent{
		Name: "delivery-sqlite-outbox-dispatcher",
		StartFunc: func(ctx context.Context) error {
			if application == nil || application.dispatcher == nil {
				return errors.New("local outbox dispatcher is not configured")
			}
			return application.dispatcher.Start(ctx)
		},
		HealthFunc: func(ctx context.Context) error {
			if application == nil || application.dispatcher == nil {
				return errors.New("local outbox dispatcher is not configured")
			}
			return application.dispatcher.Health(ctx)
		},
		ShutdownFunc: func(ctx context.Context) error {
			if application == nil || application.dispatcher == nil {
				return nil
			}
			return application.dispatcher.Shutdown(ctx)
		},
	}
}

func (application *Application) dueReminderRuntimeComponent() core.RuntimeComponent {
	return core.RuntimeComponent{
		Name: "delivery-due-reminders",
		StartFunc: func(ctx context.Context) error {
			if application == nil || application.reminders == nil {
				return errors.New("due reminder scheduler is not configured")
			}
			return application.reminders.Start(ctx)
		},
		HealthFunc: func(ctx context.Context) error {
			if application == nil || application.reminders == nil {
				return errors.New("due reminder scheduler is not configured")
			}
			return application.reminders.Health(ctx)
		},
		ShutdownFunc: func(ctx context.Context) error {
			if application == nil || application.reminders == nil {
				return nil
			}
			return application.reminders.Stop(ctx)
		},
	}
}

func seedExample(ctx context.Context, operations *deliveryapplication.Operations) error {
	if operations == nil {
		return errors.New("seed delivery operations are not configured")
	}
	bootstrapContext := identity.WithPrincipal(ctx, identity.Principal{
		Subject:       "bootstrap/seed",
		UserID:        "bootstrap/seed",
		TenantID:      localauth.DevelopmentTenantID,
		Roles:         []string{localauth.RoleLocalAdmin},
		AuthMethod:    identity.AuthMethodAPIKey,
		Authenticated: true,
	})
	items, err := operations.List(bootstrapContext)
	if err != nil {
		return fmt.Errorf("inspect existing delivery items: %w", err)
	}
	if len(items) > 0 {
		return nil
	}
	item, err := operations.Create(bootstrapContext, delivery.CreateInput{
		Title:    "样例：设备 OTA 发布验收",
		Board:    delivery.BoardResearchDelivery,
		Type:     "release",
		Owner:    "待分配",
		Priority: delivery.PriorityP0,
		Plan:     "验证分组灰度、回滚演练和发布验收证据。",
		Solution: "按设备分组推进灰度发布，并把回滚结果作为发布门禁证据。",
		IsSample: true,
	})
	if err != nil {
		return fmt.Errorf("seed sample delivery item: %w", err)
	}
	_, err = operations.UpdateContext(bootstrapContext, item.ID, item.Revision, delivery.ContextUpdate{Decision: &delivery.Decision{
		Title:        "将回滚演练纳入发布门禁",
		Context:      "OTA 发布存在设备型号和网络差异。",
		Outcome:      "发布前必须附上灰度与回滚证据。",
		Consequences: "发布负责人需要维护证据链接。",
	}})
	if err != nil {
		return fmt.Errorf("seed sample decision: %w", err)
	}
	return nil
}
