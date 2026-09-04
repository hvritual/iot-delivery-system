package configrevision

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestAppendConditionallyReturnsFinalSuccessfulResultAfterBoundedBusyRetries(t *testing.T) {
	want := fixedResult{affected: 1}
	executor := &eventualExecutor{busyCalls: 64, result: want}
	got, err := appendConditionally(t.Context(), executor, true, "INSERT")
	if err != nil || got == nil || executor.attempts != 65 {
		t.Fatalf("final retry result=%v error=%v attempts=%d, want successful result after 65 calls", got, err, executor.attempts)
	}
	affected, err := got.RowsAffected()
	if err != nil || affected != 1 {
		t.Fatalf("final retry affected=%d error=%v, want 1", affected, err)
	}
}

func TestApplyMigrationsRejectsIsolatedSemanticDrift(t *testing.T) {
	for _, test := range []struct{ name, old, replacement string }{
		{"id type", "id TEXT PRIMARY KEY NOT NULL", "id INTEGER PRIMARY KEY NOT NULL"},
		{"id nullable", "id TEXT PRIMARY KEY NOT NULL", "id TEXT PRIMARY KEY"},
		{"kind whitelist", "kind TEXT NOT NULL CHECK (kind IN ('identity_provider', 'membership', 'role_binding', 'domain_dictionary'))", "kind TEXT NOT NULL"},
		{"payload object", "payload TEXT NOT NULL CHECK (json_valid(payload) AND json_type(payload) = 'object')", "payload TEXT NOT NULL"},
		{"hash length", "payload_hash BLOB NOT NULL CHECK (length(payload_hash) = 32)", "payload_hash BLOB NOT NULL"},
		{"actor whitelist", "created_by_type TEXT NOT NULL CHECK (created_by_type IN ('human', 'service', 'system'))", "created_by_type TEXT NOT NULL"},
		{"created time", "created_at TEXT NOT NULL CHECK (created_at GLOB '????-??-??T??:??:??*Z' AND strftime('%s', created_at) IS NOT NULL)", "created_at TEXT NOT NULL"},
		{"lookup ascending", "config_key, revision DESC", "config_key, revision ASC"},
		{"update when false", "BEFORE UPDATE ON iotd_config_revisions\nBEGIN", "BEFORE UPDATE ON iotd_config_revisions WHEN 0\nBEGIN"},
		{"delete when false", "BEFORE DELETE ON iotd_config_revisions\nBEGIN", "BEFORE DELETE ON iotd_config_revisions WHEN 0\nBEGIN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			if _, err := database.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL); INSERT INTO iotd_schema_migrations VALUES ('S0-04-05_config_revisions_v1', '2026-09-04T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
			schema := strings.Replace(configRevisionSchema, test.old, test.replacement, 1)
			if _, err := database.Exec(schema); err != nil {
				t.Fatalf("prepare %s drift: %v", test.name, err)
			}
			if err := ApplyMigrations(t.Context(), database); err == nil {
				t.Fatalf("%s drift unexpectedly passed", test.name)
			}
		})
	}
}

func TestAllReadPathsRejectIncompleteRevisionChain(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	store := newStore(t, database)
	if _, err := database.Exec(`INSERT INTO iotd_config_revisions (id, organization_id, kind, config_key, revision, parent_revision, payload, payload_hash, created_by_type, created_by_id, created_at) VALUES ('chain-gap', 'org-main', 'membership', 'all-read-gap', 3, 2, '{}', ?, 'human', 'user-main', '2026-09-04T00:00:00.000000000Z')`, sha256Bytes(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ByRevision(t.Context(), "org-main", KindMembership, "all-read-gap", 3); err == nil {
		t.Fatal("ByRevision returned incomplete chain")
	}
	if _, err := store.Latest(t.Context(), "org-main", KindMembership, "all-read-gap"); err == nil {
		t.Fatal("Latest returned incomplete chain")
	}
	if _, err := store.History(t.Context(), "org-main", KindMembership, "all-read-gap", MaxHistoryLimit); err == nil {
		t.Fatal("History returned incomplete chain")
	}
	for _, revision := range []int64{0, -1, math.MaxInt64} {
		if _, err := store.ByRevision(t.Context(), "org-main", KindMembership, "all-read-gap", revision); err == nil {
			t.Fatalf("ByRevision(%d) unexpectedly succeeded", revision)
		}
	}
}

type eventualExecutor struct {
	busyCalls, attempts int
	result              sql.Result
}

func (executor *eventualExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	executor.attempts++
	if executor.attempts <= executor.busyCalls {
		return nil, errors.New("database is locked")
	}
	return executor.result, nil
}
func (*eventualExecutor) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
func (*eventualExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

type fixedResult struct{ affected int64 }

func (result fixedResult) LastInsertId() (int64, error) { return 0, nil }
func (result fixedResult) RowsAffected() (int64, error) { return result.affected, nil }
