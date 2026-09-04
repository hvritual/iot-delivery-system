package configrevision

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestBusyRetryDoesNotRetryYunkaTransactionsAndRespectsCanceledContext(t *testing.T) {
	const sentinel = "S0-04-05-BUSY-SENTINEL"
	transactional := &busyExecutor{err: errors.New("database is locked " + sentinel)}
	if _, err := appendConditionally(t.Context(), transactional, false, "INSERT"); !errors.Is(err, ErrStoreBusy) || transactional.attempts != 1 || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("transactional busy error=%v attempts=%d, want one classified non-leaking failure", err, transactional.attempts)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	direct := &busyExecutor{err: errors.New("database is locked " + sentinel)}
	if _, err := appendConditionally(canceled, direct, true, "INSERT"); !errors.Is(err, context.Canceled) || direct.attempts != 1 || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("canceled direct busy error=%v attempts=%d, want context cancellation without leak", err, direct.attempts)
	}
}

type busyExecutor struct {
	err      error
	attempts int
}

func (executor *busyExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	executor.attempts++
	return nil, executor.err
}

func (*busyExecutor) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

func (*busyExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}

func TestApplyMigrationsRejectsSemanticSchemaDrift(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	for _, statement := range []string{
		`DROP INDEX idx_iotd_config_revisions_lookup`,
		`CREATE INDEX idx_iotd_config_revisions_lookup ON iotd_config_revisions (config_key, organization_id)`,
		`DROP TRIGGER iotd_config_revisions_append_only_update`,
		`CREATE TRIGGER iotd_config_revisions_append_only_update AFTER UPDATE ON iotd_config_revisions BEGIN SELECT 1; END`,
		`DROP TRIGGER iotd_config_revisions_append_only_delete`,
		`CREATE TRIGGER iotd_config_revisions_append_only_delete BEFORE DELETE ON iotd_config_revisions BEGIN SELECT 1; END`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("prepare semantic drift %q: %v", statement, err)
		}
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("same-name index and no-op triggers unexpectedly passed migration verification")
	}
}

