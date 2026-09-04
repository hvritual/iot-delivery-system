package audit

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	_ "modernc.org/sqlite"
	"yunka.io/framework/operation"
	"yunka.io/pkg/operationplan"
)

func TestApplyMigrationsCreatesAppendOnlyAuditSchema(t *testing.T) {
	database := openTestDatabase(t)

	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	for _, column := range []string{
		"id", "sequence", "schema_version", "event_category", "organization_id", "project_id",
		"actor_type", "actor_id", "operation", "authorization_decision", "scope_type", "scope_id",
		"target_type", "target_id", "result", "reason_code", "trace_id", "request_id",
		"correlation_id", "diff_summary", "metadata", "occurred_at", "recorded_at",
	} {
		var found string
		if err := database.QueryRow(`SELECT name FROM pragma_table_info('iotd_audit_entries') WHERE name = ?`, column).Scan(&found); err != nil {
			t.Fatalf("audit column %q is required: %v", column, err)
		}
	}
	assertSQLiteIndexes(t, database, []string{
		"idx_iotd_audit_entries_organization_time",
		"idx_iotd_audit_entries_project_time",
		"idx_iotd_audit_entries_actor_time",
		"idx_iotd_audit_entries_operation_time",
		"idx_iotd_audit_entries_target_time",
		"idx_iotd_audit_entries_trace_time",
		"idx_iotd_audit_entries_correlation_time",
	})
	assertSQLiteObjects(t, database, "trigger", []string{
		"iotd_audit_entries_append_only_update",
		"iotd_audit_entries_append_only_delete",
	})
	assertMigrationRecordedOnce(t, database)
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("repeat audit migrations: %v", err)
	}
	assertMigrationRecordedOnce(t, database)
}

