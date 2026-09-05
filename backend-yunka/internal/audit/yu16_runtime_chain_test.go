package audit_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	deliverypolicy "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU16DurableJWTAuthorizationDenialPersistsDeniedAuditWithoutSideEffects(t *testing.T) {
	fixture := newYU16RuntimeFixture(t, false)
	ctx := yu16HumanContext(t)
	plan := deliverypolicy.OperationPlanCreateProject()
	request := &deliveryv1.CreateProjectRequest{
		Name:        "denied project",
		Board:       string(delivery.BoardResearchDelivery),
		Owner:       "owner",
		Description: "YU16-password-secret",
	}
	invoked := false
	_, err := fixture.executor.Execute(ctx, plan, request, func(context.Context) (any, error) {
		invoked = true
		return nil, nil
	})
	if !authz.IsDenied(err) {
		t.Fatalf("authorization result = %v, want denied", err)
	}
	if invoked {
		t.Fatal("denied durable JWT operation invoked the application")
	}
	projects, err := fixture.repository.ListProjects(t.Context())
	if err != nil {
		t.Fatalf("list projects after denial: %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("denied operation left projects: %#v", projects)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 || snapshot.InFlight != 0 || snapshot.Published != 0 || snapshot.DeadLetter != 0 {
		t.Fatalf("denied operation outbox = %#v, %v; want empty", snapshot, err)
	}
	entry := fixture.singleEntry(t, plan.OperationID)
	if entry.EventCategory != audit.EventCategoryAuthorization || entry.ActorType != audit.ActorHuman || entry.ActorID != "user-yu16" || entry.OrganizationID != "org-yu16" || entry.AuthorizationDecision != audit.DecisionDenied || entry.Result != audit.ResultDenied || entry.ReasonCode != "authorization.denied" {
		t.Fatalf("denial audit = %#v", entry)
	}
	fixture.assertNoSecrets(t)
}

func TestYU16DurableJWTApplicationFailureRollsBackBusinessAndOutboxBeforeFailureAudit(t *testing.T) {
	fixture := newYU16RuntimeFixture(t, true)
	if _, err := fixture.repository.Database().Exec(`CREATE TABLE yu16_rollback_probe (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create rollback probe: %v", err)
	}
	ctx := yu16HumanContext(t)
	plan := deliverypolicy.OperationPlanCreateProject()
	request := &deliveryv1.CreateProjectRequest{Name: "rollback project", Board: string(delivery.BoardResearchDelivery), Owner: "owner"}
	businessErr := errors.New("YU16-password-secret")
	_, err := fixture.executor.Execute(ctx, plan, request, func(callContext context.Context) (any, error) {
		handle, err := execution.TransactionHandleFrom(callContext)
		if err != nil {
			return nil, err
		}
		transaction, ok := handle.(*sql.Tx)
		if !ok || transaction == nil {
			return nil, errors.New("YU16 transaction handle is not SQLite")
		}
		if _, err := transaction.ExecContext(callContext, `INSERT INTO yu16_rollback_probe (id) VALUES ('would-rollback')`); err != nil {
			return nil, err
		}
		envelope, err := event.NewJSON("delivery.project", "delivery.project.created", "iot-delivery-system/yu16-test", struct {
			ProjectID string `json:"projectId"`
		}{ProjectID: "would-rollback"})
		if err != nil {
			return nil, err
		}
		envelope.Subject = "would-rollback"
		envelope, err = envelope.Normalize()
		if err != nil {
			return nil, err
		}
		if err := fixture.outbox.EnqueueTx(callContext, transaction, envelope); err != nil {
			return nil, err
		}
		return nil, businessErr
	})
	if !errors.Is(err, businessErr) {
		t.Fatalf("rollback error = %v, want original application failure", err)
	}
	var probeCount int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM yu16_rollback_probe`).Scan(&probeCount); err != nil {
		t.Fatalf("count rollback probe: %v", err)
	}
	if probeCount != 0 {
		t.Fatalf("rollback probe rows = %d, want 0", probeCount)
	}
	if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 || snapshot.InFlight != 0 || snapshot.Published != 0 || snapshot.DeadLetter != 0 {
		t.Fatalf("rollback outbox = %#v, %v; want empty", snapshot, err)
	}
	entry := fixture.singleEntry(t, plan.OperationID)
	if entry.EventCategory != audit.EventCategoryDelivery || entry.ActorType != audit.ActorHuman || entry.ActorID != "user-yu16" || entry.OrganizationID != "org-yu16" || entry.AuthorizationDecision != audit.DecisionAllowed || entry.Result != audit.ResultFailure || entry.ReasonCode != "application.transaction_rolled_back" {
		t.Fatalf("rollback audit = %#v", entry)
	}
	fixture.assertNoSecrets(t)
}

