// Package deliveryruntime owns the local delivery event pipeline as one Yunka
// App-scoped module. It exports only typed construction dependencies and never
// retains request identity, request contexts, active transactions, or
// request-scoped repositories.
package deliveryruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/event"
	frameworkoutbox "github.com/hvritual/yunka.io/framework/event/outbox"
)

const (
	ModuleName                 = "delivery-event-runtime"
	OutboxCapabilityName       = "delivery.outbox"
	NotificationCapabilityName = "delivery.notifications"
	ProjectionCapabilityName   = "delivery.projection"
)

// Outbox is the complete local durable and transactional event-store view.
type Outbox interface {
	frameworkoutbox.Store
	frameworkoutbox.TransactionalStore
}

// Notifications is the durable local notification read model.
type Notifications interface {
	notification.Reader
}

// Projection is the constructor dependency used by Delivery to refresh the
// Obsidian materialized view after reliable event delivery.
type Projection interface {
	delivery.Exporter
}

var (
	OutboxCapability = modulecatalog.MustCapabilityKey[Outbox](
		OutboxCapabilityName,
		"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryruntime",
		"Outbox",
	)
	NotificationCapability = modulecatalog.MustCapabilityKey[Notifications](
		NotificationCapabilityName,
		"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryruntime",
		"Notifications",
	)
	ProjectionCapability = modulecatalog.MustCapabilityKey[Projection](
		ProjectionCapabilityName,
		"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryruntime",
		"Projection",
	)
)

type databaseRuntime interface {
	Ping(context.Context) error
	Close() error
}

type dispatcherRuntime interface {
	Start(context.Context) error
	Health(context.Context) error
	Shutdown(context.Context) error
}

type reminderRuntime interface {
	Start(context.Context) error
	Health(context.Context) error
	Stop(context.Context) error
}

type brokerRuntime interface {
	Close() error
}

// Dependencies are fully prepared consumer resources. Descriptor construction
// remains side-effect free; core.App becomes their sole lifecycle owner once
// the descriptor is composed.
type Dependencies struct {
	Database      databaseRuntime
	Transactions  localtx.Factory
	Outbox        Outbox
	Notifications Notifications
	Projection    Projection
	Dispatcher    dispatcherRuntime
	Reminders     reminderRuntime
	Broker        brokerRuntime
	Subscriptions []event.Subscription
}

type Module struct {
	database      databaseRuntime
	transactions  localtx.Factory
	outbox        Outbox
	notifications Notifications
	projection    Projection
	dispatcher    dispatcherRuntime
	reminders     reminderRuntime
	broker        brokerRuntime
	subscriptions []event.Subscription

	shutdownMu sync.Mutex
	shutdown   bool
}

func New(dependencies Dependencies) (*Module, error) {
	if dependencies.Database == nil || dependencies.Transactions == nil || dependencies.Outbox == nil ||
		dependencies.Notifications == nil || dependencies.Projection == nil || dependencies.Dispatcher == nil ||
		dependencies.Reminders == nil || dependencies.Broker == nil {
		return nil, errors.New("delivery event runtime dependencies are incomplete")
	}
	for index, subscription := range dependencies.Subscriptions {
		if subscription == nil {
			return nil, fmt.Errorf("delivery event runtime subscription %d is nil", index)
		}
	}
	return &Module{
		database: dependencies.Database, transactions: dependencies.Transactions,
		outbox: dependencies.Outbox, notifications: dependencies.Notifications, projection: dependencies.Projection,
		dispatcher: dependencies.Dispatcher, reminders: dependencies.Reminders, broker: dependencies.Broker,
		subscriptions: append([]event.Subscription(nil), dependencies.Subscriptions...),
	}, nil
}

func (*Module) Name() string { return ModuleName }

// Descriptor declares all exported values explicitly. The current module
// manifest schema cannot generate Provides, so this consumer wrapper is the
// canonical handwritten composition seam and performs no I/O.
func (module *Module) Descriptor() modulecatalog.Descriptor {
	return modulecatalog.Descriptor{
		Name:    ModuleName,
		Version: "v0.1.0",
		Provides: []modulecatalog.CapabilityContract{
			localtx.TransactionFactoryCapability.Contract(),
			OutboxCapability.Contract(),
			NotificationCapability.Contract(),
			ProjectionCapability.Contract(),
		},
		Build: func(modulecatalog.BuildContext) (modulecatalog.Instance, error) {
			if module == nil {
				return nil, errors.New("delivery event runtime module is required")
			}
			return module, nil
		},
	}
}

func (module *Module) ExportCapabilities() []modulecatalog.CapabilityExport {
	if module == nil {
		return nil
	}
	return []modulecatalog.CapabilityExport{
		localtx.TransactionFactoryCapability.Export(module.transactions),
		OutboxCapability.Export(module.outbox),
		NotificationCapability.Export(module.notifications),
		ProjectionCapability.Export(module.projection),
	}
}

func (module *Module) Start(ctx context.Context) error {
	if module == nil {
		return errors.New("delivery event runtime is not configured")
	}
	module.shutdownMu.Lock()
	stopped := module.shutdown
	module.shutdownMu.Unlock()
	if stopped {
		return errors.New("delivery event runtime is already stopped")
	}
	if err := module.database.Ping(ctx); err != nil {
		return fmt.Errorf("delivery event runtime database: %w", err)
	}
	if err := module.dispatcher.Start(ctx); err != nil {
		return fmt.Errorf("delivery event runtime dispatcher: %w", err)
	}
	if err := module.reminders.Start(ctx); err != nil {
		return fmt.Errorf("delivery event runtime reminders: %w", err)
	}
	return nil
}

func (module *Module) Health(ctx context.Context) error {
	if module == nil {
		return errors.New("delivery event runtime is not configured")
	}
	return errors.Join(
		module.database.Ping(ctx),
		module.dispatcher.Health(ctx),
		module.reminders.Health(ctx),
	)
}

func (module *Module) Shutdown(ctx context.Context) error {
	if module == nil {
		return nil
	}
	module.shutdownMu.Lock()
	if module.shutdown {
		module.shutdownMu.Unlock()
		return nil
	}
	module.shutdown = true
	module.shutdownMu.Unlock()

	var shutdownErr error
	shutdownErr = errors.Join(shutdownErr, module.reminders.Stop(ctx))
	shutdownErr = errors.Join(shutdownErr, module.dispatcher.Shutdown(ctx))
	for index := len(module.subscriptions) - 1; index >= 0; index-- {
		shutdownErr = errors.Join(shutdownErr, module.subscriptions[index].Close())
	}
	shutdownErr = errors.Join(shutdownErr, module.broker.Close())
	shutdownErr = errors.Join(shutdownErr, module.database.Close())
	return shutdownErr
}
