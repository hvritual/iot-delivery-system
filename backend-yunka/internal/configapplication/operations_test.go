package configapplication

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/configrevision"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/principalauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/serviceauthz"
	_ "modernc.org/sqlite"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestOperationsChangeCompareAndRollbackUseTrustedScopeAuditAndImmutableChain(t *testing.T) {
	fixture := newFixture(t)
	first, err := fixture.operations.Change(fixture.context(t), configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "members/default", ExpectedParentRevision: 0, Payload: `{"members":["a"],"enabled":true}`})
	if err != nil || first.OrganizationID != "org-a" || first.Revision != 1 || first.CreatedByID != "user-a" {
		t.Fatalf("change result=%#v error=%v, want trusted organization/actor revision 1", first, err)
	}
	second, err := fixture.operations.Change(fixture.context(t), configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "members/default", ExpectedParentRevision: 1, Payload: `{"enabled":false,"members":["a","b"],"new":1}`})
	if err != nil || second.Revision != 2 {
		t.Fatalf("second change result=%#v error=%v, want revision 2", second, err)
	}
	differences, err := fixture.operations.Compare(fixture.context(t), configrevision.CompareInput{Kind: configrevision.KindMembership, ConfigKey: "members/default", LeftRevision: 1, RightRevision: 2})
	want := []Difference{{Path: "/enabled", Change: "changed"}, {Path: "/members/1", Change: "added"}, {Path: "/new", Change: "added"}}
	if err != nil || !reflect.DeepEqual(differences, want) {
		t.Fatalf("comparison=%#v error=%v, want %#v", differences, err, want)
	}
	if got := fixture.auditCount(t); got != 2 {
		t.Fatalf("compare wrote success audit count=%d, want 2 change-only audits", got)
	}
	rolledBack, err := fixture.operations.Rollback(fixture.context(t), configrevision.RollbackInput{Kind: configrevision.KindMembership, ConfigKey: "members/default", ExpectedParentRevision: 2, SourceRevision: 1})
	if err != nil || rolledBack.Revision.Revision != 3 || rolledBack.SourceRevision != 1 || rolledBack.Revision.Payload != first.Payload {
		t.Fatalf("rollback=%#v error=%v, want immutable revision 3 with source 1 payload", rolledBack, err)
	}
	if got := fixture.auditCount(t); got != 3 {
		t.Fatalf("successful writes audit count=%d, want 3", got)
	}
	var metadata, reasonCode string
	if err := fixture.database.QueryRow(`SELECT metadata, reason_code FROM iotd_audit_entries WHERE operation = 'config.revisions.rollback'`).Scan(&metadata, &reasonCode); err != nil || metadata != `{"kind":"membership","revision":3,"rollback_source_revision":1,"transport":"test"}` || reasonCode != "configuration.rollback.applied" {
		t.Fatalf("rollback audit metadata=%q reason=%q error=%v", metadata, reasonCode, err)
	}
}

