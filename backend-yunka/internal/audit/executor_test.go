package audit

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
	"yunka.io/framework/execution"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
	"yunka.io/pkg/operationplan"

	_ "modernc.org/sqlite"
)

func TestRecordingExecutorPersistsFailureOnlyAfterBusinessRollback(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "rollback-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE rollback_probe (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create rollback probe: %v", err)
	}
	if _, err := localoutbox.NewSQLiteStore(database); err != nil {
		t.Fatalf("create outbox store: %v", err)
	}
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewRecordingExecutor(operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)}), recorder)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a"})
	ctx = runtimecontext.WithTraceID(ctx, "rollback-trace-a")
	ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "http", RequestID: "rollback-request-a"})
	plan := operationplan.Plan{OperationID: "delivery.items.create", Execution: operationplan.Execution{Transaction: "local"}}
	businessErr := errors.New("force durable rollback")
	_, err = executor.Execute(ctx, plan, nil, func(callContext context.Context) (any, error) {
		handle, err := execution.TransactionHandleFrom(callContext)
		if err != nil {
			return nil, err
		}
		transaction := handle.(*sql.Tx)
		if _, err := transaction.ExecContext(callContext, `INSERT INTO rollback_probe (id) VALUES ('write-before-failure')`); err != nil {
			return nil, err
		}
		return nil, businessErr
	})
	if err == nil {
		t.Fatal("rollback operation unexpectedly succeeded")
	}
	var businessCount, failureAuditCount, successAuditCount, outboxCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM rollback_probe`).Scan(&businessCount); err != nil {
		t.Fatalf("count rollback probe: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE operation = 'delivery.items.create' AND result = 'failure'`).Scan(&failureAuditCount); err != nil {
		t.Fatalf("count rollback audit: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE operation = 'delivery.items.create' AND result = 'success'`).Scan(&successAuditCount); err != nil {
		t.Fatalf("count success audit: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if !errors.Is(err, businessErr) {
		t.Fatalf("rollback error = %v, want original business error", err)
	}
	if businessCount != 0 || failureAuditCount != 1 || successAuditCount != 0 || outboxCount != 0 {
		t.Fatalf("rollback persistence = business=%d failure-audit=%d success-audit=%d outbox=%d, want 0/1/0/0", businessCount, failureAuditCount, successAuditCount, outboxCount)
	}
}

func TestRecordingExecutorPreservesNilInvokerContract(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "nil-invoker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewSecurityRecorder(store)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewRecordingExecutor(operation.NewExecutor(nil), recorder)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(t.Context(), operationplan.Plan{OperationID: "delivery.items.create"}, nil, nil)
	if !errors.Is(err, operation.ErrInvokerRequired) {
		t.Fatalf("nil invoker error = %v, want ErrInvokerRequired", err)
	}
}

func TestNewRecordingExecutorRejectsMissingRecorder(t *testing.T) {
	executor, err := NewRecordingExecutor(operation.NewExecutor(nil), nil)
	if executor != nil || err == nil {
		t.Fatalf("missing recorder construction = (%T, %v), want nil/error", executor, err)
	}
}

func TestRecordingExecutorKeepsAuthorizationDenialWhenAuditFails(t *testing.T) {
	recorder, err := NewSecurityRecorder(failingStore{})
	if err != nil {
		t.Fatal(err)
	}
	denied := authz.Denied(authz.Decision{Operation: "delivery.items.create", Reason: authz.ReasonPermissionDenied})
	executor, err := NewRecordingExecutor(executorFunc(func(context.Context, operationplan.Plan, any, operation.Invoker) (any, error) { return nil, denied }), recorder)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"})
	_, err = executor.Execute(ctx, operationplan.Plan{OperationID: "delivery.items.create"}, nil, func(context.Context) (any, error) { t.Fatal("denied delegate invoked application"); return nil, nil })
	if !errors.Is(err, authz.ErrDenied) {
		t.Fatalf("denial changed by audit failure: %v", err)
	}
}

type executorFunc func(context.Context, operationplan.Plan, any, operation.Invoker) (any, error)

func (fn executorFunc) Execute(ctx context.Context, plan operationplan.Plan, input any, invoke operation.Invoker) (any, error) {
	return fn(ctx, plan, input, invoke)
}

type failingStore struct{}

func (failingStore) Append(context.Context, Entry) (Entry, error) {
	return Entry{}, errors.New("audit unavailable")
}
func (failingStore) ByID(context.Context, string) (Entry, error) { return Entry{}, ErrNotFound }
