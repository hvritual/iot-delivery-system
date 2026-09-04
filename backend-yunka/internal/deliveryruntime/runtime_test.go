package deliveryruntime

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"github.com/hvritual/yunka.io/framework/core"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/execution"
)

func TestModuleExportsTypedCapabilitiesAndOwnsLifecycleInDependencyOrder(t *testing.T) {
	events := &eventLog{}
	transactions := fakeTransactions{}
	outboxStore := fakeOutbox{}
	notifications := fakeNotifications{}
	projection := fakeProjection{}
	module, err := New(Dependencies{
		Database: &fakeDatabase{events: events}, Transactions: transactions,
		Outbox: outboxStore, Notifications: notifications, Projection: projection,
		Dispatcher: &fakeDispatcher{events: events}, Reminders: &fakeReminders{events: events},
		Broker: &fakeBroker{events: events},
		Subscriptions: []event.Subscription{
			fakeSubscription{name: "projection.close", events: events},
			fakeSubscription{name: "notification.close", events: events},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := modulecatalog.New()
	if err := catalog.Register(module.Descriptor()); err != nil {
		t.Fatal(err)
	}
	application, capabilities, err := core.NewAppWithCapabilities(core.AppOptions{Catalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	for name, resolve := range map[string]func() error{
		"transactions": func() error {
			value, resolveErr := modulecatalog.ResolveCapability(capabilities, localtx.TransactionFactoryCapability)
			if resolveErr == nil && value == nil {
				return errors.New("resolved nil transaction factory")
			}
			return resolveErr
		},
		"outbox": func() error {
			_, resolveErr := modulecatalog.ResolveCapability(capabilities, OutboxCapability)
			return resolveErr
		},
		"notifications": func() error {
			_, resolveErr := modulecatalog.ResolveCapability(capabilities, NotificationCapability)
			return resolveErr
		},
		"projection": func() error {
			_, resolveErr := modulecatalog.ResolveCapability(capabilities, ProjectionCapability)
			return resolveErr
		},
	} {
		if err := resolve(); err != nil {
			t.Fatalf("resolve %s capability: %v", name, err)
		}
	}
	if err := application.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	health := application.Health(t.Context())
	if !health.Ready || len(health.Checks) != 1 || health.Checks[0].Name != "module."+ModuleName {
		t.Fatalf("module health = %#v", health)
	}
	if err := application.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := application.Shutdown(t.Context()); err != nil {
		t.Fatalf("second shutdown must be idempotent: %v", err)
	}
	want := []string{
		"database.ping", "dispatcher.start", "reminder.start",
		"database.ping", "dispatcher.health", "reminder.health",
		"reminder.stop", "dispatcher.shutdown", "notification.close", "projection.close", "broker.close", "database.close",
	}
	if !reflect.DeepEqual(events.values, want) {
		t.Fatalf("lifecycle events = %#v, want %#v", events.values, want)
	}
}

func TestModuleDoesNotRetainRequestContextOrCapabilityResolver(t *testing.T) {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	capabilitySetType := reflect.TypeOf(modulecatalog.EmptyCapabilitySet())
	typeOfModule := reflect.TypeOf(Module{})
	for index := 0; index < typeOfModule.NumField(); index++ {
		field := typeOfModule.Field(index)
		if field.Type.Implements(contextType) || field.Type == capabilitySetType {
			t.Fatalf("module field %s retains forbidden runtime state %s", field.Name, field.Type)
		}
	}
}

func TestAppCleansUpModuleInReverseOrderWhenReminderStartFails(t *testing.T) {
	events := &eventLog{}
	module, err := New(Dependencies{
		Database: &fakeDatabase{events: events}, Transactions: fakeTransactions{},
		Outbox: fakeOutbox{}, Notifications: fakeNotifications{}, Projection: fakeProjection{},
		Dispatcher: &fakeDispatcher{events: events},
		Reminders:  &fakeReminders{events: events, startErr: errors.New("reminder unavailable")},
		Broker:     &fakeBroker{events: events},
		Subscriptions: []event.Subscription{
			fakeSubscription{name: "projection.close", events: events},
			fakeSubscription{name: "notification.close", events: events},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog := modulecatalog.New()
	if err := catalog.Register(module.Descriptor()); err != nil {
		t.Fatal(err)
	}
	application, _, err := core.NewAppWithCapabilities(core.AppOptions{Catalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Start(t.Context()); err == nil {
		t.Fatal("application start accepted failed reminder runtime")
	}
	want := []string{
		"database.ping", "dispatcher.start", "reminder.start",
		"reminder.stop", "dispatcher.shutdown", "notification.close", "projection.close", "broker.close", "database.close",
	}
	if !reflect.DeepEqual(events.values, want) {
		t.Fatalf("startup cleanup events = %#v, want %#v", events.values, want)
	}
}

type eventLog struct{ values []string }

func (log *eventLog) add(value string) { log.values = append(log.values, value) }

type fakeDatabase struct{ events *eventLog }

func (database *fakeDatabase) Ping(context.Context) error {
	database.events.add("database.ping")
	return nil
}
func (database *fakeDatabase) Close() error { database.events.add("database.close"); return nil }

type fakeDispatcher struct{ events *eventLog }

func (dispatcher *fakeDispatcher) Start(context.Context) error {
	dispatcher.events.add("dispatcher.start")
	return nil
}
func (dispatcher *fakeDispatcher) Health(context.Context) error {
	dispatcher.events.add("dispatcher.health")
	return nil
}
func (dispatcher *fakeDispatcher) Shutdown(context.Context) error {
	dispatcher.events.add("dispatcher.shutdown")
	return nil
}

type fakeReminders struct {
	events   *eventLog
	startErr error
}

func (reminders *fakeReminders) Start(context.Context) error {
	reminders.events.add("reminder.start")
	return reminders.startErr
}
func (reminders *fakeReminders) Health(context.Context) error {
	reminders.events.add("reminder.health")
	return nil
}
func (reminders *fakeReminders) Stop(context.Context) error {
	reminders.events.add("reminder.stop")
	return nil
}

type fakeBroker struct{ events *eventLog }

func (broker *fakeBroker) Close() error { broker.events.add("broker.close"); return nil }

type fakeSubscription struct {
	name   string
	events *eventLog
}

func (subscription fakeSubscription) Close() error {
	subscription.events.add(subscription.name)
	return nil
}

type fakeTransactions struct{}

func (fakeTransactions) Begin(context.Context, execution.TransactionMode) (execution.UnitOfWork, error) {
	return nil, errors.New("not used")
}

type fakeOutbox struct{}

func (fakeOutbox) Enqueue(context.Context, event.Envelope) error        { return nil }
func (fakeOutbox) EnqueueTx(context.Context, any, event.Envelope) error { return nil }
func (fakeOutbox) Claim(context.Context, outbox.ClaimOptions) ([]outbox.Record, error) {
	return nil, nil
}
func (fakeOutbox) MarkPublished(context.Context, string, string, time.Time) error { return nil }
func (fakeOutbox) Retry(context.Context, string, string, time.Time, error) error  { return nil }
func (fakeOutbox) DeadLetter(context.Context, string, string, error) error        { return nil }
func (fakeOutbox) Snapshot(context.Context) (outbox.Snapshot, error)              { return outbox.Snapshot{}, nil }

type fakeNotifications struct{}

func (fakeNotifications) List(context.Context, int) ([]notification.Notification, error) {
	return nil, nil
}

type fakeProjection struct{}

func (fakeProjection) Export(context.Context, []delivery.WorkItem) error { return nil }
