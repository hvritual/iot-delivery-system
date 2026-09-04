package obsidian_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/obsidian"
	"github.com/hvritual/yunka.io/framework/event"
)

func TestProjectionConsumerReprojectsDuplicateWorkItemEventsIdempotently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	vault := t.TempDir()
	repository := delivery.NewMemoryRepository()
	item := delivery.WorkItem{
		ID:        "IOT-PROJECTION-001",
		Title:     "重复事件投影验收",
		Board:     delivery.BoardResearchDelivery,
		Owner:     "研发负责人",
		Priority:  delivery.PriorityP1,
		Status:    delivery.StatusPlanned,
		Gate:      delivery.GatePlanning,
		CreatedAt: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
	}
	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("seed delivery item: %v", err)
	}
	consumer := obsidian.NewProjectionConsumer(delivery.NewService(repository, obsidian.NewExporter(vault)))
	envelope, err := event.NewJSON("delivery.work-item", "delivery.work-item.created", "test", map[string]string{"workItemId": item.ID})
	if err != nil {
		t.Fatalf("create delivery event: %v", err)
	}

	if err := consumer.Handle(ctx, envelope); err != nil {
		t.Fatalf("project first delivery event: %v", err)
	}
	if err := consumer.Handle(ctx, envelope); err != nil {
		t.Fatalf("project duplicate delivery event: %v", err)
	}
	overview, err := os.ReadFile(filepath.Join(vault, "10-交付管理", "00-交付总览.md"))
	if err != nil {
		t.Fatalf("read projected overview: %v", err)
	}
	if got := strings.Count(string(overview), item.ID); got != 1 {
		t.Fatalf("overview contains %d copies of %s, want 1 after duplicate events", got, item.ID)
	}
}
