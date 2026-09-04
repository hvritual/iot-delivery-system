package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	application "github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"yunka.io/framework/core/identity"
	"yunka.io/framework/core/runtimecontext"
	"yunka.io/framework/execution"
	"yunka.io/framework/operation"
	"yunka.io/gateway/authz"
)

func TestSuccessfulCreateRecordsAuditEntryInTheSameSQLiteTransaction(t *testing.T) {
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	auditStore, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite audit store: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	audited, err := application.NewAuditedDeliveryService(
		application.NewAdapter(service),
		auditStore,
		application.WithAuditIDGenerator(func() (string, error) { return "audit-create-a", nil }),
		application.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		t.Fatalf("assemble audited delivery application: %v", err)
	}
	operations := application.NewOperations(
		audited,
		operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())}),
	)
	ctx := identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodJWT,
		TenantID:      "org-a",
		UserID:        "user-a",
		Roles:         []string{localauth.RoleLocalAdmin},
	})
	ctx = runtimecontext.WithTraceID(ctx, "trace-create-a")

	item, err := operations.Create(ctx, delivery.CreateInput{Title: "audit me", Board: delivery.BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatalf("create delivery item: %v", err)
	}
	if _, err := repository.Get(t.Context(), item.ID); err != nil {
		t.Fatalf("read committed delivery item: %v", err)
	}
	if snapshot, err := outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 1 {
		t.Fatalf("committed outbox = %#v, %v; want one event", snapshot, err)
	}

	var entries int
	if err := repository.Database().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM iotd_audit_entries").Scan(&entries); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if entries != 1 {
		t.Fatalf("successful create audit entries = %d, want 1", entries)
	}
}