type yu16RuntimeFixture struct {
	repository *delivery.SQLiteRepository
	outbox     *localoutbox.SQLiteStore
	auditStore *audit.SQLiteStore
	executor   operation.Executor
}

func newYU16RuntimeFixture(t *testing.T, grantAdministrator bool) *yu16RuntimeFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	if _, err := repository.Database().Exec(`INSERT INTO organizations (id, slug, name, status) VALUES ('org-yu16', 'org-yu16', 'YU16 Organization', 'active')`); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	if _, err := repository.Database().Exec(`INSERT INTO users (id, organization_id, display_name, status) VALUES ('user-yu16', 'org-yu16', 'YU16 User', 'active')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if grantAdministrator {
		if _, err := repository.Database().Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id) VALUES ('binding-yu16-admin', 'org-yu16', 'system-administrator', 'organization', 'org-yu16', 'user-yu16')`); err != nil {
			t.Fatalf("seed administrator binding: %v", err)
		}
	}
	humanResolver, err := humanauthz.NewGrantResolver(repository.Database())
	if err != nil {
		t.Fatalf("create durable human grant resolver: %v", err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(humanResolver)
	if err != nil {
		t.Fatalf("create durable grant authorizer: %v", err)
	}
	guard, err := deliveryauthz.NewOperationGuard(repository, repository.Database())
	if err != nil {
		t.Fatalf("create delivery operation guard: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guard.GuardResolver())
	if err != nil {
		t.Fatalf("create production-shaped execution security: %v", err)
	}
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	recorder, err := audit.NewSecurityRecorder(auditStore)
	if err != nil {
		t.Fatalf("create audit recorder: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open outbox: %v", err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(repository.Database()),
	}), recorder)
	if err != nil {
		t.Fatalf("create recording executor: %v", err)
	}
	return &yu16RuntimeFixture{repository: repository, outbox: outbox, auditStore: auditStore, executor: executor}
}

func yu16HumanContext(t *testing.T) context.Context {
	t.Helper()
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      "org-yu16",
		UserID:        "user-yu16",
		Roles:         []string{"forged-role-must-not-authorize"},
	})
	ctx = runtimecontext.WithTraceID(ctx, "0123456789abcdef0123456789abcdef")
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{
		Transport: "http",
		Protocol:  "http",
		Method:    "POST",
		Route:     "/api/projects",
		RequestID: "request-yu16",
		Attributes: map[string]string{
			"correlation_id": "correlation-yu16",
			"authorization":  "Bearer YU16-authorization-secret",
			"session":        "YU16-session-secret",
			"csrf":           "YU16-csrf-secret",
		},
	})
}

func (fixture *yu16RuntimeFixture) singleEntry(t *testing.T, operationID string) audit.Entry {
	t.Helper()
	page, err := fixture.auditStore.Query(t.Context(), audit.Query{Operation: operationID})
	if err != nil {
		t.Fatalf("query audit entries: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("audit entries for %s = %d, want 1", operationID, len(page.Entries))
	}
	return page.Entries[0]
}

func (fixture *yu16RuntimeFixture) assertNoSecrets(t *testing.T) {
	t.Helper()
	rows, err := fixture.repository.Database().Query(`SELECT
COALESCE(id, ''), COALESCE(organization_id, ''), COALESCE(project_id, ''), COALESCE(actor_id, ''),
COALESCE(operation, ''), COALESCE(scope_id, ''), COALESCE(target_type, ''), COALESCE(target_id, ''),
COALESCE(reason_code, ''), COALESCE(trace_id, ''), COALESCE(request_id, ''), COALESCE(correlation_id, ''),
COALESCE(diff_summary, ''), COALESCE(metadata, '')
FROM iotd_audit_entries`)
	if err != nil {
		t.Fatalf("read audit text surface: %v", err)
	}
	defer rows.Close()
	var text strings.Builder
	for rows.Next() {
		columns := make([]string, 14)
		destinations := make([]any, len(columns))
		for index := range columns {
			destinations[index] = &columns[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatalf("scan audit text surface: %v", err)
		}
		text.WriteString(strings.Join(columns, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit text surface: %v", err)
	}
	persisted := text.String()
	for _, sentinel := range []string{"YU16-password-secret", "YU16-authorization-secret", "YU16-session-secret", "YU16-csrf-secret"} {
		if strings.Contains(persisted, sentinel) {
			t.Fatalf("audit persisted sensitive sentinel %q in %q", sentinel, persisted)
		}
	}
}
