package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"yunka.io/framework/event"
)

const DeliveryTopic = "delivery.work-item"

// Consumer converts reliable delivery events into channel-neutral
// notifications. The persisted local inbox provides the idempotency boundary;
// the consumer itself remains safe for Yunka's at-least-once outbox delivery.
type Consumer struct {
	router       *Router
	destinations []string
}

func NewConsumer(router *Router, destinations ...string) *Consumer {
	return &Consumer{router: router, destinations: append([]string(nil), destinations...)}
}

func (consumer *Consumer) Handle(ctx context.Context, envelope event.Envelope) error {
	if consumer == nil || consumer.router == nil {
		return errors.New("notification consumer is not configured")
	}
	if strings.TrimSpace(envelope.Topic) != DeliveryTopic {
		return fmt.Errorf("notification consumer received unexpected topic %q", envelope.Topic)
	}
	value := Notification{
		DeliveryID: envelope.ID,
		EventType:  envelope.Type,
		Subject:    envelope.Subject,
		Title:      notificationTitle(envelope.Type),
		Body:       notificationBody(envelope.Type, envelope.Subject),
		OccurredAt: envelope.OccurredAt,
	}
	return consumer.router.Deliver(ctx, value, consumer.destinations)
}

func notificationTitle(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "delivery.work-item.created":
		return "新建交付事项"
	case "delivery.work-item.gate-advanced":
		return "交付事项关卡已推进"
	case "delivery.work-item.closed":
		return "交付事项已关闭"
	case "delivery.work-item.context-updated":
		return "交付事项上下文已更新"
	case "delivery.work-item.due-reminder":
		return "交付事项临近截止"
	case "delivery.project.created":
		return "新建交付项目"
	default:
		return "交付事项有新动态"
	}
}

func notificationBody(eventType, subject string) string {
	if strings.TrimSpace(subject) == "" {
		return fmt.Sprintf("事件：%s", strings.TrimSpace(eventType))
	}
	return fmt.Sprintf("%s · 事件：%s", strings.TrimSpace(subject), strings.TrimSpace(eventType))
}
