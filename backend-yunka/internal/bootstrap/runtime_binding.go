package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	generatedassembly "github.com/hvritual/iot-delivery-system/backend-yunka/internal/assembly"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configapplication"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configrevision"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliveryapplication "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryruntime"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/obsidian"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/framework/platform"
)

// applicationRuntimeBinder owns consumer construction facts only. Generated
// Assembly invokes it after App modules have exported capabilities and before
// either HTTP or gRPC begins serving.
type applicationRuntimeBinder struct {
	bindMu               sync.Mutex
	bound                bool
	repository           *delivery.SQLiteRepository
	auditStore           *audit.SQLiteStore
	securityRecorder     *audit.SecurityRecorder
	security             operation.SecurityPhase
	eventRuntime         *deliveryruntime.Module
	broker               *event.LocalBroker
	notificationChannels []notification.Channel
	dueReminder          delivery.DueReminderConfig
	bootstrapMode        BootstrapMode
	application          *Application
}

func (binder *applicationRuntimeBinder) Bind(
	ctx context.Context,
	provider *platform.Provider,
	capabilities modulecatalog.CapabilitySet,
	httpMux *http.ServeMux,
) (generatedassembly.RuntimeBindings, error) {
	if binder == nil || binder.repository == nil || binder.auditStore == nil || binder.securityRecorder == nil ||
		binder.security == nil || binder.eventRuntime == nil || binder.broker == nil || binder.application == nil {
		return generatedassembly.RuntimeBindings{}, errors.New("delivery runtime binder is not configured")
	}
	if provider == nil {
		return generatedassembly.RuntimeBindings{}, errors.New("delivery runtime Platform is required")
	}
	if httpMux == nil {
		return generatedassembly.RuntimeBindings{}, errors.New("delivery runtime HTTP registrar is required")
	}
	binder.bindMu.Lock()
	defer binder.bindMu.Unlock()
	if binder.bound {
		return generatedassembly.RuntimeBindings{}, errors.New("delivery runtime is already bound")
	}

	dependencies, err := resolveApplicationCapabilities(capabilities)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, err
	}
	stager := delivery.NewTransactionalOutboxStager(dependencies.DeliveryOutbox)
	service := delivery.NewRootTransactionalService(
		binder.repository,
		dependencies.DeliveryProjection,
		stager,
	)
	adapter := deliveryapplication.NewAdapter(service, dependencies.DeliveryNotifications)
	auditedAdapter, err := deliveryapplication.NewAuditedDeliveryService(
		adapter,
		binder.auditStore,
		deliveryapplication.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure audited delivery application: %w", err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(binder.security, operation.ExecutorOptions{
		Transactions: dependencies.SqliteTransactionFactory,
	}), binder.securityRecorder)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure security audit executor: %w", err)
	}
	configStore, err := configrevision.NewSQLiteStore(binder.repository.Database())
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure config revision store: %w", err)
	}
	configOps, err := configapplication.New(configStore, binder.auditStore, executor, configapplication.WithOutbox(dependencies.DeliveryOutbox))
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure config revision operations: %w", err)
	}
	credentialStore, err := localcredential.NewSQLiteRepository(binder.repository.Database())
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure local member credential store: %w", err)
	}
	memberAdmin, err := localmemberadmin.NewManager(
		binder.repository.Database(),
		credentialStore,
		binder.auditStore,
		dependencies.DeliveryOutbox,
		executor,
	)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure local member administration: %w", err)
	}
	projectRoleAdmin, err := localprojectroleadmin.NewManager(
		binder.repository.Database(),
		binder.repository,
		binder.auditStore,
		dependencies.DeliveryOutbox,
		executor,
	)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure project role administration: %w", err)
	}
	operations := deliveryapplication.NewOperations(auditedAdapter, executor, service)
	if binder.bootstrapMode == BootstrapModeExample {
		if err := seedExample(ctx, operations); err != nil {
			return generatedassembly.RuntimeBindings{}, err
		}
	}

	reminders, err := delivery.NewDueReminderScheduler(service, dependencies.DeliveryOutbox, binder.dueReminder)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, fmt.Errorf("configure due reminder scheduler: %w", err)
	}
	subscriptions, err := binder.subscribeApplication(ctx, service, dependencies.DeliveryNotifications)
	if err != nil {
		return generatedassembly.RuntimeBindings{}, err
	}
	if err := binder.eventRuntime.BindApplication(deliveryruntime.ApplicationDependencies{
		Reminders: reminders, Subscriptions: subscriptions,
	}); err != nil {
		return generatedassembly.RuntimeBindings{}, errors.Join(
			fmt.Errorf("bind delivery event runtime application: %w", err),
			closeSubscriptions(subscriptions),
		)
	}

	// Compatibility HTTP routes must be complete before generated Bootstrap
	// registers gRPC and starts App-owned transport components.
	httpapi.Register(httpMux, operations)
	binder.application.operations = operations
	binder.application.configOps = configOps
	binder.application.memberAdmin = memberAdmin
	binder.application.projectRoleAdmin = projectRoleAdmin
	binder.bound = true
	return generatedassembly.RuntimeBindings{
		Factories: applicationFactories{deliveryManagement: auditedAdapter},
		Executor:  executor,
	}, nil
}

