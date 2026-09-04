// Package notification provides the local, durable notification delivery seam.
//
// A channel is intentionally a small adapter: production integrations such as
// WeCom, SMTP email, and signed webhooks only implement Channel and are
// registered during application assembly. The local MVP registers the durable
// inbox channel only, so it cannot emit traffic to an external service.
package notification

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const LocalInboxChannelName = "local-inbox"

// Notification is the channel-independent record derived from a reliable
// delivery event. DeliveryID is the upstream event identifier and is used as
// the per-channel idempotency key.
type Notification struct {
	DeliveryID  string    `json:"deliveryId"`
	Channel     string    `json:"channel"`
	EventType   string    `json:"eventType"`
	Subject     string    `json:"subject,omitempty"`
	Title       string    `json:"title,omitempty"`
	Body        string    `json:"body,omitempty"`
	OccurredAt  time.Time `json:"occurredAt"`
	DeliveredAt time.Time `json:"deliveredAt"`
}

// Reader is the read-side boundary exposed to the delivery application.
type Reader interface {
	List(context.Context, int) ([]Notification, error)
}

// InboxStore lets the local inbox persist its channel deliveries without
// coupling the router to SQLite. Alternative local test stores are therefore
// straightforward too.
type InboxStore interface {
	Save(context.Context, Notification) error
	Reader
}

// Channel is the only contract an external notification integration needs to
// implement. The router owns registration, targeting, and channel metadata;
// individual adapters own their credentials and transport policy.
type Channel interface {
	Name() string
	Deliver(context.Context, Notification) error
}

type Router struct {
	mu       sync.RWMutex
	channels map[string]Channel
}

func NewRouter(channels ...Channel) (*Router, error) {
	router := &Router{channels: make(map[string]Channel)}
	for _, channel := range channels {
		if err := router.Register(channel); err != nil {
			return nil, err
		}
	}
	return router, nil
}

// Register attaches a named channel. Duplicate names are rejected so changing
// a configured transport cannot silently redirect notifications.
func (router *Router) Register(channel Channel) error {
	if router == nil {
		return errors.New("notification router is not configured")
	}
	if channel == nil {
		return errors.New("notification channel is required")
	}
	name := normalizeChannelName(channel.Name())
	if name == "" {
		return errors.New("notification channel name is required")
	}
	router.mu.Lock()
	defer router.mu.Unlock()
	if _, exists := router.channels[name]; exists {
		return fmt.Errorf("notification channel %q is already registered", name)
	}
	router.channels[name] = channel
	return nil
}

// Deliver sends a normalized notification to the selected channels. An empty
// destination list means every registered channel, ordered by name; that keeps
// the default local behavior deterministic while making extra channels opt-in
// through application assembly.
func (router *Router) Deliver(ctx context.Context, value Notification, destinations []string) error {
	if router == nil {
		return errors.New("notification router is not configured")
	}
	value, err := normalize(value)
	if err != nil {
		return err
	}
	channels, err := router.resolve(destinations)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		current := value
		current.Channel = normalizeChannelName(channel.Name())
		if current.DeliveredAt.IsZero() {
			current.DeliveredAt = time.Now().UTC()
		}
		if err := channel.Deliver(ctx, current); err != nil {
			return fmt.Errorf("deliver notification to %s: %w", current.Channel, err)
		}
	}
	return nil
}

func (router *Router) resolve(destinations []string) ([]Channel, error) {
	router.mu.RLock()
	defer router.mu.RUnlock()
	if len(router.channels) == 0 {
		return nil, errors.New("no notification channels are registered")
	}
	names := append([]string(nil), destinations...)
	if len(names) == 0 {
		names = make([]string, 0, len(router.channels))
		for name := range router.channels {
			names = append(names, name)
		}
	}
	seen := make(map[string]struct{}, len(names))
	resolved := make([]Channel, 0, len(names))
	for _, rawName := range names {
		name := normalizeChannelName(rawName)
		if name == "" {
			return nil, errors.New("notification destination name is required")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		channel, exists := router.channels[name]
		if !exists {
			return nil, fmt.Errorf("notification channel %q is not registered", name)
		}
		seen[name] = struct{}{}
		resolved = append(resolved, channel)
	}
	sort.Slice(resolved, func(left, right int) bool {
		return normalizeChannelName(resolved[left].Name()) < normalizeChannelName(resolved[right].Name())
	})
	return resolved, nil
}

func normalize(value Notification) (Notification, error) {
	value.DeliveryID = strings.TrimSpace(value.DeliveryID)
	value.EventType = strings.TrimSpace(value.EventType)
	value.Subject = strings.TrimSpace(value.Subject)
	value.Title = strings.TrimSpace(value.Title)
	value.Body = strings.TrimSpace(value.Body)
	if value.DeliveryID == "" {
		return Notification{}, errors.New("notification delivery ID is required")
	}
	if value.EventType == "" {
		return Notification{}, errors.New("notification event type is required")
	}
	if value.OccurredAt.IsZero() {
		value.OccurredAt = time.Now().UTC()
	} else {
		value.OccurredAt = value.OccurredAt.UTC()
	}
	if !value.DeliveredAt.IsZero() {
		value.DeliveredAt = value.DeliveredAt.UTC()
	}
	return value, nil
}

func normalizeChannelName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
