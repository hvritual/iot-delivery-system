package delivery

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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

func TestSQLiteRepositoryWaitsForConcurrentWriterInsteadOfFailingBusy(t *testing.T) {
	repository, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open sqlite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	ctx := context.Background()
	transaction, err := repository.Database().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin lock-holding transaction: %v", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `INSERT INTO iotd_delivery_items (id, payload, updated_at, revision) VALUES (?, ?, ?, ?)`, "lock-holder", `{}`, time.Now().UTC().Format(timeLayout), 1); err != nil {
		t.Fatalf("write lock-holding item: %v", err)
	}

	waiter := make(chan error, 1)
	go func() {
		_, writeErr := repository.Database().ExecContext(context.Background(), `INSERT INTO iotd_delivery_items (id, payload, updated_at, revision) VALUES (?, ?, ?, ?)`, "waiter", `{}`, time.Now().UTC().Format(timeLayout), 1)
		waiter <- writeErr
	}()

	select {
	case writeErr := <-waiter:
		t.Fatalf("concurrent writer returned before the transaction released its lock: %v", writeErr)
	case <-time.After(150 * time.Millisecond):
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit lock-holding transaction: %v", err)
	}
	select {
	case writeErr := <-waiter:
		if writeErr != nil {
			t.Fatalf("concurrent writer failed after lock release: %v", writeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent writer did not resume after lock release")
	}
}