func resolveApplicationCapabilities(capabilities modulecatalog.CapabilitySet) (generatedassembly.DeliveryManagementDependencies, error) {
	transactions, err := modulecatalog.ResolveCapability(capabilities, localtx.TransactionFactoryCapability)
	if err != nil {
		return generatedassembly.DeliveryManagementDependencies{}, fmt.Errorf("resolve SQLite transaction factory capability: %w", err)
	}
	outboxStore, err := modulecatalog.ResolveCapability(capabilities, deliveryruntime.OutboxCapability)
	if err != nil {
		return generatedassembly.DeliveryManagementDependencies{}, fmt.Errorf("resolve delivery Outbox capability: %w", err)
	}
	notifications, err := modulecatalog.ResolveCapability(capabilities, deliveryruntime.NotificationCapability)
	if err != nil {
		return generatedassembly.DeliveryManagementDependencies{}, fmt.Errorf("resolve delivery notification capability: %w", err)
	}
	projection, err := modulecatalog.ResolveCapability(capabilities, deliveryruntime.ProjectionCapability)
	if err != nil {
		return generatedassembly.DeliveryManagementDependencies{}, fmt.Errorf("resolve delivery projection capability: %w", err)
	}
	return generatedassembly.DeliveryManagementDependencies{
		DeliveryNotifications:    notifications,
		DeliveryOutbox:           outboxStore,
		DeliveryProjection:       projection,
		SqliteTransactionFactory: transactions,
	}, nil
}

func (binder *applicationRuntimeBinder) subscribeApplication(
	ctx context.Context,
	service *delivery.Service,
	notifications deliveryruntime.Notifications,
) ([]event.Subscription, error) {
	subscriptions := make([]event.Subscription, 0, 2)
	projectionSubscription, err := binder.broker.Subscribe(ctx, "delivery.work-item", obsidian.NewProjectionConsumer(service).Handle)
	if err != nil {
		return nil, fmt.Errorf("subscribe local Obsidian projection: %w", err)
	}
	subscriptions = append(subscriptions, projectionSubscription)
	channels := make([]notification.Channel, 0, 1+len(binder.notificationChannels))
	channels = append(channels, notification.NewLocalInboxChannel(notifications))
	channels = append(channels, binder.notificationChannels...)
	router, err := notification.NewRouter(channels...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("configure local notification router: %w", err), closeSubscriptions(subscriptions))
	}
	notificationSubscription, err := binder.broker.Subscribe(ctx, notification.DeliveryTopic, notification.NewConsumer(router).Handle)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("subscribe local notification delivery: %w", err), closeSubscriptions(subscriptions))
	}
	return append(subscriptions, notificationSubscription), nil
}

type applicationFactories struct {
	deliveryManagement deliveryapplication.DeliveryService
}

func (factories applicationFactories) BuildDeliveryManagement(dependencies generatedassembly.DeliveryManagementDependencies) (deliveryapplication.DeliveryService, error) {
	if dependencies.SqliteTransactionFactory == nil {
		return nil, errors.New("SQLite transaction factory capability is not configured")
	}
	if dependencies.DeliveryOutbox == nil || dependencies.DeliveryNotifications == nil || dependencies.DeliveryProjection == nil {
		return nil, errors.New("delivery event runtime capabilities are not configured")
	}
	if factories.deliveryManagement == nil {
		return nil, errors.New("delivery management application adapter is not configured")
	}
	return factories.deliveryManagement, nil
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
