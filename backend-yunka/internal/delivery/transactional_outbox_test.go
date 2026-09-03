package delivery_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	frameworkoutbox "yunka.io/framework/event/outbox"
	"yunka.io/framework/operation"
	"yunka.io/pkg/operationplan"
)

func TestCreateOperationCommitsWorkItemAndOutboxEventTogether(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox store: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(repository.Database()),
	})
	plan := operationplan.Plan{
		OperationID: "delivery.items.create",
		Execution:   operationplan.Execution{Transaction: "local", Idempotency: "none"},
	}

	createdValue, err := executor.Execute(ctx, plan, nil, func(callCtx context.Context) (any, error) {
		return service.Create(callCtx, delivery.CreateInput{
			Title: "本地事务 Outbox 验收",
			Board: delivery.BoardResearchDelivery,
			Owner: "研发负责人",
		})
	})
	if err != nil {
		t.Fatalf("create through local operation: %v", err)
	}
	created := createdValue.(delivery.WorkItem)
	if _, err := repository.Get(ctx, created.ID); err != nil {
		t.Fatalf("read committed work item: %v", err)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read outbox snapshot: %v", err)
	}
	if snapshot.Pending != 1 {
		t.Fatalf("pending outbox events = %d, want 1", snapshot.Pending)
	}

	var rolledBack delivery.WorkItem
	_, err = executor.Execute(ctx, plan, nil, func(callCtx context.Context) (any, error) {
		var createErr error
		rolledBack, createErr = service.Create(callCtx, delivery.CreateInput{
			Title: "必须回滚的事项",
			Board: delivery.BoardResearchDelivery,
			Owner: "研发负责人",
		})
		if createErr != nil {
			return nil, createErr
		}
		return nil, errors.New("force transaction rollback")
	})
	if err == nil || err.Error() != "force transaction rollback" {
		t.Fatalf("rollback error = %v, want forced rollback", err)
	}
	if _, err := repository.Get(ctx, rolledBack.ID); !errors.Is(err, delivery.ErrNotFound) {
		t.Fatalf("rolled back work item error = %v, want ErrNotFound", err)
	}
	snapshot, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read outbox snapshot after rollback: %v", err)
	}
	if snapshot.Pending != 1 {
		t.Fatalf("pending outbox events after rollback = %d, want 1", snapshot.Pending)
	}
	records, err := store.Claim(ctx, frameworkoutbox.ClaimOptions{Owner: "event-shape-test", Limit: 1, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("claim committed outbox event: %v", err)
	}
	if len(records) != 1 || records[0].Envelope.ID == "" || records[0].Envelope.Type != "delivery.work-item.created" || records[0].Envelope.SchemaVersion < 1 || records[0].Envelope.OccurredAt.IsZero() {
		t.Fatalf("committed outbox record = %#v, want ID/type/version/time", records)
	}
}

func TestTransactionalStagerRejectsDirectMutationBeforeWritingWorkItem(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery-direct.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox store: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store))
	if _, err := service.Create(ctx, delivery.CreateInput{
		Title: "不能绕过事务执行器",
		Board: delivery.BoardResearchDelivery,
		Owner: "研发负责人",
	}); err == nil {
		t.Fatal("direct mutation without a Yunka transaction should fail")
	}
	items, err := repository.List(ctx)
	if err != nil {
		t.Fatalf("list direct-mutation repository: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("direct mutation persisted %#v, want no work item", items)
	}
	snapshot, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("read direct-mutation outbox: %v", err)
	}
	if snapshot.Pending != 0 {
		t.Fatalf("direct mutation queued %d events, want 0", snapshot.Pending)
	}
}