func TestAllRegisteredSuccessfulWriteOperationsRecordOneSafeAuditEntry(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		operation string
		actorID   string
		call      func(t *testing.T, fixture *auditedFixture) (targetID, outboxSubject string, err error)
		wantType  string
		wantEvent string
	}{
		{
			name: "create project", operation: "delivery.projects.create", actorID: "user-a", wantType: "delivery.project", wantEvent: "delivery.project.created",
			call: func(_ *testing.T, fixture *auditedFixture) (string, string, error) {
				project, err := fixture.operations.CreateProject(fixture.admin, delivery.ProjectInput{Name: "project", Board: delivery.BoardResearchDelivery, Owner: "owner"})
				return project.ID, project.ID, err
			},
		},
		{
			name: "create item", operation: "delivery.items.create", actorID: "user-a", wantType: "delivery.work-item", wantEvent: "delivery.work-item.created",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				project := fixture.project(t)
				item, err := fixture.operations.Create(fixture.admin, delivery.CreateInput{Title: "item", Board: delivery.BoardResearchDelivery, Owner: "owner", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
				return item.ID, item.ID, err
			},
		},
		{
			name: "update item", operation: "delivery.items.update", actorID: "user-a", wantType: "delivery.work-item", wantEvent: "delivery.work-item.updated",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				item := fixture.item(t)
				title := "changed"
				updated, err := fixture.operations.UpdateWorkItem(fixture.admin, item.ID, delivery.WorkItemUpdate{Title: &title})
				return updated.ID, updated.ID, err
			},
		},
		{
			name: "create comment", operation: "delivery.items.comment.create", actorID: "user-a", wantType: "delivery.comment", wantEvent: "delivery.work-item.commented",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				item := fixture.item(t)
				comment, err := fixture.operations.AddComment(fixture.admin, item.ID, delivery.CommentInput{Body: "not retained in audit"})
				return comment.ID, item.ID, err
			},
		},
		{
			name: "update context", operation: "delivery.items.update-context", actorID: "user-a", wantType: "delivery.work-item", wantEvent: "delivery.work-item.context-updated",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				plan := "private plan text"
				item, err := fixture.operations.UpdateContext(fixture.admin, fixture.item(t).ID, delivery.ContextUpdate{Plan: &plan})
				return item.ID, item.ID, err
			},
		},
		{
			name: "advance gate", operation: "delivery.items.advance-gate", actorID: "user-a", wantType: "delivery.work-item", wantEvent: "delivery.work-item.gate-advanced",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				item, err := fixture.operations.AdvanceGate(fixture.admin, fixture.item(t).ID, delivery.GateSolutionReviewed, []delivery.Evidence{{Kind: "review", Title: "reviewed"}})
				return item.ID, item.ID, err
			},
		},
		{
			name: "close item", operation: "delivery.items.close", actorID: "reviewer-a", wantType: "delivery.work-item", wantEvent: "delivery.work-item.closed",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				item := fixture.item(t)
				for _, gate := range []delivery.Gate{delivery.GateSolutionReviewed, delivery.GateDevelopmentCompleted, delivery.GateTestPassed} {
					if _, err := fixture.operations.AdvanceGate(fixture.admin, item.ID, gate, []delivery.Evidence{{Kind: "test", Title: string(gate)}}); err != nil {
						return "", "", err
					}
				}
				if _, err := fixture.operations.AdvanceGate(fixture.reviewer, item.ID, delivery.GateProductionValidated, []delivery.Evidence{{Kind: "validation", Title: "independent"}}); err != nil {
					return "", "", err
				}
				closed, err := fixture.operations.Close(fixture.reviewer, item.ID, "not retained in audit")
				return closed.ID, closed.ID, err
			},
		},
		{
			name: "create release", operation: "delivery.releases.create", actorID: "user-a", wantType: "delivery.release", wantEvent: "delivery.release.created",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				release, err := fixture.operations.CreateRelease(fixture.admin, delivery.ReleaseInput{ProjectID: fixture.project(t).ID, Name: "release", Version: "v1", Status: "planned"})
				return release.ID, release.ID, err
			},
		},
		{
			name: "create sprint", operation: "delivery.sprints.create", actorID: "user-a", wantType: "delivery.sprint", wantEvent: "delivery.sprint.created",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				sprint, err := fixture.operations.CreateSprint(fixture.admin, delivery.SprintInput{ProjectID: fixture.project(t).ID, Name: "sprint", Goal: "goal", StartDate: "2026-09-01", EndDate: "2026-09-08", Status: "planned"})
				return sprint.ID, sprint.ID, err
			},
		},
		{
			name: "create milestone", operation: "delivery.milestones.create", actorID: "user-a", wantType: "delivery.milestone", wantEvent: "delivery.milestone.created",
			call: func(t *testing.T, fixture *auditedFixture) (string, string, error) {
				milestone, err := fixture.operations.CreateMilestone(fixture.admin, delivery.MilestoneInput{ProjectID: fixture.project(t).ID, Name: "milestone", TargetDate: "2026-09-30", Status: "planned"})
				return milestone.ID, milestone.ID, err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAuditedFixture(t)
			targetID, outboxSubject, err := testCase.call(t, fixture)
			if err != nil {
				t.Fatalf("%s: %v", testCase.operation, err)
			}
			entry := fixture.entryForOperation(t, testCase.operation)
			if entry.EventCategory != audit.EventCategoryDelivery || entry.SchemaVersion != audit.SchemaVersion || entry.ActorType != audit.ActorHuman || entry.ActorID != testCase.actorID || entry.AuthorizationDecision != audit.DecisionAllowed || entry.Result != audit.ResultSuccess || entry.ReasonCode != "delivery.change.applied" {
				t.Fatalf("%s audit identity/result = %#v", testCase.operation, entry)
			}
			if entry.OrganizationID != "org-a" || entry.Operation != testCase.operation || entry.TargetType != testCase.wantType || entry.TargetID != targetID || entry.TraceID != "trace-a" || entry.RequestID != "request-a" || entry.CorrelationID != "correlation-a" || !strings.Contains(entry.Metadata, `"transport":"test"`) || strings.Contains(entry.Metadata, "not-audited") {
				t.Fatalf("%s audit scope/target/trace = %#v", testCase.operation, entry)
			}
			if testCase.operation == "delivery.projects.create" {
				if entry.ScopeType != audit.ScopeOrganization || entry.ScopeID != "org-a" || entry.ProjectID != "" {
					t.Fatalf("project create scope = %#v", entry)
				}
			} else if entry.ScopeType != audit.ScopeProject || entry.ScopeID == "" || entry.ProjectID != entry.ScopeID {
				t.Fatalf("%s project scope = %#v", testCase.operation, entry)
			}
			var summary map[string]json.RawMessage
			if err := json.Unmarshal([]byte(entry.DiffSummary), &summary); err != nil || summary["change"] == nil || summary["fields"] == nil || strings.Contains(strings.ToLower(entry.DiffSummary), "private") || strings.Contains(strings.ToLower(entry.DiffSummary), "retained") {
				t.Fatalf("%s diff summary = %q, %v", testCase.operation, entry.DiffSummary, err)
			}
			fixture.assertOutboxEvent(t, testCase.wantEvent, outboxSubject)
		})
	}
}