func TestOperationsStageOutboxOnlyForCommittedConfigurationMutations(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	const sensitivePayloadSentinel = "S0-04-09-sensitive-configuration-payload"
	if got := fixture.outboxCount(t); got != 0 {
		t.Fatalf("initial Outbox count=%d, want 0", got)
	}
	first, err := fixture.operations.Change(ctx, configrevision.ChangeInput{
		Kind:      configrevision.KindMembership,
		ConfigKey: "members/outbox",
		Payload:   `{"opaque":"` + sensitivePayloadSentinel + `"}`,
	})
	if err != nil {
		t.Fatalf("create configuration revision: %v", err)
	}
	if got := fixture.outboxCount(t); got != 1 {
		t.Fatalf("configuration create Outbox count=%d, want 1", got)
	}
	rolledBack, err := fixture.operations.Rollback(ctx, configrevision.RollbackInput{
		Kind:                   configrevision.KindMembership,
		ConfigKey:              "members/outbox",
		ExpectedParentRevision: first.Revision,
		SourceRevision:         first.Revision,
	})
	if err != nil {
		t.Fatalf("rollback configuration revision: %v", err)
	}
	if got := fixture.outboxCount(t); got != 2 {
		t.Fatalf("configuration rollback Outbox count=%d, want 2", got)
	}
	rows, err := fixture.database.Query(`SELECT envelope_json FROM iotd_outbox`)
	if err != nil {
		t.Fatalf("read configuration Outbox envelopes: %v", err)
	}
	defer rows.Close()
	seen := map[string]event.Envelope{}
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			t.Fatalf("scan configuration Outbox envelope: %v", err)
		}
		if strings.Contains(encoded, sensitivePayloadSentinel) {
			t.Fatalf("configuration Outbox leaked sensitive payload sentinel: %q", encoded)
		}
		var envelope event.Envelope
		if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
			t.Fatalf("decode configuration Outbox envelope: %v", err)
		}
		if _, duplicate := seen[envelope.Type]; duplicate {
			t.Fatalf("duplicate configuration Outbox type %q", envelope.Type)
		}
		seen[envelope.Type] = envelope
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate configuration Outbox envelopes: %v", err)
	}
	for eventType, want := range map[string]struct {
		subject        string
		revision       int64
		rollbackSource int64
	}{
		configRevisionChangedEvent:    {subject: first.ID, revision: first.Revision},
		configRevisionRolledBackEvent: {subject: rolledBack.Revision.ID, revision: rolledBack.Revision.Revision, rollbackSource: first.Revision},
	} {
		envelope, found := seen[eventType]
		if !found || envelope.Topic != configRevisionEventTopic || envelope.Subject != want.subject {
			t.Fatalf("Outbox envelope %q = %#v", eventType, envelope)
		}
		var payload struct {
			OrganizationID         string              `json:"organizationId"`
			Kind                   configrevision.Kind `json:"kind"`
			ConfigKey              string              `json:"configKey"`
			Revision               int64               `json:"revision"`
			RollbackSourceRevision int64               `json:"rollbackSourceRevision"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.OrganizationID != "org-a" || payload.Kind != configrevision.KindMembership || payload.ConfigKey != "members/outbox" || payload.Revision != want.revision || payload.RollbackSourceRevision != want.rollbackSource {
			t.Fatalf("Outbox envelope %q payload=%#v error=%v", eventType, payload, err)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("configuration Outbox envelope count=%d, want 2", len(seen))
	}
}

func TestOperationsOutboxFailureRollsBackConfigurationRevisionAndAudit(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		prepare func(*testing.T, *fixture)
		run     func(*testing.T, *fixture, *Operations)
		assert  func(*testing.T, *fixture)
	}{
		{
			name:    "change",
			prepare: func(*testing.T, *fixture) {},
			run: func(t *testing.T, fixture *fixture, operations *Operations) {
				t.Helper()
				if _, err := operations.Change(fixture.context(t), configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "members/outbox-failure", Payload: `{"enabled":true}`}); err == nil {
					t.Fatal("Outbox failure unexpectedly committed configuration change")
				}
			},
		},
		{
			name: "rollback",
			prepare: func(t *testing.T, fixture *fixture) {
				t.Helper()
				first, err := fixture.operations.Change(fixture.context(t), configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "members/outbox-failure", Payload: `{"enabled":true}`})
				if err != nil {
					t.Fatalf("seed first configuration revision: %v", err)
				}
				second, err := fixture.operations.Change(fixture.context(t), configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "members/outbox-failure", ExpectedParentRevision: first.Revision, Payload: `{"enabled":false}`})
				if err != nil || second.Revision != 2 {
					t.Fatalf("seed second configuration revision=%#v error=%v", second, err)
				}
			},
			run: func(t *testing.T, fixture *fixture, operations *Operations) {
				t.Helper()
				if _, err := operations.Rollback(fixture.context(t), configrevision.RollbackInput{Kind: configrevision.KindMembership, ConfigKey: "members/outbox-failure", ExpectedParentRevision: 2, SourceRevision: 1}); err == nil {
					t.Fatal("Outbox failure unexpectedly committed configuration rollback")
				}
			},
			assert: func(t *testing.T, fixture *fixture) {
				t.Helper()
				history, err := fixture.store.History(t.Context(), "org-a", configrevision.KindMembership, "members/outbox-failure", 3)
				if err != nil || len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 {
					t.Fatalf("rollback Outbox failure changed immutable history=%#v error=%v", history, err)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newFixture(t)
			operations, err := New(fixture.store, fixture.operations.audit, fixture.executor, WithOutbox(failingOutboxStore{}), WithIDGenerator(fixture.nextID), WithClock(fixture.clock))
			if err != nil {
				t.Fatalf("construct operations with failing Outbox: %v", err)
			}
			scenario.prepare(t, fixture)
			beforeRevisions, beforeAudits, beforeOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
			scenario.run(t, fixture, operations)
			if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits || fixture.outboxCount(t) != beforeOutbox {
				t.Fatal("Outbox failure left a partial configuration, success audit, or Outbox write")
			}
			if scenario.assert != nil {
				scenario.assert(t, fixture)
			}
		})
	}
}

func TestDiffPreservesJSONPresenceNumbersAndRFC6901Paths(t *testing.T) {
	for name, input := range map[string]struct {
		left, right string
		want        []Difference
	}{
		"object null changed":     {left: `{"value":null}`, right: `{"value":"safe"}`, want: []Difference{{Path: "/value", Change: "changed"}}},
		"array null changed":      {left: `{"values":[null]}`, right: `{"values":[1]}`, want: []Difference{{Path: "/values/0", Change: "changed"}}},
		"missing key added":       {left: `{}`, right: `{"value":null}`, want: []Difference{{Path: "/value", Change: "added"}}},
		"missing index removed":   {left: `{"values":[null]}`, right: `{"values":[]}`, want: []Difference{{Path: "/values/0", Change: "removed"}}},
		"large integer changed":   {left: `{"value":9007199254740992}`, right: `{"value":9007199254740993}`, want: []Difference{{Path: "/value", Change: "changed"}}},
		"numeric spellings equal": {left: `{"one":1,"decimal":1.0,"exponent":1e0}`, right: `{"one":1.0,"decimal":1e0,"exponent":1}`, want: []Difference{}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := diff(input.left, input.right)
			if err != nil || !reflect.DeepEqual(got, input.want) {
				t.Fatalf("diff=%#v error=%v, want %#v", got, err, input.want)
			}
		})
	}

	got, err := diff(`{"a.b":1,"a/b":1,"a~b":1,"空 格":1}`, `{"a.b":2,"a/b":2,"a~b":2,"空 格":2}`)
	want := []Difference{{Path: "/a.b", Change: "changed"}, {Path: "/a~1b", Change: "changed"}, {Path: "/a~0b", Change: "changed"}, {Path: "/空 格", Change: "changed"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("escaped diff=%#v error=%v, want %#v", got, err, want)
	}
	seen := make(map[string]struct{}, len(got))
	for _, difference := range got {
		field := auditPath(difference.Path)
		if strings.Contains(field, "secret-value") {
			t.Fatalf("audit field leaked value: %q", field)
		}
		encoded, found := strings.CutPrefix(field, "p_")
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if !found || decodeErr != nil || string(decoded) != difference.Path {
			t.Fatalf("audit field=%q is not a reversible path encoding", field)
		}
		if _, duplicate := seen[field]; duplicate {
			t.Fatalf("audit field collision for %q", difference.Path)
		}
		seen[field] = struct{}{}
	}
}

func TestChangeUsesPersistedCanonicalPayloadForAuditDiff(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	first, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "canonical", Payload: `{"number":1}`})
	if err != nil || first.Payload != `{"number":1}` {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	for _, change := range []struct {
		revision int64
		payload  string
	}{{revision: 2, payload: `{"number":1.0}`}, {revision: 3, payload: " { \n \t\"number\" : 1e0 \n } "}} {
		revision, payload := change.revision, change.payload
		next, changeErr := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "canonical", ExpectedParentRevision: revision - 1, Payload: payload})
		if changeErr != nil || next.Payload != first.Payload {
			t.Fatalf("revision %d=%#v error=%v, want canonical payload %q", revision, next, changeErr, first.Payload)
		}
		var summary, reasonCode string
		if err := fixture.database.QueryRow(`SELECT diff_summary, reason_code FROM iotd_audit_entries WHERE operation = 'config.revisions.change' AND target_id = 'canonical' ORDER BY sequence DESC LIMIT 1`).Scan(&summary, &reasonCode); err != nil || summary != `{"change":"changed","fields":[]}` || reasonCode != "configuration.change.applied" {
			t.Fatalf("revision %d audit summary=%q reason=%q error=%v", revision, summary, reasonCode, err)
		}
	}
}

func TestInitialChangeAndEscapedKeysPersistValueFreeReversibleAuditPaths(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	first, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "escaped", Payload: `{"a.b":"harmless-sentinel","a/b":"harmless-sentinel","a~b":"harmless-sentinel","空 格":"harmless-sentinel"}`})
	if err != nil || first.Revision != 1 {
		t.Fatalf("first=%#v error=%v", first, err)
	}
	var rootSummary string
	if err := fixture.database.QueryRow(`SELECT diff_summary FROM iotd_audit_entries WHERE operation = 'config.revisions.change' ORDER BY sequence LIMIT 1`).Scan(&rootSummary); err != nil || rootSummary != `{"change":"changed","fields":["p_"]}` {
		t.Fatalf("initial audit=%q error=%v", rootSummary, err)
	}
	if _, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "escaped", ExpectedParentRevision: 1, Payload: `{"a.b":"safe","a/b":"safe","a~b":"safe","空 格":"safe"}`}); err != nil {
		t.Fatal(err)
	}
	var summary string
	if err := fixture.database.QueryRow(`SELECT diff_summary FROM iotd_audit_entries WHERE operation = 'config.revisions.change' ORDER BY sequence DESC LIMIT 1`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Fields []string `json:"fields"`
	}
	if err := json.Unmarshal([]byte(summary), &persisted); err != nil || strings.Contains(summary, "harmless-sentinel") {
		t.Fatalf("unsafe audit summary=%q error=%v", summary, err)
	}
	want := map[string]struct{}{"/a.b": {}, "/a~1b": {}, "/a~0b": {}, "/空 格": {}}
	if len(persisted.Fields) != len(want) {
		t.Fatalf("audit fields=%#v", persisted.Fields)
	}
	for _, field := range persisted.Fields {
		encoded, found := strings.CutPrefix(field, "p_")
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		if !found || decodeErr != nil {
			t.Fatalf("audit field=%q decode error=%v", field, decodeErr)
		}
		if _, exists := want[string(decoded)]; !exists {
			t.Fatalf("audit field pointer=%q is unexpected", decoded)
		}
		delete(want, string(decoded))
	}
	if len(want) != 0 {
		t.Fatalf("missing audit pointers=%#v", want)
	}
}

func TestConfigOperationPlansAreDefensiveAndStrict(t *testing.T) {
	first := ConfigOperationPlans()
	if len(first) != 3 {
		t.Fatalf("plan count=%d, want 3", len(first))
	}
	first[0].Security.Authentication[0] = "forged"
	first[0].Security.Permissions[0] = "forged.permission"
	second := ConfigOperationPlans()
	if second[0].Security.Authentication[0] != "jwt" || second[0].Security.Permissions[0] != "config.revisions.write" || changePlan.Security.Authentication[0] != "jwt" || changePlan.Security.Permissions[0] != "config.revisions.write" {
		t.Fatalf("mutable plan slices leaked: %#v", second[0])
	}
	want := map[string]struct {
		permission  string
		transaction string
	}{
		"config.revisions.change":   {permission: "config.revisions.write", transaction: "local"},
		"config.revisions.compare":  {permission: "config.revisions.read", transaction: "read_only"},
		"config.revisions.rollback": {permission: "config.revisions.rollback", transaction: "local"},
	}
	for _, plan := range second {
		expected, ok := want[plan.OperationID]
		if !ok || plan.Security.PermissionMode != "all" || !reflect.DeepEqual(plan.Security.Authentication, []string{"jwt", "service-token"}) || !reflect.DeepEqual(plan.Security.Permissions, []string{expected.permission}) || plan.Execution.Transaction != expected.transaction || plan.Execution.Idempotency != "none" || plan.Composition.Boundary != "local" || plan.Bindings.RPC != "" || len(plan.Bindings.HTTP) != 0 {
			t.Fatalf("plan contract drift: %#v", plan)
		}
	}
}

func TestOperationsPreserveNonNotFoundParentReadErrors(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	for _, seed := range []struct {
		revision int64
		payload  string
	}{
		{revision: 1, payload: `{"value":1}`},
		{revision: 2, payload: `{"value":2}`},
	} {
		if _, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "read-error", ExpectedParentRevision: seed.revision - 1, Payload: seed.payload}); err != nil {
			t.Fatalf("seed revision %d: %v", seed.revision, err)
		}
	}
	for name, parentErr := range map[string]error{"context canceled": context.Canceled, "stored corruption": errors.New("stored config chain corruption")} {
		t.Run(name, func(t *testing.T) {
			operations, err := New(parentReadErrorStore{Store: fixture.store, revision: 2, err: parentErr}, fixture.operations.audit, fixture.executor, WithOutbox(fixture.operations.outbox), WithIDGenerator(fixture.nextID), WithClock(fixture.clock))
			if err != nil {
				t.Fatal(err)
			}
			beforeRevisions, beforeAudits, beforeOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
			if _, err := operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "read-error", ExpectedParentRevision: 2, Payload: `{"value":3}`}); !errors.Is(err, parentErr) || errors.Is(err, configrevision.ErrRevisionConflict) {
				t.Fatalf("change parent error=%v, want preserved %v", err, parentErr)
			}
			if _, err := operations.Rollback(ctx, configrevision.RollbackInput{Kind: configrevision.KindMembership, ConfigKey: "read-error", ExpectedParentRevision: 2, SourceRevision: 1}); !errors.Is(err, parentErr) || errors.Is(err, configrevision.ErrRevisionConflict) {
				t.Fatalf("rollback parent error=%v, want preserved %v", err, parentErr)
			}
			if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits || fixture.outboxCount(t) != beforeOutbox {
				t.Fatal("parent read failure mutated persistence")
			}
		})
	}
}

func TestOperationsRejectsConflictAndAuditFailureWithoutRevisionOrAuditSideEffects(t *testing.T) {
	fixture := newFixture(t)
	context := fixture.context(t)
	if _, err := fixture.operations.Change(context, configrevision.ChangeInput{Kind: configrevision.KindRoleBinding, ConfigKey: "roles/default", Payload: `{"role":"viewer"}`}); err != nil {
		t.Fatal(err)
	}
	beforeRevisions, beforeAudits := fixture.revisionCount(t), fixture.auditCount(t)
	if _, err := fixture.operations.Change(context, configrevision.ChangeInput{Kind: configrevision.KindRoleBinding, ConfigKey: "roles/default", ExpectedParentRevision: 0, Payload: `{"role":"admin"}`}); !errors.Is(err, configrevision.ErrRevisionConflict) {
		t.Fatalf("stale change error=%v, want ErrRevisionConflict", err)
	}
	if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits {
		t.Fatal("CAS conflict changed revisions or audits")
	}
	failing, err := New(fixture.store, failingAuditStore{}, fixture.executor, WithOutbox(fixture.operations.outbox), WithIDGenerator(fixture.nextID), WithClock(fixture.clock))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Change(context, configrevision.ChangeInput{Kind: configrevision.KindRoleBinding, ConfigKey: "roles/default", ExpectedParentRevision: 1, Payload: `{"role":"editor"}`}); err == nil {
		t.Fatal("audit failure unexpectedly committed configuration revision")
	}
	if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits {
		t.Fatal("audit failure left a revision or success audit")
	}
}

func TestOperationsUsePersistedServiceGrantAndRejectMissingOrCrossOrganizationServiceAuthority(t *testing.T) {
	fixture := newFixture(t)
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name) VALUES ('org-b', 'org-b', 'Organization B')`,
		`INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'Config service')`,
		`INSERT INTO iotd_config_service_grants (id, organization_id, service_account_id, operation_id, permission_id) VALUES ('config-grant-a', 'org-a', 'service-a', 'config.revisions.change', 'config.revisions.write')`,
	} {
		if _, err := fixture.database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.operations.Change(fixture.serviceContext(t, "org-a"), configrevision.ChangeInput{Kind: configrevision.KindDomainDictionary, ConfigKey: "dictionary/default", Payload: `{"version":1}`}); err != nil {
		t.Fatalf("persisted service grant denied: %v", err)
	}
	beforeRevisions, beforeAudits := fixture.revisionCount(t), fixture.auditCount(t)
	if _, err := fixture.operations.Compare(fixture.serviceContext(t, "org-a"), configrevision.CompareInput{Kind: configrevision.KindDomainDictionary, ConfigKey: "dictionary/default", LeftRevision: 1, RightRevision: 1}); err == nil {
		t.Fatal("service missing compare permission unexpectedly succeeded")
	}
	if _, err := fixture.operations.Change(fixture.serviceContext(t, "org-b"), configrevision.ChangeInput{Kind: configrevision.KindDomainDictionary, ConfigKey: "dictionary/default", Payload: `{"version":2}`}); err == nil {
		t.Fatal("cross-organization service grant unexpectedly succeeded")
	}
	if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits {
		t.Fatal("service denial changed revisions or audits")
	}
}

func TestOperationsAllConfigurationKindsFollowImmutableAuditedRevisionChain(t *testing.T) {
	for _, kind := range []configrevision.Kind{configrevision.KindIdentityProvider, configrevision.KindMembership, configrevision.KindRoleBinding, configrevision.KindDomainDictionary} {
		t.Run(string(kind), func(t *testing.T) {
			fixture := newFixture(t)
			ctx := fixture.context(t)
			const sentinel = "harmless-sentinel-value"
			first, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: kind, ConfigKey: "default", Payload: `{"value":"` + sentinel + `","items":[9007199254740993]}`})
			if err != nil || first.Revision != 1 || first.ParentRevision != 0 || first.OrganizationID != "org-a" || first.CreatedByID != "user-a" {
				t.Fatalf("first=%#v err=%v", first, err)
			}
			second, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: kind, ConfigKey: "default", ExpectedParentRevision: 1, Payload: `{"value":"safe","items":[9007199254740993,2]}`})
			if err != nil || second.Revision != 2 || second.ParentRevision != 1 {
				t.Fatalf("second=%#v err=%v", second, err)
			}
			diff, err := fixture.operations.Compare(ctx, configrevision.CompareInput{Kind: kind, ConfigKey: "default", LeftRevision: 1, RightRevision: 2})
			if err != nil || len(diff) != 2 || diff[0].Path != "/items/1" || diff[1].Path != "/value" {
				t.Fatalf("diff=%#v err=%v", diff, err)
			}
			before := fixture.auditCount(t)
			result, err := fixture.operations.Rollback(ctx, configrevision.RollbackInput{Kind: kind, ConfigKey: "default", ExpectedParentRevision: 2, SourceRevision: 1})
			if err != nil || result.Revision.Revision != 3 || result.Revision.ParentRevision != 2 || result.SourceRevision != 1 || result.Revision.Payload != first.Payload {
				t.Fatalf("rollback=%#v err=%v", result, err)
			}
			if fixture.auditCount(t) != before+1 {
				t.Fatal("compare or rollback audit count is incorrect")
			}
			history, err := fixture.store.History(ctx, "org-a", kind, "default", 3)
			if err != nil || len(history) != 3 || history[0] != first || history[1] != second {
				t.Fatalf("history=%#v err=%v", history, err)
			}
			rows, err := fixture.database.Query(`SELECT operation, target_id, diff_summary, metadata, trace_id, request_id, correlation_id FROM iotd_audit_entries ORDER BY sequence`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			count := 0
			for rows.Next() {
				var operation, target, summary, metadata, trace, request, correlation string
				if err := rows.Scan(&operation, &target, &summary, &metadata, &trace, &request, &correlation); err != nil {
					t.Fatal(err)
				}
				if target != "default" || trace != "trace-a" || request != "request-a" || correlation != "correlation-a" || strings.Contains(summary+metadata, sentinel) {
					t.Fatalf("unsafe audit %#v", []string{operation, target, summary, metadata})
				}
				count++
			}
			if count != 3 {
				t.Fatalf("audits=%d", count)
			}
		})
	}
}

