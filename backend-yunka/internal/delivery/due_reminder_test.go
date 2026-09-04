package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"yunka.io/framework/event/outbox"
)

func TestDueReminderSchedulerQueuesOneReminderPerOpenItemPerDay(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	today := time.Now().UTC().Format("2006-01-02")
	item, err := service.Create(ctx, delivery.CreateInput{
		Title:   "完成灰度发布验收",
		Board:   delivery.BoardResearchDelivery,
		Owner:   "发布负责人",
		DueDate: today,
	})
	if err != nil {
		t.Fatalf("create due item: %v", err)
	}
	store := outbox.NewMemoryStore()
	scheduler, err := delivery.NewDueReminderScheduler(service, store, delivery.DueReminderConfig{LeadDays: 1, Interval: time.Hour})
	if err != nil {
		t.Fatalf("create due reminder scheduler: %v", err)
	}

	queued, err := scheduler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("queue due reminders: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued reminders = %d, want 1", queued)
	}
	queued, err = scheduler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("re-run due reminders: %v", err)
	}
	if queued != 0 {
		t.Fatalf("re-run queued reminders = %d, want 0 due to persistent idempotency", queued)
	}
	records, err := store.Claim(ctx, outbox.ClaimOptions{Owner: "due-reminder-test", Limit: 10, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("claim reminder event: %v", err)
	}
	if len(records) != 1 || records[0].Envelope.Type != delivery.DueReminderEventType || records[0].Envelope.Subject != item.ID {
		t.Fatalf("reminder outbox record = %#v, want one due reminder for %q", records, item.ID)
	}
}

func TestDueReminderSchedulerSkipsCompletedItems(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	today := time.Now().UTC().Format("2006-01-02")
	item, err := service.Create(ctx, delivery.CreateInput{Title: "已完成的发布记录", Board: delivery.BoardResearchDelivery, Owner: "发布负责人", DueDate: today})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	completed := 100
	if _, err := service.UpdateWorkItem(ctx, item.ID, item.Revision, delivery.WorkItemUpdate{ProgressPercent: &completed}); err != nil {
		t.Fatalf("complete item: %v", err)
	}
	scheduler, err := delivery.NewDueReminderScheduler(service, outbox.NewMemoryStore(), delivery.DueReminderConfig{})
	if err != nil {
		t.Fatalf("create due reminder scheduler: %v", err)
	}
	queued, err := scheduler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("queue due reminders: %v", err)
	}
	if queued != 0 {
		t.Fatalf("queued reminders = %d, want completed item skipped", queued)
	}
}

func TestDueReminderSchedulerStartsAndStopsAsARuntimeWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	if _, err := service.Create(ctx, delivery.CreateInput{
		Title:   "启动即检查的截止事项",
		Board:   delivery.BoardResearchDelivery,
		Owner:   "发布负责人",
		DueDate: time.Now().UTC().Format("2006-01-02"),
	}); err != nil {
		t.Fatalf("create due item: %v", err)
	}
	store := outbox.NewMemoryStore()
	scheduler, err := delivery.NewDueReminderScheduler(service, store, delivery.DueReminderConfig{Interval: time.Hour})
	if err != nil {
		t.Fatalf("create scheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start due reminder worker: %v", err)
	}
	t.Cleanup(func() { _ = scheduler.Stop(context.Background()) })
	snapshot, err := store.Snapshot(ctx)
	if err != nil || snapshot.Pending != 1 {
		t.Fatalf("started reminder worker snapshot = %#v, error=%v; want one pending reminder", snapshot, err)
	}
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatalf("stop due reminder worker: %v", err)
	}
}
