package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	application "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU15SavedViewCommitsBusinessOutboxAndAuditInRootTransaction(t *testing.T) {
	fixture := newYU15SavedViewFixture(t, nil, nil)

	view, err := fixture.operations.SaveView(fixture.ctx, delivery.SavedViewInput{Name: "my work"})
	if err != nil {
		t.Fatalf("save view: %v", err)
	}
	views, err := fixture.repository.ListSavedViews(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("list saved views: %v", err)
	}
	if len(views) != 1 || views[0].ID != view.ID {
		t.Fatalf("saved views = %#v, want committed view %q", views, view.ID)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 1 {
		t.Fatalf("outbox after save view = %#v, %v; want one pending event", snapshot, err)
	}
	if count := fixture.auditCount(t); count != 1 {
		t.Fatalf("audit entries after save view = %d, want 1", count)
	}
}

func TestYU15SavedViewOutboxFailureRollsBackBusinessAndAudit(t *testing.T) {
	fixture := newYU15SavedViewFixture(t, failingMutationStager{}, nil)

	if _, err := fixture.operations.SaveView(fixture.ctx, delivery.SavedViewInput{Name: "must roll back"}); err == nil {
		t.Fatal("save view succeeded despite outbox staging failure")
	}
	fixture.assertNoSavedViewResidue(t)
}

func TestYU15SavedViewAuditFailureRollsBackBusinessAndOutbox(t *testing.T) {
	fixture := newYU15SavedViewFixture(t, nil, failingYU15AuditStore{})

	if _, err := fixture.operations.SaveView(fixture.ctx, delivery.SavedViewInput{Name: "must roll back"}); err == nil {
		t.Fatal("save view succeeded despite audit append failure")
	}
	fixture.assertNoSavedViewResidue(t)
}

func TestYU15DirectSavedViewWriteFailsClosedWithTransactionalOutbox(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	service := delivery.NewRootTransactionalService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      "org-a",
		UserID:        "user-a",
		Roles:         []string{localauth.RoleLocalAdmin},
	})

	if _, err := service.SaveView(ctx, delivery.SavedViewInput{Name: "bypass"}); err == nil {
		t.Fatal("direct saved-view mutation succeeded without a root execution transaction")
	}
	views, err := repository.ListSavedViews(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("list saved views after rejected direct write: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("direct write left saved views: %#v", views)
	}
	if snapshot, err := outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 {
		t.Fatalf("direct write left outbox residue: %#v, %v", snapshot, err)
	}
}

type yu15SavedViewFixture struct {
	repository *delivery.SQLiteRepository
	outbox     *localoutbox.SQLiteStore
	operations *application.Operations
	ctx        context.Context
}

func newYU15SavedViewFixture(t *testing.T, overrideStager delivery.MutationStager, overrideAudit audit.Store) *yu15SavedViewFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite audit store: %v", err)
	}
	selectedAudit := audit.Store(auditStore)
	if overrideAudit != nil {
		selectedAudit = overrideAudit
	}
	stager := delivery.MutationStager(delivery.NewTransactionalOutboxStager(outbox))
	if overrideStager != nil {
		stager = overrideStager
	}
	service := delivery.NewRootTransactionalService(repository, nil, stager)
	audited, err := application.NewAuditedDeliveryService(
		application.NewAdapter(service),
		selectedAudit,
		application.WithAuditIDGenerator(func() (string, error) { return "yu15-saved-view-audit", nil }),
		application.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		t.Fatalf("assemble audited delivery service: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	executor := operation.NewExecutorWithOptions(security, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(repository.Database()),
	})
	operations := application.NewOperations(audited, executor, service)
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      "org-a",
		UserID:        "user-a",
		Roles:         []string{localauth.RoleLocalAdmin},
	})
	return &yu15SavedViewFixture{repository: repository, outbox: outbox, operations: operations, ctx: ctx}
}

func (fixture *yu15SavedViewFixture) auditCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.repository.Database().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	return count
}

func (fixture *yu15SavedViewFixture) assertNoSavedViewResidue(t *testing.T) {
	t.Helper()
	views, err := fixture.repository.ListSavedViews(t.Context(), "user-a")
	if err != nil {
		t.Fatalf("list saved views after rollback: %v", err)
	}
	if len(views) != 0 {
		t.Fatalf("rollback left saved views: %#v", views)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 {
		t.Fatalf("rollback left outbox residue: %#v, %v", snapshot, err)
	}
	if count := fixture.auditCount(t); count != 0 {
		t.Fatalf("rollback left audit entries: %d", count)
	}
}

type failingMutationStager struct{}

func (failingMutationStager) Stage(context.Context, string, delivery.WorkItem) error {
	return errors.New("forced outbox staging failure")
}

type failingYU15AuditStore struct{}

func (failingYU15AuditStore) Append(context.Context, audit.Entry) (audit.Entry, error) {
	return audit.Entry{}, errors.New("forced audit append failure")
}

func (failingYU15AuditStore) ByID(context.Context, string) (audit.Entry, error) {
	return audit.Entry{}, errors.New("audit entry unavailable")
}