func TestOperationsRollbackFailuresLeaveNoRevisionOrAuditSideEffects(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	if _, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "failure", Payload: `{"value":"harmless-sentinel"}`}); err != nil {
		t.Fatal(err)
	}
	beforeRevisions, beforeAudits, beforeOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
	for name, input := range map[string]configrevision.RollbackInput{
		"missing source": {Kind: configrevision.KindMembership, ConfigKey: "failure", ExpectedParentRevision: 1, SourceRevision: 99},
		"stale parent":   {Kind: configrevision.KindMembership, ConfigKey: "failure", ExpectedParentRevision: 0, SourceRevision: 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fixture.operations.Rollback(ctx, input)
			if err == nil || (name == "stale parent" && !errors.Is(err, configrevision.ErrRevisionConflict)) {
				t.Fatalf("rollback error=%v", err)
			}
			if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits || fixture.outboxCount(t) != beforeOutbox {
				t.Fatal("rollback failure mutated revisions, audits, or outbox")
			}
		})
	}
}

func TestOperationsRollbackAuditFailureLeavesNoRevisionAuditOrOutboxSideEffects(t *testing.T) {
	fixture := newFixture(t)
	ctx := fixture.context(t)
	first, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "rollback-audit-failure", Payload: `{"value":"harmless-sentinel"}`})
	if err != nil || first.Revision != 1 {
		t.Fatalf("first change=%#v error=%v", first, err)
	}
	second, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "rollback-audit-failure", ExpectedParentRevision: 1, Payload: `{"value":"safe"}`})
	if err != nil || second.Revision != 2 {
		t.Fatalf("second change=%#v error=%v", second, err)
	}
	beforeRevisions, beforeAudits, beforeOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
	failing, err := New(fixture.store, failingAuditStore{}, fixture.executor, WithOutbox(fixture.operations.outbox), WithIDGenerator(fixture.nextID), WithClock(fixture.clock))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Rollback(ctx, configrevision.RollbackInput{Kind: configrevision.KindMembership, ConfigKey: "rollback-audit-failure", ExpectedParentRevision: 2, SourceRevision: 1}); err == nil {
		t.Fatal("rollback audit failure unexpectedly committed revision")
	} else if strings.Contains(err.Error(), "harmless-sentinel") {
		t.Fatalf("rollback error leaked payload sentinel: %v", err)
	}
	if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits || fixture.outboxCount(t) != beforeOutbox {
		t.Fatal("rollback audit failure mutated revisions, audits, or outbox")
	}
	history, err := fixture.store.History(ctx, "org-a", configrevision.KindMembership, "rollback-audit-failure", 3)
	if err != nil || len(history) != 2 || history[0] != first || history[1] != second {
		t.Fatalf("history=%#v error=%v, want unchanged revisions 1 and 2", history, err)
	}
}