func TestSQLiteStoreAppendsAndReadsCompleteEntriesInSequence(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("create audit SQLite store: %v", err)
	}
	first, err := store.Append(t.Context(), completeEntry("audit-001"))
	if err != nil {
		t.Fatalf("append first audit entry: %v", err)
	}
	second, err := store.Append(t.Context(), completeEntry("audit-002"))
	if err != nil {
		t.Fatalf("append second audit entry: %v", err)
	}
	if first.Sequence == 0 || second.Sequence != first.Sequence+1 {
		t.Fatalf("sequences = %d, %d; want strict monotonic increase", first.Sequence, second.Sequence)
	}
	got, err := store.ByID(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("read first audit entry: %v", err)
	}
	if got != first {
		t.Fatalf("audit readback = %#v, want %#v", got, first)
	}
	for _, statement := range []string{
		`UPDATE iotd_audit_entries SET reason_code = 'tampered' WHERE id = 'audit-001'`,
		`DELETE FROM iotd_audit_entries WHERE id = 'audit-001'`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, actor_type, operation, authorization_decision, scope_type, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('audit-001', 1, 'authorization', 'human', 'delivery.items.create', 'allowed', 'project', 'success', 'duplicate', '{}', '{}', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, actor_type, operation, authorization_decision, scope_type, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('bad-enum', 1, 'invalid', 'human', 'delivery.items.create', 'allowed', 'project', 'success', 'invalid_enum', '{}', '{}', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, actor_type, operation, authorization_decision, scope_type, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('bad-json', 1, 'authorization', 'human', 'delivery.items.create', 'allowed', 'project', 'success', 'invalid_json', 'not-json', '{}', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, actor_type, operation, authorization_decision, scope_type, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('bad-time', 1, 'authorization', 'human', 'delivery.items.create', 'allowed', 'project', 'success', 'invalid_time', '{}', '{}', '2026-09-04T08:00:00+08:00', '2026-09-04T00:00:00Z')`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, actor_type, actor_id, operation, authorization_decision, scope_type, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('bad-anonymous-actor', 1, 'authorization', 'anonymous', 'unknown', 'delivery.items.create', 'allowed', 'system', 'success', 'invalid_actor', '{}', '{}', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')`,
		`INSERT INTO iotd_audit_entries (id, schema_version, event_category, project_id, actor_type, actor_id, operation, authorization_decision, scope_type, scope_id, result, reason_code, diff_summary, metadata, occurred_at, recorded_at) VALUES ('bad-project-organization', 1, 'authorization', 'project-a', 'human', 'user-a', 'delivery.items.create', 'allowed', 'object', 'target-a', 'success', 'invalid_project', '{}', '{}', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')`,
	} {
		if _, err := database.Exec(statement); err == nil {
			t.Fatalf("append-only audit schema accepted invalid mutation: %s", statement)
		}
	}
}

func TestSQLiteStoreAssignsVersionAndRecordedAtFromUTCClock(t *testing.T) {
	database := openMigratedTestDatabase(t)
	clockValue := time.Date(2026, 9, 4, 8, 0, 1, 123456789, time.FixedZone("test-zone", 8*60*60))
	store, err := NewSQLiteStore(database, WithClock(func() time.Time { return clockValue }))
	if err != nil {
		t.Fatalf("create clocked audit SQLite store: %v", err)
	}
	entry := completeEntry("audit-clocked")
	if entry.SchemaVersion != SchemaVersion {
		t.Fatalf("entry schema version = %d, want public SchemaVersion %d", entry.SchemaVersion, SchemaVersion)
	}
	got, err := store.Append(t.Context(), entry)
	if err != nil {
		t.Fatalf("append clocked audit entry: %v", err)
	}
	wantRecordedAt := clockValue.UTC()
	if !got.RecordedAt.Equal(wantRecordedAt) || got.RecordedAt.Location() != time.UTC {
		t.Fatalf("recorded at = %s (%s), want %s UTC", got.RecordedAt, got.RecordedAt.Location(), wantRecordedAt)
	}
	readback, err := store.ByID(t.Context(), entry.ID)
	if err != nil || !readback.RecordedAt.Equal(wantRecordedAt) || readback.RecordedAt.Nanosecond() != wantRecordedAt.Nanosecond() {
		t.Fatalf("clocked audit readback = %#v error=%v, want recorded at %s", readback, err, wantRecordedAt)
	}
	entry = completeEntry("audit-forged-recorded-at")
	entry.RecordedAt = wantRecordedAt
	if _, err := store.Append(t.Context(), entry); err == nil {
		t.Fatal("audit entry with caller-supplied recorded at unexpectedly appended")
	}
}

func TestSQLiteStoreRejectsInvalidEntryValuesAndAllowsAnonymousFailure(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("create audit SQLite store: %v", err)
	}
	for _, mutate := range []func(*Entry){
		func(entry *Entry) { entry.SchemaVersion = SchemaVersion + 1 },
		func(entry *Entry) { entry.ActorType = "unknown" },
		func(entry *Entry) { entry.DiffSummary = "[]" },
		func(entry *Entry) { entry.Metadata = `{"token":"must-not-store"}` },
		func(entry *Entry) { entry.OccurredAt = entry.OccurredAt.In(time.FixedZone("not-utc", 8*60*60)) },
		func(entry *Entry) { entry.RecordedAt = time.Now().UTC() },
		func(entry *Entry) { entry.ActorType, entry.ActorID = ActorAnonymous, "unknown" },
		func(entry *Entry) { entry.ActorID = "" },
		func(entry *Entry) { entry.OrganizationID = "" },
		func(entry *Entry) { entry.ScopeType, entry.ScopeID = ScopeSystem, "unexpected" },
		func(entry *Entry) { entry.ScopeType, entry.ScopeID = ScopeOrganization, "different-org" },
		func(entry *Entry) { entry.ScopeType, entry.ScopeID = ScopeProject, "different-project" },
		func(entry *Entry) { entry.ScopeType, entry.ScopeID = ScopeObject, "" },
	} {
		entry := completeEntry("invalid")
		mutate(&entry)
		if _, err := store.Append(t.Context(), entry); err == nil {
			t.Fatalf("invalid audit entry %#v unexpectedly appended", entry)
		}
	}
	anonymous := Entry{
		ID:                    "audit-anonymous-failure",
		SchemaVersion:         SchemaVersion,
		EventCategory:         EventCategoryAuthentication,
		ActorType:             ActorAnonymous,
		Operation:             "authentication.login",
		AuthorizationDecision: DecisionNotEvaluated,
		ScopeType:             ScopeSystem,
		Result:                ResultFailure,
		ReasonCode:            "authentication.invalid_credentials",
		DiffSummary:           `{}`,
		Metadata:              `{"provider":"local"}`,
		OccurredAt:            time.Date(2026, 9, 4, 0, 0, 0, 123456789, time.UTC),
	}
	got, err := store.Append(t.Context(), anonymous)
	if err != nil {
		t.Fatalf("append anonymous authentication failure: %v", err)
	}
	if got.ActorID != "" || got.OrganizationID != "" || got.ProjectID != "" || got.TargetID != "" {
		t.Fatalf("anonymous failure readback = %#v, want no invented identity or target", got)
	}
	if !got.OccurredAt.Equal(anonymous.OccurredAt) {
		t.Fatalf("anonymous occurred at drifted: got %#v want %#v", got, anonymous)
	}
	if got.RecordedAt.IsZero() || got.RecordedAt.Location() != time.UTC {
		t.Fatalf("anonymous recorded at = %s (%s), want store-assigned UTC time", got.RecordedAt, got.RecordedAt.Location())
	}
}

func TestSQLiteStoreUsesYunkaTransactionContext(t *testing.T) {
	database := openMigratedTestDatabase(t)
	store, err := NewSQLiteStore(database)
	if err != nil {
		t.Fatalf("create audit SQLite store: %v", err)
	}
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	plan := operationplan.Plan{OperationID: "audit.entries.append", Execution: operationplan.Execution{Transaction: "local", Idempotency: "none"}}
	if _, err := executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		_, appendErr := store.Append(ctx, completeEntry("audit-committed"))
		return nil, appendErr
	}); err != nil {
		t.Fatalf("append committed audit entry in Yunka transaction: %v", err)
	}
	if _, err := store.ByID(t.Context(), "audit-committed"); err != nil {
		t.Fatalf("read committed audit entry: %v", err)
	}
	if _, err := executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		if _, appendErr := store.Append(ctx, completeEntry("audit-rolled-back")); appendErr != nil {
			return nil, appendErr
		}
		return nil, errors.New("force audit rollback")
	}); err == nil || err.Error() != "force audit rollback" {
		t.Fatalf("rollback error = %v, want forced rollback", err)
	}
	if _, err := store.ByID(t.Context(), "audit-rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back audit entry read error = %v, want ErrNotFound", err)
	}
}

