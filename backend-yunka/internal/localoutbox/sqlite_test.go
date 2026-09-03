package localoutbox_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	_ "modernc.org/sqlite"
	"yunka.io/framework/event"
	frameworkoutbox "yunka.io/framework/event/outbox"
)

func TestDispatcherPublishesPendingEventAndMarksItPublished(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("open SQLite store: %v", err)
	}
	envelope, err := event.NewJSON("delivery.work-item", "delivery.work-item.created", "test", map[string]string{"workItemId": "IOT-OUTBOX-001"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := store.Enqueue(ctx, envelope); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}

	broker := event.NewLocalBroker(nil)
	t.Cleanup(func() { _ = broker.Close() })
	delivered := make(chan event.Envelope, 1)
	subscription, err := broker.Subscribe(ctx, "delivery.work-item", func(_ context.Context, received event.Envelope) error {
		delivered <- received
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe local broker: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })

	dispatcher, err := frameworkoutbox.NewDispatcher(store, broker, frameworkoutbox.DispatcherConfig{
		WorkerID:       "test-worker",
		BatchSize:      1,
		Concurrency:    1,
		LeaseDuration:  time.Second,
		PublishTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("create dispatcher: %v", err)
	}
	if err := dispatcher.RunOnce(ctx); err != nil {
		t.Fatalf("dispatch pending event: %v", err)
	}

	select {
	case received := <-delivered:
		if received.ID != envelope.ID || received.Type != "delivery.work-item.created" {
			t.Fatalf("received event = %#v, want the queued delivery event", received)
		}
	default:
		t.Fatal("local broker did not receive the queued event")
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read outbox snapshot: %v", err)
	}
	if snapshot.Pending != 0 || snapshot.Published != 1 {
		t.Fatalf("outbox snapshot = %#v, want 0 pending and 1 published", snapshot)
	}
}

func TestDispatcherRetriesFailedEventAfterRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "outbox-retry.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("open SQLite store: %v", err)
	}
	envelope, err := event.NewJSON("delivery.work-item", "delivery.work-item.created", "test", map[string]string{"workItemId": "IOT-OUTBOX-RETRY"})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := store.Enqueue(ctx, envelope); err != nil {
		t.Fatalf("enqueue event: %v", err)
	}

	broker := event.NewLocalBroker(nil)
	t.Cleanup(func() { _ = broker.Close() })
	attempts := 0
	subscription, err := broker.Subscribe(ctx, "delivery.work-item", func(context.Context, event.Envelope) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary projection failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe local broker: %v", err)
	}
	t.Cleanup(func() { _ = subscription.Close() })
	clock := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	configuration := func() frameworkoutbox.DispatcherConfig {
		return frameworkoutbox.DispatcherConfig{
			WorkerID:       "restart-test-worker",
			BatchSize:      1,
			Concurrency:    1,
			LeaseDuration:  time.Second,
			PublishTimeout: time.Second,
			RetryBase:      time.Millisecond,
			RetryMax:       time.Second,
			Now:            func() time.Time { return clock },
		}
	}
	first, err := frameworkoutbox.NewDispatcher(store, broker, configuration())
	if err != nil {
		t.Fatalf("create first dispatcher: %v", err)
	}
	if err := first.RunOnce(ctx); err != nil {
		t.Fatalf("record retryable publish failure: %v", err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read retry snapshot: %v", err)
	}
	if snapshot.Pending != 1 || snapshot.Published != 0 {
		t.Fatalf("retry snapshot = %#v, want one pending event", snapshot)
	}

	clock = clock.Add(time.Second)
	restarted, err := frameworkoutbox.NewDispatcher(store, broker, configuration())
	if err != nil {
		t.Fatalf("create restarted dispatcher: %v", err)
	}
	if err := restarted.RunOnce(ctx); err != nil {
		t.Fatalf("publish retry after restart: %v", err)
	}
	snapshot, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read published snapshot: %v", err)
	}
	if attempts != 2 || snapshot.Pending != 0 || snapshot.Published != 1 {
		t.Fatalf("attempts=%d snapshot=%#v, want two attempts and one published event", attempts, snapshot)
	}
}
