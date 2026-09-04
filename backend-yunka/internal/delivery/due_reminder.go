package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
)

// DueReminderEventType is delivered through the same reliable event path as
// lifecycle changes, allowing the local inbox and any explicitly configured
// channels to receive deadline reminders without a second notification stack.
const DueReminderEventType = "delivery.work-item.due-reminder"

type DueReminderConfig struct {
	// LeadDays includes items due today through this many calendar days ahead.
	// Overdue open items remain eligible so a missed deadline stays visible.
	LeadDays int
	// Interval is consumed by the runtime component; RunOnce is also safe for
	// a manual scheduler or a test process.
	Interval time.Duration
}

type DueReminderScheduler struct {
	service  *Service
	store    outbox.Store
	leadDays int
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	lastErr error
}

func NewDueReminderScheduler(service *Service, store outbox.Store, configuration DueReminderConfig) (*DueReminderScheduler, error) {
	if service == nil {
		return nil, errors.New("due reminder delivery service is required")
	}
	if store == nil {
		return nil, errors.New("due reminder outbox store is required")
	}
	if configuration.LeadDays < 0 {
		return nil, errors.New("due reminder lead days cannot be negative")
	}
	if configuration.Interval <= 0 {
		configuration.Interval = time.Hour
	}
	return &DueReminderScheduler{
		service: service, store: store, leadDays: configuration.LeadDays, interval: configuration.Interval, now: time.Now,
	}, nil
}

func (scheduler *DueReminderScheduler) Interval() time.Duration {
	if scheduler == nil || scheduler.interval <= 0 {
		return time.Hour
	}
	return scheduler.interval
}

// Start performs an immediate check, then keeps the reminder queue current at
// the configured interval. Outbox IDs make repeated ticks and restarts safe.
func (scheduler *DueReminderScheduler) Start(ctx context.Context) error {
	if scheduler == nil {
		return errors.New("due reminder scheduler is not configured")
	}
	scheduler.mu.Lock()
	if scheduler.cancel != nil {
		scheduler.mu.Unlock()
		return errors.New("due reminder scheduler is already running")
	}
	scheduler.mu.Unlock()
	if _, err := scheduler.RunOnce(ctx); err != nil {
		return err
	}
	loopContext, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	scheduler.mu.Lock()
	scheduler.cancel = cancel
	scheduler.done = done
	scheduler.lastErr = nil
	scheduler.mu.Unlock()
	go scheduler.run(loopContext, done)
	return nil
}

func (scheduler *DueReminderScheduler) run(ctx context.Context, done chan struct{}) {
	ticker := time.NewTicker(scheduler.Interval())
	defer ticker.Stop()
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := scheduler.RunOnce(context.Background())
			scheduler.mu.Lock()
			scheduler.lastErr = err
			scheduler.mu.Unlock()
		}
	}
}

func (scheduler *DueReminderScheduler) Health(ctx context.Context) error {
	if scheduler == nil {
		return errors.New("due reminder scheduler is not configured")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.lastErr
}

func (scheduler *DueReminderScheduler) Stop(ctx context.Context) error {
	if scheduler == nil {
		return nil
	}
	scheduler.mu.Lock()
	cancel := scheduler.cancel
	done := scheduler.done
	scheduler.cancel = nil
	scheduler.done = nil
	scheduler.mu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	if ctx == nil {
		<-done
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunOnce queues a stable, per-item/per-day event. The persisted outbox ID is
// the idempotency boundary across repeated runs and process restarts.
func (scheduler *DueReminderScheduler) RunOnce(ctx context.Context) (int, error) {
	if scheduler == nil || scheduler.service == nil || scheduler.store == nil {
		return 0, errors.New("due reminder scheduler is not configured")
	}
	items, err := scheduler.service.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("list work items for due reminders: %w", err)
	}
	now := scheduler.now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	latest := today.AddDate(0, 0, scheduler.leadDays)
	queued := 0
	for _, item := range items {
		if effectiveProgress(item) >= 100 || strings.TrimSpace(item.DueDate) == "" {
			continue
		}
		dueDate, err := time.Parse("2006-01-02", item.DueDate)
		if err != nil || dueDate.After(latest) {
			continue
		}
		envelope, err := newDueReminderEnvelope(item, today, now)
		if err != nil {
			return queued, err
		}
		if err := scheduler.store.Enqueue(ctx, envelope); err != nil {
			if errors.Is(err, outbox.ErrDuplicate) {
				continue
			}
			return queued, fmt.Errorf("queue due reminder for %s: %w", item.ID, err)
		}
		queued++
	}
	return queued, nil
}

func newDueReminderEnvelope(item WorkItem, today, occurredAt time.Time) (event.Envelope, error) {
	payload := struct {
		WorkItemID   string `json:"workItemId"`
		Title        string `json:"title"`
		Owner        string `json:"owner"`
		DueDate      string `json:"dueDate"`
		ReminderDate string `json:"reminderDate"`
	}{
		WorkItemID: item.ID, Title: item.Title, Owner: item.Owner, DueDate: item.DueDate, ReminderDate: today.Format("2006-01-02"),
	}
	envelope, err := event.NewJSON(workItemEventTopic, DueReminderEventType, "iot-delivery-system/reminder", payload)
	if err != nil {
		return event.Envelope{}, fmt.Errorf("create due reminder event: %w", err)
	}
	envelope.ID = dueReminderEventID(item.ID, today)
	envelope.CorrelationID = envelope.ID
	envelope.Subject = item.ID
	envelope.OccurredAt = occurredAt.UTC()
	envelope, err = envelope.Normalize()
	if err != nil {
		return event.Envelope{}, fmt.Errorf("normalize due reminder event: %w", err)
	}
	return envelope, nil
}

func dueReminderEventID(itemID string, day time.Time) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(itemID) + "|" + day.UTC().Format("2006-01-02")))
	return "due-reminder-" + hex.EncodeToString(sum[:12])
}
