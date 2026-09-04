package bootstrap_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend/internal/bootstrap"
)

func TestApplicationSeedsOneClearlyMarkedSampleAndProjectsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	temporaryRoot := t.TempDir()
	configuration := bootstrap.Config{
		Address:       "127.0.0.1:0",
		DatabasePath:  filepath.Join(temporaryRoot, "data", "delivery.db"),
		ObsidianVault: filepath.Join(temporaryRoot, "vault"),
	}
	application, err := bootstrap.New(ctx, configuration)
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	items, err := application.Service().List(ctx)
	if err != nil {
		t.Fatalf("list seeded items: %v", err)
	}
	if len(items) != 1 || !items[0].IsSample {
		t.Fatalf("seeded items = %#v, want one marked sample", items)
	}
	if _, err := os.Stat(filepath.Join(configuration.ObsidianVault, "10-交付管理", "00-交付总览.md")); err != nil {
		t.Fatalf("generated Obsidian overview is missing: %v", err)
	}
	if err := application.Close(ctx); err != nil {
		t.Fatalf("close application: %v", err)
	}
	overviewPath := filepath.Join(configuration.ObsidianVault, "10-交付管理", "00-交付总览.md")
	if err := os.WriteFile(overviewPath, []byte("stale projection"), 0o644); err != nil {
		t.Fatalf("make stale projection: %v", err)
	}

	reopened, err := bootstrap.New(ctx, configuration)
	if err != nil {
		t.Fatalf("reopen application: %v", err)
	}
	defer reopened.Close(ctx)
	items, err = reopened.Service().List(ctx)
	if err != nil {
		t.Fatalf("list reopened items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("sample was seeded again after reopen: %d items", len(items))
	}
	content, err := os.ReadFile(overviewPath)
	if err != nil {
		t.Fatalf("read refreshed overview: %v", err)
	}
	if strings.Contains(string(content), "stale projection") {
		t.Fatal("existing items were not projected again after application reopen")
	}
}
