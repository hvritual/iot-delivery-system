package delivery

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteRepositoryPersistsItemsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "delivery.db")
	repository, err := NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	service := NewService(repository, nil)
	created, err := service.Create(ctx, CreateInput{
		Title:    "设备 OTA 发布验收",
		Board:    BoardResearchDelivery,
		Owner:    "研发负责人",
		Priority: PriorityP0,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close first repository: %v", err)
	}

	reopened, err := NewSQLiteRepository(databasePath)
	if err != nil {
		t.Fatalf("reopen sqlite repository: %v", err)
	}
	defer reopened.Close()
	stored, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get persisted item: %v", err)
	}
	if stored.Title != created.Title || stored.Status != StatusPlanned {
		t.Fatalf("stored item = %#v, want title %q and status %q", stored, created.Title, StatusPlanned)
	}

	second, err := NewService(reopened, nil).Create(ctx, CreateInput{
		Title: "设备 OTA 发布复盘",
		Board: BoardResearchDelivery,
		Owner: "研发负责人",
	})
	if err != nil {
		t.Fatalf("create second item after reopen: %v", err)
	}
	if second.ID == created.ID {
		t.Fatal("item IDs collide after repository reopen")
	}
}
