package delivery_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"yunka.io/framework/core/identity"
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

func TestSeparationOfDutiesRejectionsLeaveSQLiteAndOutboxUnchanged(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(t *testing.T, fixture separationOfDutiesFixture)
	}{
		{
			name: "malformed persisted implementation source",
			run: func(t *testing.T, fixture separationOfDutiesFixture) {
				item := fixture.advanceToTestPassed(t)
				item.ImplementationPrincipal = delivery.PrincipalSource{Kind: "human", AuthMethod: identity.AuthMethodJWT, SubjectID: "implementer"}
				if err := fixture.repository.Save(t.Context(), item); err != nil {
					t.Fatal(err)
				}
				fixture.assertRejectedAdvance(t, item.ID, fixture.reviewer)
			},
		},
		{
			name: "cross tenant production validation",
			run: func(t *testing.T, fixture separationOfDutiesFixture) {
				item := fixture.advanceToTestPassed(t)
				fixture.assertRejectedAdvance(t, item.ID, fixture.crossTenantReviewer)
			},
		},
		{
			name: "cross tenant close",
			run: func(t *testing.T, fixture separationOfDutiesFixture) {
				item := fixture.advanceToTestPassed(t)
				if _, err := fixture.advance(t, fixture.reviewer, item.ID, delivery.GateProductionValidated); err != nil {
					t.Fatal(err)
				}
				before, err := fixture.repository.Get(t.Context(), item.ID)
				if err != nil {
					t.Fatal(err)
				}
				beforeOutbox, err := fixture.store.Snapshot(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				_, err = fixture.executor.Execute(fixture.crossTenantReviewer, fixture.plan, nil, func(callContext context.Context) (any, error) {
					return fixture.service.Close(callContext, item.ID, "cross tenant retrospective")
				})
				if !errors.Is(err, delivery.ErrImplementationSourceRequired) {
					t.Fatalf("cross-tenant close error = %v", err)
				}
				after, err := fixture.repository.Get(t.Context(), item.ID)
				if err != nil || !reflect.DeepEqual(after, before) {
					t.Fatalf("cross-tenant close changed item: %#v, %v", after, err)
				}
				afterOutbox, err := fixture.store.Snapshot(t.Context())
				if err != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
					t.Fatalf("cross-tenant close changed outbox: %#v, %v", afterOutbox, err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.run(t, newSeparationOfDutiesFixture(t))
		})
	}
}

type separationOfDutiesFixture struct {
	repository          *delivery.SQLiteRepository
	store               *localoutbox.SQLiteStore
	service             *delivery.Service
	executor            operation.Executor
	plan                operationplan.Plan
	implementer         context.Context
	reviewer            context.Context
	crossTenantReviewer context.Context
}

func newSeparationOfDutiesFixture(t *testing.T) separationOfDutiesFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	store, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatal(err)
	}
	implementer := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "implementer"})
	reviewer := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "reviewer"})
	crossTenantReviewer := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-b", UserID: "reviewer"})
	return separationOfDutiesFixture{
		repository:          repository,
		store:               store,
		service:             delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(store)),
		executor:            operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
		plan:                operationplan.Plan{OperationID: "delivery.items.advance-gate", Execution: operationplan.Execution{Transaction: "local", Idempotency: "none"}},
		implementer:         implementer,
		reviewer:            reviewer,
		crossTenantReviewer: crossTenantReviewer,
	}
}

func (fixture separationOfDutiesFixture) advanceToTestPassed(t *testing.T) delivery.WorkItem {
	t.Helper()
	createdValue, err := fixture.executor.Execute(fixture.implementer, fixture.plan, nil, func(callContext context.Context) (any, error) {
		return fixture.service.Create(callContext, delivery.CreateInput{Title: t.Name(), Board: delivery.BoardResearchDelivery, Owner: "display owner"})
	})
	if err != nil {
		t.Fatal(err)
	}
	item := createdValue.(delivery.WorkItem)
	for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
		item, err = fixture.advance(t, fixture.implementer, item.ID, gate)
		if err != nil {
			t.Fatal(err)
		}
	}
	return item
}

func (fixture separationOfDutiesFixture) advance(t *testing.T, ctx context.Context, itemID string, gate delivery.Gate) (delivery.WorkItem, error) {
	t.Helper()
	value, err := fixture.executor.Execute(ctx, fixture.plan, nil, func(callContext context.Context) (any, error) {
		return fixture.service.AdvanceGate(callContext, itemID, gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}})
	})
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return value.(delivery.WorkItem), nil
}

func (fixture separationOfDutiesFixture) assertRejectedAdvance(t *testing.T, itemID string, reviewer context.Context) {
	t.Helper()
	before, err := fixture.repository.Get(t.Context(), itemID)
	if err != nil {
		t.Fatal(err)
	}
	beforeOutbox, err := fixture.store.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.advance(t, reviewer, itemID, delivery.GateProductionValidated); !errors.Is(err, delivery.ErrImplementationSourceRequired) {
		t.Fatalf("production validation error = %v", err)
	}
	after, err := fixture.repository.Get(t.Context(), itemID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected production validation changed item: %#v, %v", after, err)
	}
	afterOutbox, err := fixture.store.Snapshot(t.Context())
	if err != nil || !reflect.DeepEqual(afterOutbox, beforeOutbox) {
		t.Fatalf("rejected production validation changed outbox: %#v, %v", afterOutbox, err)
	}
}