func TestAuditFailureOrMissingTrustedActorRollsBackBusinessAndOutbox(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		store     func(*audit.SQLiteStore) audit.Store
		newID     func() (string, error)
		principal identity.Principal
	}{
		{
			name:      "ID generation failure",
			store:     func(store *audit.SQLiteStore) audit.Store { return store },
			newID:     func() (string, error) { return "", errors.New("ID source unavailable") },
			principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Roles: []string{localauth.RoleLocalAdmin}},
		},
		{
			name:      "append failure",
			store:     func(*audit.SQLiteStore) audit.Store { return failingAuditStore{} },
			newID:     func() (string, error) { return "audit-append-failure", nil },
			principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Roles: []string{localauth.RoleLocalAdmin}},
		},
		{
			name:      "unsupported trusted actor",
			store:     func(store *audit.SQLiteStore) audit.Store { return store },
			newID:     func() (string, error) { return "audit-invalid-actor", nil },
			principal: identity.Principal{Authenticated: true, AuthMethod: "unsupported", TenantID: "org-a", UserID: "user-a", Roles: []string{localauth.RoleLocalAdmin}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, outbox, operations, ctx := newAuditFailureFixture(t, testCase.store, testCase.newID, testCase.principal)
			if _, err := operations.Create(ctx, delivery.CreateInput{Title: "must roll back", Board: delivery.BoardResearchDelivery, Owner: "owner"}); err == nil {
				t.Fatal("create succeeded despite audit failure")
			}
			for _, table := range []string{"iotd_delivery_items", "iotd_outbox", "iotd_audit_entries"} {
				var count int
				if err := repository.Database().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
					t.Fatalf("count %s: %v", table, err)
				}
				if count != 0 {
					t.Fatalf("%s rows = %d, want rollback to zero", table, count)
				}
			}
			if snapshot, err := outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 {
				t.Fatalf("outbox after audit failure = %#v, %v", snapshot, err)
			}
		})
	}
}

func TestReadOperationsDoNotAppendAuditEntries(t *testing.T) {
	t.Parallel()

	fixture := newAuditedFixture(t)
	if _, err := fixture.operations.List(fixture.admin); err != nil {
		t.Fatalf("list items: %v", err)
	}
	var count int
	if err := fixture.repository.Database().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM iotd_audit_entries").Scan(&count); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("read operation audit entries = %d, want 0", count)
	}
}

