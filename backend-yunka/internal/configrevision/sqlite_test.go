package configrevision

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	_ "modernc.org/sqlite"
	"yunka.io/framework/operation"
	"yunka.io/pkg/operationplan"
)

func TestSQLiteStoreAppendsCanonicalImmutableRevisionChainAndReadsAfterReopen(t *testing.T) {
	path := t.TempDir() + "/config-revisions.db"
	database := migratedDatabase(t, path)
	store := newStore(t, database)
	first, err := store.Append(t.Context(), appendInput("revision-1", `{"z":1,"large":900719925474099312345,"a":{"b":true}}`, 0))
	if err != nil {
		t.Fatalf("append first revision: %v", err)
	}
	second, err := store.Append(t.Context(), appendInput("revision-2", `{"a":{"b":true},"large":900719925474099312345,"z":1}`, 1))
	if err != nil {
		t.Fatalf("append second revision: %v", err)
	}
	if first.Revision != 1 || first.ParentRevision != 0 || second.Revision != 2 || second.ParentRevision != 1 || first.Payload != second.Payload || first.PayloadHash != second.PayloadHash {
		t.Fatalf("revision chain = %#v, %#v; want canonical contiguous revisions", first, second)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-other', 'org-other', 'Other')`); err != nil {
		t.Fatalf("insert other organization: %v", err)
	}
	if _, err := store.ByRevision(t.Context(), "org-other", KindIdentityProvider, "provider/default", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-organization read error = %v, want ErrNotFound", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	database, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store = newStore(t, database)
	latest, err := store.Latest(t.Context(), "org-main", KindIdentityProvider, "provider/default")
	if err != nil || latest != second {
		t.Fatalf("latest after reopen = %#v error=%v, want %#v", latest, err, second)
	}
	history, err := store.History(t.Context(), "org-main", KindIdentityProvider, "provider/default", 0)
	if err != nil || len(history) != 2 || history[0] != first || history[1] != second {
		t.Fatalf("history after reopen = %#v error=%v", history, err)
	}
}

func TestSQLiteStoreRejectsInvalidInputsWithoutPersistenceOrCredentialLeaks(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	store := newStore(t, database)
	const sentinel = "S0-04-05-SECRET-SENTINEL"
	for _, mutate := range []func(*AppendInput){
		func(input *AppendInput) { input.ID = "" },
		func(input *AppendInput) { input.Kind = "unknown" },
		func(input *AppendInput) { input.CreatedByType = "unknown" },
		func(input *AppendInput) { input.CreatedByID = "" },
		func(input *AppendInput) { input.CreatedAt = time.Now().UTC() },
		func(input *AppendInput) { input.Payload = "[]" },
		func(input *AppendInput) { input.Payload = `{"password":"` + sentinel + `"}` },
		func(input *AppendInput) { input.Payload = `{"safe":"Bearer ` + sentinel + `"}` },
		func(input *AppendInput) { input.Payload = `{"a":1,"a":2}` },
		func(input *AppendInput) { input.Payload = `{"a":NaN}` },
		func(input *AppendInput) {
			input.Payload = strings.Repeat(`{"a":`, maxPayloadDepth+1) + `0` + strings.Repeat(`}`, maxPayloadDepth+1)
		},
		func(input *AppendInput) {
			input.Payload = `{"a":[` + strings.TrimSuffix(strings.Repeat(`0,`, maxPayloadNodes), ",") + `]}`
		},
	} {
		input := appendInput("invalid-revision", `{"ok":true}`, 0)
		mutate(&input)
		if _, err := store.Append(t.Context(), input); err == nil || strings.Contains(err.Error(), sentinel) {
			t.Fatalf("invalid input %#v error=%v, want redacted rejection", input, err)
		}
		assertNoRevisions(t, database)
	}
	missingOrganization := appendInput("missing-organization", `{}`, 0)
	missingOrganization.OrganizationID = "org-missing"
	if _, err := store.Append(t.Context(), missingOrganization); err == nil {
		t.Fatal("config revision for an unknown organization unexpectedly appended")
	}
	assertNoRevisions(t, database)
	if _, err := store.Append(t.Context(), appendInput("safe-revision", `{"value":"safe"}`, 0)); err != nil {
		t.Fatalf("append safe revision: %v", err)
	}
	secretAttempt := appendInput("secret-attempt", `{"nested":{"client-secret":"`+sentinel+`"}}`, 1)
	if _, err := store.Append(t.Context(), secretAttempt); err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("secret-bearing append error = %v, want redacted rejection", err)
	}
	assertSentinelAbsentFromRevisionStorage(t, database, sentinel)
}

