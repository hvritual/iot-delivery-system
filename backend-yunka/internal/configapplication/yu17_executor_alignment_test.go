package configapplication

import (
	"errors"
	"reflect"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configrevision"
	frameworkoutbox "github.com/hvritual/yunka.io/framework/event/outbox"
)

func TestYU17InternalConfigPlansRemainCanonicalAndTransportless(t *testing.T) {
	plans := ConfigOperationPlans()
	want := map[string]struct {
		permission  string
		transaction string
	}{
		"config.revisions.change":   {permission: "config.revisions.write", transaction: "local"},
		"config.revisions.compare":  {permission: "config.revisions.read", transaction: "read_only"},
		"config.revisions.rollback": {permission: "config.revisions.rollback", transaction: "local"},
	}
	if len(plans) != len(want) {
		t.Fatalf("registered config plan count = %d, want %d", len(plans), len(want))
	}
	seen := make(map[string]bool, len(plans))
	for _, plan := range plans {
		expected, ok := want[plan.OperationID]
		if !ok || seen[plan.OperationID] {
			t.Fatalf("unexpected or duplicate config plan registration: %#v", plan)
		}
		seen[plan.OperationID] = true
		if plan.Domain != "config" || plan.Application != "revisions" ||
			!reflect.DeepEqual(plan.Security.Authentication, []string{"jwt", "service-token"}) ||
			!reflect.DeepEqual(plan.Security.Permissions, []string{expected.permission}) ||
			plan.Security.PermissionMode != "all" || plan.Execution.Transaction != expected.transaction ||
			plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" ||
			plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 {
			t.Fatalf("config plan %q drifted from internal canonical contract: %#v", plan.OperationID, plan)
		}
	}
}

func TestYU17RecordingExecutorKeepsChangeCompareRollbackAtomic(t *testing.T) {
	fixture := newFixture(t)
	operations := yu17RecordingOperations(t, fixture, fixture.operations.audit, fixture.operations.outbox)
	ctx := fixture.context(t)

	first, err := operations.Change(ctx, configrevision.ChangeInput{
		Kind:      configrevision.KindMembership,
		ConfigKey: "yu17/canonical",
		Payload:   `{"enabled":true,"members":["a"]}`,
	})
	if err != nil || first.Revision != 1 {
		t.Fatalf("first change = %#v, %v", first, err)
	}
	second, err := operations.Change(ctx, configrevision.ChangeInput{
		Kind:                   configrevision.KindMembership,
		ConfigKey:              "yu17/canonical",
		ExpectedParentRevision: 1,
		Payload:                `{"enabled":false,"members":["a","b"]}`,
	})
	if err != nil || second.Revision != 2 {
		t.Fatalf("second change = %#v, %v", second, err)
	}

	beforeCompareRevisions, beforeCompareAudits, beforeCompareOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
	differences, err := operations.Compare(ctx, configrevision.CompareInput{
		Kind:          configrevision.KindMembership,
		ConfigKey:     "yu17/canonical",
		LeftRevision:  1,
		RightRevision: 2,
	})
	wantDiff := []Difference{{Path: "/enabled", Change: "changed"}, {Path: "/members/1", Change: "added"}}
	if err != nil || !reflect.DeepEqual(differences, wantDiff) {
		t.Fatalf("compare = %#v, %v; want %#v", differences, err, wantDiff)
	}
	if fixture.revisionCount(t) != beforeCompareRevisions || fixture.auditCount(t) != beforeCompareAudits || fixture.outboxCount(t) != beforeCompareOutbox {
		t.Fatal("read-only compare changed revision, audit, or Outbox state")
	}

	rolledBack, err := operations.Rollback(ctx, configrevision.RollbackInput{
		Kind:                   configrevision.KindMembership,
		ConfigKey:              "yu17/canonical",
		ExpectedParentRevision: 2,
		SourceRevision:         1,
	})
	if err != nil || rolledBack.Revision.Revision != 3 || rolledBack.SourceRevision != 1 || rolledBack.Revision.Payload != first.Payload {
		t.Fatalf("rollback = %#v, %v", rolledBack, err)
	}
	if fixture.revisionCount(t) != 3 || fixture.outboxCount(t) != 3 {
		t.Fatalf("committed config state revisions=%d outbox=%d, want 3/3", fixture.revisionCount(t), fixture.outboxCount(t))
	}
	success, failure := yu17AuditCounts(t, fixture)
	if success != 3 || failure != 0 {
		t.Fatalf("config audits success=%d failure=%d, want 3/0", success, failure)
	}
}