func TestServiceTokenAuditActorUsesStableServiceAccountID(t *testing.T) {
	t.Parallel()

	fixture := newAuditedFixture(t)
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodServiceToken,
		TenantID:      "org-a",
		Subject:       "service-account/release-bot",
		Roles:         []string{localauth.RoleLocalAdmin},
	})
	ctx = runtimecontext.WithTraceID(ctx, "trace-service-a")
	if _, err := fixture.operations.Create(ctx, delivery.CreateInput{Title: "service change", Board: delivery.BoardResearchDelivery, Owner: "service"}); err != nil {
		t.Fatalf("create as service: %v", err)
	}
	entry := fixture.entryForOperation(t, "delivery.items.create")
	if entry.ActorType != audit.ActorService || entry.ActorID != "release-bot" || entry.OrganizationID != "org-a" || entry.TraceID != "trace-service-a" {
		t.Fatalf("service audit actor = %#v", entry)
	}
}

func TestAuditedApplicationRejectsMissingOrMismatchedOperationPlanContextBeforeWriting(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		operation   string
		metadataOp  string
		transaction execution.TransactionMode
	}{
		{name: "missing execution frame"},
		{name: "non-local execution transaction", operation: "delivery.items.create", metadataOp: "delivery.items.create", transaction: execution.TransactionReadOnly},
		{name: "mismatched execution operation", operation: "delivery.items.update", metadataOp: "delivery.items.update"},
		{name: "mismatched runtime metadata", operation: "delivery.items.create", metadataOp: "delivery.items.update"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newAuditedFixture(t)
			ctx := fixture.admin
			var root *execution.Root
			if testCase.operation != "" {
				ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Operation: testCase.metadataOp, Transport: "test", RequestID: "request-a"})
				var err error
				transaction := testCase.transaction
				if transaction == "" {
					transaction = execution.TransactionLocal
				}
				ctx, root, err = execution.BeginRoot(ctx, testCase.operation, transaction, nil, localtx.NewSQLiteFactory(fixture.repository.Database()))
				if err != nil {
					t.Fatalf("begin mismatched execution root: %v", err)
				}
			}
			if _, err := fixture.audited.CreateItem(ctx, &deliveryv1.CreateItemRequest{Title: "must not write", Board: string(delivery.BoardResearchDelivery), Owner: "owner"}); err == nil || !strings.Contains(err.Error(), "audit execution") {
				t.Fatalf("direct audited create error = %v, want operation-plan context rejection", err)
			}
			if root != nil {
				if err := root.Rollback(t.Context()); err != nil {
					t.Fatalf("rollback rejected direct execution root: %v", err)
				}
			}
			fixture.assertNoCommittedWrites(t)
		})
	}
}

type failingAuditStore struct{}

func (failingAuditStore) Append(context.Context, audit.Entry) (audit.Entry, error) {
	return audit.Entry{}, errors.New("audit append unavailable")
}

func (failingAuditStore) ByID(context.Context, string) (audit.Entry, error) {
	return audit.Entry{}, errors.New("audit entry unavailable")
}

func newAuditFailureFixture(t *testing.T, selectStore func(*audit.SQLiteStore) audit.Store, newID func() (string, error), principal identity.Principal) (*delivery.SQLiteRepository, *localoutbox.SQLiteStore, *application.Operations, context.Context) {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	store, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	audited, err := application.NewAuditedDeliveryService(application.NewAdapter(service), selectStore(store), application.WithAuditIDGenerator(newID), application.WithWorkItemResolver(service.Get))
	if err != nil {
		t.Fatalf("assemble audited application: %v", err)
	}
	ctx := identity.WithPrincipal(t.Context(), principal)
	ctx = runtimecontext.WithTraceID(ctx, "trace-failure-a")
	return repository, outbox, application.NewOperations(audited, operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())})), ctx
}

type auditedFixture struct {
	repository *delivery.SQLiteRepository
	service    *delivery.Service
	seed       *application.Operations
	operations *application.Operations
	audited    application.DeliveryService
	store      *audit.SQLiteStore
	outbox     *localoutbox.SQLiteStore
	admin      context.Context
	reviewer   context.Context
}