func TestSQLiteStoreRejectsStaleParentDuplicateIDAndConcurrentAppendWithoutGaps(t *testing.T) {
	database := migratedDatabase(t, t.TempDir()+"/race.db")
	database.SetMaxOpenConns(4)
	store := newStore(t, database)
	if _, err := store.Append(t.Context(), appendInput("seed", `{"mode":"seed"}`, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(t.Context(), appendInput("next", `{"mode":"next"}`, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(t.Context(), appendInput("stale", `{"mode":"stale"}`, 1)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale parent error = %v, want ErrRevisionConflict", err)
	}
	if _, err := store.Append(t.Context(), appendInput("jump", `{"mode":"jump"}`, 5)); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("jumped parent error = %v, want ErrRevisionConflict", err)
	}
	if _, err := store.Append(t.Context(), appendInput("next", `{"mode":"duplicate"}`, 2)); err == nil {
		t.Fatal("duplicate global ID unexpectedly appended")
	}

	var group sync.WaitGroup
	errorsByAttempt := make(chan error, 2)
	for _, id := range []string{"race-a", "race-b"} {
		group.Go(func() {
			_, err := store.Append(context.Background(), appendInput(id, `{"mode":"race"}`, 2))
			errorsByAttempt <- err
		})
	}
	group.Wait()
	close(errorsByAttempt)
	successes := 0
	conflicts := 0
	for err := range errorsByAttempt {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent append error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent appends successes=%d conflicts=%d, want one each", successes, conflicts)
	}
	history, err := store.History(t.Context(), "org-main", KindIdentityProvider, "provider/default", MaxHistoryLimit)
	if err != nil || len(history) != 3 {
		t.Fatalf("history after race = %#v error=%v", history, err)
	}
	for index, revision := range history {
		if revision.Revision != int64(index+1) || revision.ParentRevision != int64(index) {
			t.Fatalf("revision chain has gap at %#v", revision)
		}
	}
}

func TestSQLiteStoreParticipatesInYunkaSQLiteTransactions(t *testing.T) {
	database := migratedDatabase(t, ":memory:")
	store := newStore(t, database)
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	plan := operationplan.Plan{OperationID: "config.revisions.append", Execution: operationplan.Execution{Transaction: "local", Idempotency: "none"}}
	if _, err := executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		_, appendErr := store.Append(ctx, appendInput("committed", `{"transaction":"commit"}`, 0))
		return nil, appendErr
	}); err != nil {
		t.Fatalf("append in committed transaction: %v", err)
	}
	if _, err := store.Latest(t.Context(), "org-main", KindIdentityProvider, "provider/default"); err != nil {
		t.Fatalf("read committed revision: %v", err)
	}
	if _, err := executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		_, appendErr := store.Append(ctx, appendInput("rolled-back", `{"transaction":"rollback"}`, 1))
		if appendErr != nil {
			return nil, appendErr
		}
		return nil, errors.New("force config revision rollback")
	}); err == nil {
		t.Fatal("forced rollback unexpectedly succeeded")
	}
	if _, err := store.ByRevision(t.Context(), "org-main", KindIdentityProvider, "provider/default", 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back revision read error = %v, want ErrNotFound", err)
	}
}

func migratedDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name) VALUES ('org-main', 'org-main', 'Main')`); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply config revision migrations: %v", err)
	}
	return database
}

func newStore(t *testing.T, database *sql.DB) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(database, WithClock(func() time.Time { return time.Date(2026, 9, 4, 0, 0, 0, 123456789, time.UTC) }))
	if err != nil {
		t.Fatalf("new config revision store: %v", err)
	}
	return store
}

func appendInput(id, payload string, parent int64) AppendInput {
	return AppendInput{ID: id, OrganizationID: "org-main", Kind: KindIdentityProvider, ConfigKey: "provider/default", ExpectedParentRevision: parent, Payload: payload, CreatedByType: CreatedByHuman, CreatedByID: "user-main"}
}

func assertNoRevisions(t *testing.T, database *sql.DB) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_config_revisions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("config revision count = %d error=%v, want zero", count, err)
	}
}

func assertSentinelAbsentFromRevisionStorage(t *testing.T, database *sql.DB, sentinel string) {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM pragma_table_info('iotd_config_revisions') WHERE type IN ('TEXT', 'BLOB')`)
	if err != nil {
		t.Fatalf("list config revision text/blob columns: %v", err)
	}
	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan config revision column name: %v", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate config revision text/blob columns: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close config revision column rows: %v", err)
	}
	for _, column := range columns {
		var leaked int
		query := `SELECT COUNT(*) FROM iotd_config_revisions WHERE instr(CAST("` + column + `" AS TEXT), ?) > 0`
		if err := database.QueryRow(query, sentinel).Scan(&leaked); err != nil || leaked != 0 {
			t.Fatalf("secret sentinel leaked in config revision column %q count=%d error=%v", column, leaked, err)
		}
	}
}