func TestOperationsFailClosedForInvalidConstructionAndPrincipals(t *testing.T) {
	fixture := newFixture(t)
	if _, err := New(nil, nil, nil); err == nil {
		t.Fatal("nil dependencies accepted")
	}
	if _, err := New(fixture.store, fixture.operations.audit, fixture.executor, nil); err == nil {
		t.Fatal("nil option accepted")
	}
	if _, err := New(fixture.store, fixture.operations.audit, fixture.executor, WithIDGenerator(nil)); err == nil {
		t.Fatal("nil generator accepted")
	}
	if _, err := New(fixture.store, fixture.operations.audit, fixture.executor, WithClock(nil)); err == nil {
		t.Fatal("nil clock accepted")
	}
	if _, err := New(fixture.store, fixture.operations.audit, fixture.executor, WithIDGenerator(fixture.nextID), WithClock(fixture.clock)); err == nil {
		t.Fatal("missing Outbox accepted")
	}
	var zero *Operations
	if _, err := zero.Change(t.Context(), configrevision.ChangeInput{}); err == nil {
		t.Fatal("zero operations accepted")
	}
	beforeRevisions, beforeAudits, beforeOutbox := fixture.revisionCount(t), fixture.auditCount(t), fixture.outboxCount(t)
	for name, principal := range map[string]identity.Principal{
		"missing": {}, "unauthenticated": {Authenticated: false}, "empty tenant": {Authenticated: true, AuthMethod: identity.AuthMethodJWT, UserID: "user-a"}, "missing user": {Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a"}, "bad service": {Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "bad"}, "service user": {Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/service-a", UserID: "user-a"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := identity.WithPrincipal(t.Context(), principal)
			_, err := fixture.operations.Change(ctx, configrevision.ChangeInput{Kind: configrevision.KindMembership, ConfigKey: "denied", Payload: `{"value":"harmless-sentinel"}`})
			if err == nil {
				t.Fatal("invalid principal accepted")
			}
			if fixture.revisionCount(t) != beforeRevisions || fixture.auditCount(t) != beforeAudits || fixture.outboxCount(t) != beforeOutbox {
				t.Fatal("denial mutated revisions, audits, or outbox")
			}
		})
	}
}