func newAuditedFixture(t *testing.T) *auditedFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("open SQLite repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if err := audit.ApplyMigrations(t.Context(), repository.Database()); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	store, err := audit.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	outbox, err := localoutbox.NewSQLiteStore(repository.Database())
	if err != nil {
		t.Fatalf("open SQLite outbox: %v", err)
	}
	authorizer, err := localauth.NewAuthorizer()
	if err != nil {
		t.Fatalf("create local authorizer: %v", err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, nil)
	if err != nil {
		t.Fatalf("create execution security: %v", err)
	}
	service := delivery.NewService(repository, nil, delivery.NewTransactionalOutboxStager(outbox))
	sequence := 0
	audited, err := application.NewAuditedDeliveryService(
		application.NewAdapter(service),
		store,
		application.WithAuditIDGenerator(func() (string, error) {
			sequence++
			return fmt.Sprintf("audit-%03d", sequence), nil
		}),
		application.WithWorkItemResolver(service.Get),
	)
	if err != nil {
		t.Fatalf("assemble audited application: %v", err)
	}
	newContext := func(userID string) context.Context {
		ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: userID, Roles: []string{localauth.RoleLocalAdmin}})
		ctx = runtimecontext.WithTraceID(ctx, "trace-a")
		return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "test", RequestID: "request-a", Attributes: map[string]string{"correlation_id": "correlation-a", "ignored": "not-audited"}})
	}
	executor := operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(repository.Database())})
	return &auditedFixture{
		repository: repository,
		service:    service,
		seed:       application.NewOperations(application.NewAdapter(service), executor),
		operations: application.NewOperations(audited, executor),
		audited:    audited,
		store:      store,
		outbox:     outbox,
		admin:      newContext("user-a"),
		reviewer:   newContext("reviewer-a"),
	}
}

func (fixture *auditedFixture) assertNoCommittedWrites(t *testing.T) {
	t.Helper()
	for _, table := range []string{"iotd_delivery_items", "iotd_outbox", "iotd_audit_entries"} {
		var count int
		if err := fixture.repository.Database().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d, want zero", table, count)
		}
	}
}

func (fixture *auditedFixture) assertOutboxEvent(t *testing.T, eventType, subject string) {
	t.Helper()
	var count int
	if err := fixture.repository.Database().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM iotd_outbox
WHERE json_extract(envelope_json, '$.type') = ? AND json_extract(envelope_json, '$.subject') = ?`, eventType, subject).Scan(&count); err != nil {
		t.Fatalf("count outbox event %s/%s: %v", eventType, subject, err)
	}
	if count != 1 {
		t.Fatalf("outbox event %s/%s count = %d, want 1", eventType, subject, count)
	}
}

func (fixture *auditedFixture) project(t *testing.T) delivery.Project {
	t.Helper()
	project, err := fixture.seed.CreateProject(fixture.admin, delivery.ProjectInput{Name: "project", Board: delivery.BoardResearchDelivery, Owner: "owner"})
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return project
}

func (fixture *auditedFixture) item(t *testing.T) delivery.WorkItem {
	t.Helper()
	project := fixture.project(t)
	item, err := fixture.seed.Create(fixture.admin, delivery.CreateInput{Title: "item", Board: delivery.BoardResearchDelivery, Owner: "owner", ProjectID: project.ID, Kind: delivery.WorkItemKindTask})
	if err != nil {
		t.Fatalf("seed item: %v", err)
	}
	return item
}

func (fixture *auditedFixture) entryForOperation(t *testing.T, operationID string) audit.Entry {
	t.Helper()
	rows, err := fixture.repository.Database().QueryContext(t.Context(), "SELECT id FROM iotd_audit_entries WHERE operation = ?", operationID)
	if err != nil {
		t.Fatalf("query %s audit entries: %v", operationID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan audit ID: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit IDs: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("%s audit entries = %d, want 1", operationID, len(ids))
	}
	entry, err := fixture.store.ByID(t.Context(), ids[0])
	if err != nil {
		t.Fatalf("read audit entry %q: %v", ids[0], err)
	}
	return entry
}
