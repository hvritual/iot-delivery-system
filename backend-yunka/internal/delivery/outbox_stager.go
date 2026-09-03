package delivery

import (
	"context"
	"fmt"
	"time"

	"yunka.io/framework/event"
	"yunka.io/framework/event/outbox"
	"yunka.io/framework/execution"
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
	envelope, err := event.NewJSON(workItemEventTopic, eventType, "iot-delivery-system/local", struct {
		WorkItemID string    `json:"workItemId"`
		UpdatedAt  time.Time `json:"updatedAt"`
	}{WorkItemID: item.ID, UpdatedAt: item.UpdatedAt.UTC()})
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