type fixture struct {
	database   *sql.DB
	store      *configrevision.SQLiteStore
	operations *Operations
	executor   operation.Executor
	clock      func() time.Time
	nextID     func() (string, error)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := configrevision.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	outboxStore, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A')`,
		`INSERT INTO users (id, organization_id, display_name) VALUES ('user-a', 'org-a', 'Alice')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id) VALUES ('binding-a', 'org-a', 'system-administrator', 'organization', 'org-a', 'user-a')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	clock := func() time.Time { return time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC) }
	store, err := configrevision.NewSQLiteStore(database, configrevision.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database, audit.WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	humans, err := humanauthz.NewGrantResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	services, err := serviceauthz.NewGrantResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := principalauthz.New(humans, services)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(resolver)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := deliveryauthz.NewOperationGuard(delivery.NewMemoryRepository(), database)
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guard.GuardResolver())
	if err != nil {
		t.Fatal(err)
	}
	executor := operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	n := 0
	nextID := func() (string, error) { n++; return "config-audit-" + string(rune('a'+n)), nil }
	operations, err := New(store, auditStore, executor, WithOutbox(outboxStore), WithIDGenerator(nextID), WithClock(clock))
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{database: database, store: store, operations: operations, executor: executor, clock: clock, nextID: nextID}
}

func (fixture *fixture) context(t *testing.T) context.Context {
	t.Helper()
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a"})
	ctx = runtimecontext.WithTraceID(ctx, "trace-a")
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "test", RequestID: "request-a", Attributes: map[string]string{correlationIDAttribute: "correlation-a"}})
}

func (fixture *fixture) serviceContext(t *testing.T, organizationID string) context.Context {
	t.Helper()
	ctx := identity.WithPrincipal(t.Context(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: organizationID, Subject: "service-account/service-a"})
	ctx = runtimecontext.WithTraceID(ctx, "trace-service-a")
	return runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "test", RequestID: "request-service-a", Attributes: map[string]string{correlationIDAttribute: "correlation-service-a"}})
}

func (fixture *fixture) revisionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_config_revisions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func (fixture *fixture) auditCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (fixture *fixture) outboxCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRow(`SELECT COUNT(*) FROM iotd_outbox`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type failingAuditStore struct{}

func (failingAuditStore) Append(context.Context, audit.Entry) (audit.Entry, error) {
	return audit.Entry{}, errors.New("audit unavailable")
}
func (failingAuditStore) ByID(context.Context, string) (audit.Entry, error) {
	return audit.Entry{}, audit.ErrNotFound
}

type failingOutboxStore struct{}

func (failingOutboxStore) EnqueueTx(context.Context, any, event.Envelope) error {
	return errors.New("Outbox unavailable")
}

type parentReadErrorStore struct {
	configrevision.Store
	revision int64
	err      error
}

func (store parentReadErrorStore) ByRevision(ctx context.Context, organizationID string, kind configrevision.Kind, configKey string, revision int64) (configrevision.ConfigRevision, error) {
	if revision == store.revision {
		return configrevision.ConfigRevision{}, store.err
	}
	return store.Store.ByRevision(ctx, organizationID, kind, configKey, revision)
}