func TestApplyMigrationsUpgradesAndRollsBackOnConflict(t *testing.T) {
	database := openTestDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply existing identity migrations: %v", err)
	}
	if _, err := database.Exec(`
INSERT INTO organizations (id, slug, name) VALUES ('org-before', 'org-before', 'Organization Before');
CREATE TABLE iotd_delivery_items (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
CREATE TABLE iotd_outbox (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
INSERT INTO iotd_delivery_items (id, payload) VALUES ('item-before', '{"title":"keep"}');
INSERT INTO iotd_outbox (id, payload) VALUES ('event-before', '{"type":"keep"}');`); err != nil {
		t.Fatalf("prepare existing database: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("upgrade existing database: %v", err)
	}
	for _, check := range []struct {
		query string
		want  string
	}{
		{query: `SELECT name FROM organizations WHERE id = 'org-before'`, want: "Organization Before"},
		{query: `SELECT payload FROM iotd_delivery_items WHERE id = 'item-before'`, want: `{"title":"keep"}`},
		{query: `SELECT payload FROM iotd_outbox WHERE id = 'event-before'`, want: `{"type":"keep"}`},
	} {
		var got string
		if err := database.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("existing data after audit upgrade = %q, %v; want %q", got, err, check.want)
		}
	}
	assertMigrationRecordedOnce(t, database)

	conflict := openTestDatabase(t)
	if _, err := conflict.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL); CREATE TABLE iotd_audit_entries (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("prepare conflicting audit database: %v", err)
	}
	if err := ApplyMigrations(t.Context(), conflict); err == nil {
		t.Fatal("audit migration with a conflicting table unexpectedly succeeded")
	}
	var count int
	if err := conflict.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed audit migration ledger count = %d error=%v, want 0", count, err)
	}
	var table string
	if err := conflict.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'iotd_audit_entries'`).Scan(&table); err != nil || table != "iotd_audit_entries" {
		t.Fatalf("conflicting audit table was not preserved: %q, %v", table, err)
	}
}

func TestApplyMigrationsFailsClosedWhenRecordedSchemaDrifts(t *testing.T) {
	for _, mutation := range []struct {
		name  string
		apply func(t *testing.T, database *sql.DB)
	}{
		{
			name: "audit table is missing",
			apply: func(t *testing.T, database *sql.DB) {
				t.Helper()
				if _, err := database.Exec(`DROP TABLE iotd_audit_entries`); err != nil {
					t.Fatalf("drop audit table: %v", err)
				}
			},
		},
		{
			name: "append-only trigger is missing",
			apply: func(t *testing.T, database *sql.DB) {
				t.Helper()
				if _, err := database.Exec(`DROP TRIGGER iotd_audit_entries_append_only_update`); err != nil {
					t.Fatalf("drop audit trigger: %v", err)
				}
			},
		},
		{
			name: "query index is missing",
			apply: func(t *testing.T, database *sql.DB) {
				t.Helper()
				if _, err := database.Exec(`DROP INDEX idx_iotd_audit_entries_trace_time`); err != nil {
					t.Fatalf("drop audit index: %v", err)
				}
			},
		},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			database := openMigratedTestDatabase(t)
			mutation.apply(t, database)
			if err := ApplyMigrations(t.Context(), database); err == nil {
				t.Fatal("ledger-present schema drift unexpectedly passed migration")
			}
			assertMigrationRecordedOnce(t, database)
		})
	}
}

func completeEntry(id string) Entry {
	return Entry{
		ID:                    id,
		SchemaVersion:         SchemaVersion,
		EventCategory:         EventCategoryAuthorization,
		OrganizationID:        "org-a",
		ProjectID:             "project-a",
		ActorType:             ActorHuman,
		ActorID:               "user-a",
		Operation:             "delivery.items.create",
		AuthorizationDecision: DecisionAllowed,
		ScopeType:             ScopeProject,
		ScopeID:               "project-a",
		TargetType:            "delivery.item",
		TargetID:              "item-a",
		Result:                ResultSuccess,
		ReasonCode:            "authorization.allowed",
		TraceID:               "trace-a",
		RequestID:             "request-a",
		CorrelationID:         "correlation-a",
		DiffSummary:           `{"changed":["title"]}`,
		Metadata:              `{"source":"test"}`,
		OccurredAt:            time.Date(2026, 9, 4, 0, 0, 0, 123456789, time.UTC),
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func openMigratedTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database := openTestDatabase(t)
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply audit migrations: %v", err)
	}
	return database
}

func assertSQLiteObjects(t *testing.T, database *sql.DB, objectType string, names []string) {
	t.Helper()
	for _, name := range names {
		var found string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = ? AND name = ?`, objectType, name).Scan(&found); err != nil || found != name {
			t.Fatalf("SQLite %s %q is required, found=%q error=%v", objectType, name, found, err)
		}
	}
}

func assertSQLiteIndexes(t *testing.T, database *sql.DB, names []string) {
	t.Helper()
	for _, name := range names {
		var found string
		if err := database.QueryRow(`SELECT name FROM pragma_index_list('iotd_audit_entries') WHERE name = ?`, name).Scan(&found); err != nil || found != name {
			t.Fatalf("SQLite index %q is required, found=%q error=%v", name, found, err)
		}
	}
}

func assertMigrationRecordedOnce(t *testing.T, database *sql.DB) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit migration ledger count = %d error=%v, want 1", count, err)
	}
}