func TestApplyMigrationsRejectsLedgeredTableWithoutForeignKeyChecksAndUniqueChain(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL);
INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES ('S0-04-05_config_revisions_v1', '2026-09-04T00:00:00Z');
CREATE TABLE iotd_config_revisions (
 id TEXT PRIMARY KEY, organization_id TEXT NOT NULL, kind TEXT NOT NULL, config_key TEXT NOT NULL,
 revision INTEGER NOT NULL, parent_revision INTEGER NOT NULL, payload TEXT NOT NULL, payload_hash BLOB NOT NULL,
 created_by_type TEXT NOT NULL, created_by_id TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX idx_iotd_config_revisions_lookup ON iotd_config_revisions (organization_id, kind, config_key, revision DESC);
CREATE TRIGGER iotd_config_revisions_append_only_update BEFORE UPDATE ON iotd_config_revisions BEGIN SELECT RAISE(ABORT, 'config revisions are append-only'); END;
CREATE TRIGGER iotd_config_revisions_append_only_delete BEFORE DELETE ON iotd_config_revisions BEGIN SELECT RAISE(ABORT, 'config revisions are append-only'); END;`); err != nil {
		t.Fatalf("prepare weak ledgered schema: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("ledgered table without FK/check/unique chain unexpectedly passed")
	}
}

func TestReadPathsRejectCorruptRowsAndHistoryEnforcesCompletePrefix(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	store := newStore(t, database)
	for _, row := range []struct {
		key     string
		id      string
		payload string
		hash    []byte
	}{
		{key: "hash-mismatch", id: "raw-hash", payload: `{}`, hash: make([]byte, sha256.Size)},
		{key: "noncanonical", id: "raw-canonical", payload: `{"b":1,"a":2}`, hash: sha256Bytes(`{"b":1,"a":2}`)},
		{key: "sensitive", id: "raw-sensitive", payload: `{"password":"raw"}`, hash: sha256Bytes(`{"password":"raw"}`)},
		{key: "bad-identifier", id: "bad id", payload: `{}`, hash: sha256Bytes(`{}`)},
	} {
		t.Run(row.key, func(t *testing.T) {
			if _, err := database.Exec(`INSERT INTO iotd_config_revisions (id, organization_id, kind, config_key, revision, parent_revision, payload, payload_hash, created_by_type, created_by_id, created_at) VALUES (?, 'org-main', 'identity_provider', ?, 1, 0, ?, ?, 'human', 'user-main', '2026-09-04T00:00:00.000000000Z')`, row.id, row.key, row.payload, row.hash); err != nil {
				t.Fatalf("insert corrupt row: %v", err)
			}
			if _, err := store.ByRevision(t.Context(), "org-main", KindIdentityProvider, row.key, 1); err == nil {
				t.Fatal("ByRevision returned corrupt row")
			}
			if _, err := store.Latest(t.Context(), "org-main", KindIdentityProvider, row.key); err == nil {
				t.Fatal("Latest returned corrupt row")
			}
			if _, err := store.History(t.Context(), "org-main", KindIdentityProvider, row.key, 1); err == nil {
				t.Fatal("History returned corrupt row")
			}
		})
	}
	if _, err := database.Exec(`INSERT INTO iotd_config_revisions (id, organization_id, kind, config_key, revision, parent_revision, payload, payload_hash, created_by_type, created_by_id, created_at) VALUES ('gap', 'org-main', 'membership', 'gap', 3, 2, '{}', ?, 'human', 'user-main', '2026-09-04T00:00:00.000000000Z')`, sha256Bytes(`{}`)); err != nil {
		t.Fatalf("insert chain gap: %v", err)
	}
	if _, err := store.History(t.Context(), "org-main", KindMembership, "gap", MaxHistoryLimit); err == nil {
		t.Fatal("History returned a chain with no first revision")
	}
}

func TestHistoryBoundsKindsAndExpectedParentOverflow(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	store := newStore(t, database)
	for _, kind := range []Kind{KindIdentityProvider, KindMembership, KindRoleBinding, KindDomainDictionary} {
		input := appendInput("kind-"+string(kind), `{}`, 0)
		input.Kind, input.ConfigKey = kind, "key-"+string(kind)
		if _, err := store.Append(t.Context(), input); err != nil {
			t.Fatalf("append %s: %v", kind, err)
		}
		if _, err := store.Latest(t.Context(), "org-main", kind, input.ConfigKey); err != nil {
			t.Fatalf("read %s: %v", kind, err)
		}
	}
	for parent, id := range []string{"history-2", "history-3"} {
		input := appendInput(id, `{}`, int64(parent+1))
		input.ConfigKey = "key-identity_provider"
		if _, err := store.Append(t.Context(), input); err != nil {
			t.Fatalf("append history revision %d: %v", parent+2, err)
		}
	}
	for _, limit := range []int{0, MaxHistoryLimit} {
		history, err := store.History(t.Context(), "org-main", KindIdentityProvider, "key-identity_provider", limit)
		if err != nil || len(history) != 3 || history[0].Revision != 1 || history[2].Revision != 3 {
			t.Fatalf("history limit %d = %#v error=%v, want complete prefix", limit, history, err)
		}
	}
	truncated, err := store.History(t.Context(), "org-main", KindIdentityProvider, "key-identity_provider", 2)
	if err != nil || len(truncated) != 2 || truncated[0].Revision != 1 || truncated[1].Revision != 2 {
		t.Fatalf("truncated history = %#v error=%v, want earliest contiguous prefix", truncated, err)
	}
	if _, err := store.History(t.Context(), "org-main", KindIdentityProvider, "key-identity_provider", -1); err == nil {
		t.Fatal("negative history limit unexpectedly used the default")
	}
	overflow := appendInput("overflow", `{}`, math.MaxInt)
	if _, err := store.Append(t.Context(), overflow); err == nil || errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("overflow append error = %v, want explicit validation failure", err)
	}
}

func sha256Bytes(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}