func TestYU17CASConflictRollsBackAndRecordsConfigurationFailure(t *testing.T) {
	fixture := newFixture(t)
	operations := yu17RecordingOperations(t, fixture, fixture.operations.audit, fixture.operations.outbox)
	ctx := fixture.context(t)

	if _, err := operations.Change(ctx, configrevision.ChangeInput{
		Kind:      configrevision.KindRoleBinding,
		ConfigKey: "yu17/cas",
		Payload:   `{"role":"viewer"}`,
	}); err != nil {
		t.Fatalf("seed config revision: %v", err)
	}
	beforeRevisions, beforeOutbox := fixture.revisionCount(t), fixture.outboxCount(t)
	beforeSuccess, beforeFailure := yu17AuditCounts(t, fixture)
	_, err := operations.Change(ctx, configrevision.ChangeInput{
		Kind:                   configrevision.KindRoleBinding,
		ConfigKey:              "yu17/cas",
		ExpectedParentRevision: 0,
		Payload:                `{"role":"administrator"}`,
	})
	if !errors.Is(err, configrevision.ErrRevisionConflict) {
		t.Fatalf("stale config change error = %v, want ErrRevisionConflict", err)
	}
	if fixture.revisionCount(t) != beforeRevisions || fixture.outboxCount(t) != beforeOutbox {
		t.Fatal("CAS conflict left a config revision or Outbox event")
	}
	afterSuccess, afterFailure := yu17AuditCounts(t, fixture)
	if afterSuccess != beforeSuccess || afterFailure != beforeFailure+1 {
		t.Fatalf("CAS audit counts success=%d failure=%d, before %d/%d", afterSuccess, afterFailure, beforeSuccess, beforeFailure)
	}
	var category, result, reason string
	if err := fixture.database.QueryRow(`SELECT event_category, result, reason_code FROM iotd_audit_entries WHERE operation = 'config.revisions.change' AND result = 'failure' ORDER BY sequence DESC LIMIT 1`).Scan(&category, &result, &reason); err != nil {
		t.Fatalf("read CAS failure audit: %v", err)
	}
	if category != string(audit.EventCategoryConfiguration) || result != string(audit.ResultFailure) || reason != "configuration.transaction_rolled_back" {
		t.Fatalf("CAS failure audit category=%q result=%q reason=%q", category, result, reason)
	}
}

func TestYU17AuditAndOutboxFailuresRollbackBeforeConfigurationFailureAudit(t *testing.T) {
	for _, test := range []struct {
		name       string
		auditStore audit.Store
		outbox     func(*fixture) frameworkoutbox.TransactionalStore
	}{
		{
			name:       "audit failure",
			auditStore: failingAuditStore{},
			outbox:     func(fixture *fixture) frameworkoutbox.TransactionalStore { return fixture.operations.outbox },
		},
		{
			name:       "Outbox failure",
			auditStore: nil,
			outbox:     func(*fixture) frameworkoutbox.TransactionalStore { return failingOutboxStore{} },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFixture(t)
			applicationAudit := test.auditStore
			if applicationAudit == nil {
				applicationAudit = fixture.operations.audit
			}
			operations := yu17RecordingOperations(t, fixture, applicationAudit, test.outbox(fixture))
			_, err := operations.Change(fixture.context(t), configrevision.ChangeInput{
				Kind:      configrevision.KindDomainDictionary,
				ConfigKey: "yu17/failure",
				Payload:   `{"version":1}`,
			})
			if err == nil {
				t.Fatal("forced persistence failure unexpectedly succeeded")
			}
			if fixture.revisionCount(t) != 0 || fixture.outboxCount(t) != 0 {
				t.Fatalf("failure left revision=%d outbox=%d, want 0/0", fixture.revisionCount(t), fixture.outboxCount(t))
			}
			success, failure := yu17AuditCounts(t, fixture)
			if success != 0 || failure != 1 {
				t.Fatalf("failure audit counts success=%d failure=%d, want 0/1", success, failure)
			}
			var category, reason string
			if err := fixture.database.QueryRow(`SELECT event_category, reason_code FROM iotd_audit_entries WHERE operation = 'config.revisions.change' AND result = 'failure'`).Scan(&category, &reason); err != nil {
				t.Fatalf("read failure audit: %v", err)
			}
			if category != string(audit.EventCategoryConfiguration) || reason != "configuration.transaction_rolled_back" {
				t.Fatalf("failure audit category=%q reason=%q", category, reason)
			}
		})
	}
}

func yu17RecordingOperations(t *testing.T, fixture *fixture, applicationAudit audit.Store, outboxStore frameworkoutbox.TransactionalStore) *Operations {
	t.Helper()
	recorder, err := audit.NewSecurityRecorder(fixture.operations.audit)
	if err != nil {
		t.Fatalf("create security recorder: %v", err)
	}
	executor, err := audit.NewRecordingExecutor(fixture.executor, recorder)
	if err != nil {
		t.Fatalf("create recording executor: %v", err)
	}
	operations, err := New(
		fixture.store,
		applicationAudit,
		executor,
		WithOutbox(outboxStore),
		WithIDGenerator(fixture.nextID),
		WithClock(fixture.clock),
	)
	if err != nil {
		t.Fatalf("create runtime-shaped config operations: %v", err)
	}
	return operations
}

func yu17AuditCounts(t *testing.T, fixture *fixture) (success int, failure int) {
	t.Helper()
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE event_category = 'configuration' AND result = 'success'`).Scan(&success); err != nil {
		t.Fatalf("count config success audits: %v", err)
	}
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE event_category = 'configuration' AND result = 'failure'`).Scan(&failure); err != nil {
		t.Fatalf("count config failure audits: %v", err)
	}
	return success, failure
}
