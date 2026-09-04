package notification_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"yunka.io/framework/event"

	_ "modernc.org/sqlite"
)

func TestRouterRoutesToRegisteredCustomChannel(t *testing.T) {
	t.Parallel()

	channel := &recordingChannel{name: "custom-channel"}
	router, err := notification.NewRouter(channel)
	if err != nil {
		t.Fatalf("create notification router: %v", err)
	}
	note := notification.Notification{
		DeliveryID: "event-001",
		EventType:  "delivery.work-item.created",
		Subject:    "IOT-20260903-0001",
		OccurredAt: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC),
	}
	if err := router.Deliver(context.Background(), note, []string{"custom-channel"}); err != nil {
		t.Fatalf("deliver through custom notification channel: %v", err)
	}
	if len(channel.deliveries) != 1 || channel.deliveries[0].Subject != note.Subject {
		t.Fatalf("custom channel deliveries = %#v, want one delivery for %#v", channel.deliveries, note)
	}
}

func TestLocalInboxDeduplicatesRetransmittedEvent(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := notification.NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("open notification store: %v", err)
	}
	router, err := notification.NewRouter(notification.NewLocalInboxChannel(store))
	if err != nil {
		t.Fatalf("create notification router: %v", err)
	}
	consumer := notification.NewConsumer(router, notification.LocalInboxChannelName)
	envelope, err := event.NewJSON(notification.DeliveryTopic, "delivery.work-item.created", "test", map[string]string{"workItemId": "IOT-NOTIFY-001"})
	if err != nil {
		t.Fatalf("create event envelope: %v", err)
	}
	envelope.Subject = "IOT-NOTIFY-001"
	if err := consumer.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("deliver first event: %v", err)
	}
	if err := consumer.Handle(context.Background(), envelope); err != nil {
		t.Fatalf("deliver retransmitted event: %v", err)
	}
	notifications, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("list local inbox: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("local inbox notifications = %#v, want exactly one idempotent delivery", notifications)
	}
	if notifications[0].Channel != notification.LocalInboxChannelName || notifications[0].Subject != envelope.Subject {
		t.Fatalf("local inbox notification = %#v, want local event for %q", notifications[0], envelope.Subject)
	}
}

func TestConsumerLabelsDueReminderForEveryConfiguredChannel(t *testing.T) {
	t.Parallel()

	channel := &recordingChannel{name: "capture"}
	router, err := notification.NewRouter(channel)
	if err != nil {
		t.Fatalf("create notification router: %v", err)
	}
	envelope, err := event.NewJSON(notification.DeliveryTopic, "delivery.work-item.due-reminder", "test", map[string]string{"workItemId": "IOT-DUE-001"})
	if err != nil {
		t.Fatalf("create due reminder event: %v", err)
	}
	envelope.Subject = "IOT-DUE-001"
	if err := notification.NewConsumer(router).Handle(context.Background(), envelope); err != nil {
		t.Fatalf("consume due reminder: %v", err)
	}
	if len(channel.deliveries) != 1 || channel.deliveries[0].Title != "交付事项临近截止" {
		t.Fatalf("due reminder notification = %#v, want one deadline-labelled delivery", channel.deliveries)
	}
}

type recordingChannel struct {
	name       string
	deliveries []notification.Notification
}

func (channel *recordingChannel) Name() string {
	return channel.name
}

func (channel *recordingChannel) Deliver(_ context.Context, value notification.Notification) error {
	channel.deliveries = append(channel.deliveries, value)
	return nil
}
