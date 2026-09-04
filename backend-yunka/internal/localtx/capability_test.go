package localtx

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/hvritual/yunka.io/framework/core"
	"github.com/hvritual/yunka.io/framework/core/modulecatalog"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	_ "modernc.org/sqlite"
)

func TestCapabilityProvidesOneRootSQLiteUnitOfWorkAndRollsBackJoinedChild(t *testing.T) {
	database, err := sql.Open("sqlite", "file:localtx-capability?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`CREATE TABLE effects (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	counting := &countingFactory{delegate: NewSQLiteFactory(database)}
	descriptor, err := CapabilityDescriptor(counting)
	if err != nil {
		t.Fatal(err)
	}
	catalog := modulecatalog.New()
	if err := catalog.Register(descriptor); err != nil {
		t.Fatal(err)
	}
	application, capabilities, err := core.NewAppWithCapabilities(core.AppOptions{Catalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	transactions, err := modulecatalog.ResolveCapability(capabilities, TransactionFactoryCapability)
	if err != nil {
		t.Fatal(err)
	}
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: transactions})
	rollbackCause := errors.New("reject joined child")
	plan := operationplan.Plan{
		OperationID: "test.root",
		Security:    operationplan.Security{Public: true},
		Execution:   operationplan.Execution{Transaction: "local", Idempotency: "none"},
		Composition: operationplan.Composition{Boundary: "local", RequiresOperations: []string{"test.child"}},
	}
	_, err = executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		rootHandle, err := execution.TransactionHandleFrom(ctx)
		if err != nil {
			return nil, err
		}
		childCtx, err := execution.JoinChild(ctx, "test.child", execution.TransactionLocal, nil)
		if err != nil {
			return nil, err
		}
		childHandle, err := execution.TransactionHandleFrom(childCtx)
		if err != nil {
			return nil, err
		}
		if rootHandle != childHandle {
			return nil, errors.New("joined child received a different SQLite transaction")
		}
		transaction, ok := childHandle.(*sql.Tx)
		if !ok {
			return nil, errors.New("joined child did not receive a SQLite transaction")
		}
		if _, err := transaction.ExecContext(childCtx, `INSERT INTO effects (value) VALUES ('must-rollback')`); err != nil {
			return nil, err
		}
		return nil, rollbackCause
	})
	if !errors.Is(err, rollbackCause) {
		t.Fatalf("root execution error = %v, want rollback cause", err)
	}
	if got := counting.begins.Load(); got != 1 {
		t.Fatalf("SQLite UnitOfWork begins = %d, want exactly 1", got)
	}
	var effects int
	if err := database.QueryRow(`SELECT COUNT(*) FROM effects`).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatalf("rolled-back joined child effects = %d, want 0", effects)
	}
}

func TestCapabilityDescriptorRejectsMissingFactory(t *testing.T) {
	if _, err := CapabilityDescriptor(nil); err == nil {
		t.Fatal("capability descriptor accepted a missing SQLite transaction factory")
	}
}

type countingFactory struct {
	delegate execution.TransactionFactory
	begins   atomic.Int64
}

func (factory *countingFactory) Begin(ctx context.Context, mode execution.TransactionMode) (execution.UnitOfWork, error) {
	factory.begins.Add(1)
	return factory.delegate.Begin(ctx, mode)
}
