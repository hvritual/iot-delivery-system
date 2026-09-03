package obsidian

import (
	"context"
	"errors"
	"strings"

	"yunka.io/framework/event"
)

const workItemTopic = "delivery.work-item"

type Synchronizer interface {
	Sync(context.Context) error
}

// ProjectionConsumer deliberately projects the current materialized state
// rather than applying a delta. Replaying the same delivery event therefore
// produces the same vault content and is safe for at-least-once delivery.
type ProjectionConsumer struct {
	synchronizer Synchronizer
}

func NewProjectionConsumer(synchronizer Synchronizer) *ProjectionConsumer {
	return &ProjectionConsumer{synchronizer: synchronizer}
}

func (consumer *ProjectionConsumer) Handle(ctx context.Context, envelope event.Envelope) error {
	if consumer == nil || consumer.synchronizer == nil {
		return errors.New("Obsidian projection consumer is not configured")
	}
	if strings.TrimSpace(envelope.Topic) != workItemTopic {
		return errors.New("Obsidian projection consumer received an unexpected topic")
	}
	return consumer.synchronizer.Sync(ctx)
}
