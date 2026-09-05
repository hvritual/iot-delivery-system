package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/execution"
)

const workItemEventTopic = "delivery.work-item"

type MutationStager interface {
	Stage(context.Context, string, WorkItem) error
}

type transactionalOutboxStager struct {
	store outbox.TransactionalStore
}

func NewTransactionalOutboxStager(store outbox.TransactionalStore) MutationStager {
	return &transactionalOutboxStager{store: store}
}

func (stager *transactionalOutboxStager) Stage(ctx context.Context, eventType string, item WorkItem) error {
	if stager == nil || stager.store == nil {
		return fmt.Errorf("delivery transactional outbox stager is not configured")
	}
	transaction, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return fmt.Errorf("get delivery transaction handle: %w", err)
	}
	topic, err := mutationEventTopic(eventType)
	if err != nil {
		return err
	}
	var payload any
	if topic == workItemEventTopic {
		// Preserve the established work-item event payload contract.
		payload = struct {
			WorkItemID string    `json:"workItemId"`
			UpdatedAt  time.Time `json:"updatedAt"`
		}{WorkItemID: item.ID, UpdatedAt: item.UpdatedAt.UTC()}
	} else {
		payload = struct {
			AggregateID string    `json:"aggregateId"`
			UpdatedAt   time.Time `json:"updatedAt"`
		}{AggregateID: item.ID, UpdatedAt: item.UpdatedAt.UTC()}
	}
	envelope, err := event.NewJSON(topic, eventType, "iot-delivery-system/local", payload)
	if err != nil {
		return fmt.Errorf("create delivery outbox event: %w", err)
	}
	envelope.Subject = item.ID
	if envelope, err = envelope.Normalize(); err != nil {
		return fmt.Errorf("normalize delivery outbox event: %w", err)
	}
	if err := stager.store.EnqueueTx(ctx, transaction, envelope); err != nil {
		return fmt.Errorf("stage delivery outbox event: %w", err)
	}
	return nil
}

// mutationEventTopic keeps event routing aligned with the aggregate named by
// the event type. Work-item projections subscribe only to delivery.work-item;
// project, planning, and saved-view events must not enter that projection by
// pretending their aggregate IDs are work-item IDs.
func mutationEventTopic(eventType string) (string, error) {
	eventType = strings.TrimSpace(eventType)
	parts := strings.Split(eventType, ".")
	if len(parts) < 3 || parts[0] != "delivery" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("delivery mutation event type %q does not identify an aggregate", eventType)
	}
	return "delivery." + parts[1], nil
}
