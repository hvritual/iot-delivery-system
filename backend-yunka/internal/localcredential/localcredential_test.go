package localcredential

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/pkg/operationplan"
	_ "modernc.org/sqlite"
)

func TestYU18MigrationRequiresIdentitySchemaAndIsRepeatable(t *testing.T) {
	database := openDatabase(t)
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("local credential migration accepted a database without identity users")
	}
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply local credential migration: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("repeat local credential migration: %v", err)
	}
	var migrationCount, columnCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('iotd_local_user_credentials')`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 1 || columnCount != 13 {
		t.Fatalf("migration count=%d columns=%d, want 1/13", migrationCount, columnCount)
	}
}

func TestYU18MigrationDoesNotTrustForgedLedgerOverMalformedSchema(t *testing.T) {
	database := openDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_local_user_credentials (organization_id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id) VALUES (?)`, MigrationID); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged migration ledger hid a malformed local credential schema")
	}
}

func TestYU18RepositoryUsesOWASPArgon2idSaltedHashWithoutPlaintextPersistence(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganizationAndUser(t, database, "org-a", "user-a")
	seedOrganizationAndUser(t, database, "org-b", "user-b")
	clock := func() time.Time { return time.Date(2026, 9, 5, 6, 0, 0, 0, time.UTC) }
	random := bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)...))
	repository, err := NewSQLiteRepository(database, WithClock(clock), WithRandomSource(random))
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("YU18-plaintext-password-sentinel")
	first, err := repository.SetPassword(t.Context(), "org-a", "user-a", password, 0)
	if err != nil {
		t.Fatalf("set first password: %v", err)
	}
	second, err := repository.SetPassword(t.Context(), "org-b", "user-b", password, 0)
	if err != nil {
		t.Fatalf("set second password: %v", err)
	}
	for _, metadata := range []Metadata{first, second} {
		if metadata.Revision != 1 || metadata.PolicyVersion != DefaultPolicyVersion || metadata.Algorithm != AlgorithmArgon2id || metadata.ArgonVersion != 19 || metadata.MemoryKiB != 19456 || metadata.Iterations != 2 || metadata.Parallelism != 1 || metadata.SaltLength != 16 || metadata.HashLength != 32 {
			t.Fatalf("credential metadata = %#v", metadata)
		}
	}
	var firstSalt, firstHash, secondSalt, secondHash []byte
	if err := database.QueryRow(`SELECT salt, password_hash FROM iotd_local_user_credentials WHERE organization_id = 'org-a' AND user_id = 'user-a'`).Scan(&firstSalt, &firstHash); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT salt, password_hash FROM iotd_local_user_credentials WHERE organization_id = 'org-b' AND user_id = 'user-b'`).Scan(&secondSalt, &secondHash); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstSalt, secondSalt) || bytes.Equal(firstHash, secondHash) {
		t.Fatal("same password reused a salt or produced the same stored hash")
	}
	assertPlaintextAbsent(t, database, string(password))
	verification, err := repository.VerifyPassword(t.Context(), "org-a", "user-a", password)
	if err != nil || !verification.Match || verification.NeedsRehash || verification.Revision != 1 {
		t.Fatalf("correct password verification=%#v err=%v", verification, err)
	}
	wrong, err := repository.VerifyPassword(t.Context(), "org-a", "user-a", []byte("wrong-password"))
	if err != nil || wrong.Match || wrong.NeedsRehash {
		t.Fatalf("wrong password verification=%#v err=%v", wrong, err)
	}
}

func TestYU18RepositoryRequiresExistingTenantUserAndUsesCAS(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganizationAndUser(t, database, "org-a", "user-a")
	repository, err := NewSQLiteRepository(database, WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x33}, 64))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SetPassword(t.Context(), "org-a", "missing", []byte("safe-password"), 0); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user error=%v, want ErrUserNotFound", err)
	}
	if _, err := repository.SetPassword(t.Context(), "org-b", "user-a", []byte("safe-password"), 0); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("cross-tenant user error=%v, want ErrUserNotFound", err)
	}
	first, err := repository.SetPassword(t.Context(), "org-a", "user-a", []byte("first-password"), 0)
	if err != nil || first.Revision != 1 {
		t.Fatalf("first password=%#v err=%v", first, err)
	}
	if _, err := repository.SetPassword(t.Context(), "org-a", "user-a", []byte("stale-password"), 0); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale create error=%v, want ErrRevisionConflict", err)
	}
	if _, err := repository.SetPassword(t.Context(), "org-a", "user-a", []byte("stale-password"), 7); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update error=%v, want ErrRevisionConflict", err)
	}
	second, err := repository.SetPassword(t.Context(), "org-a", "user-a", []byte("second-password"), 1)
	if err != nil || second.Revision != 2 {
		t.Fatalf("second password=%#v err=%v", second, err)
	}
	oldPassword, err := repository.VerifyPassword(t.Context(), "org-a", "user-a", []byte("first-password"))
	if err != nil || oldPassword.Match {
		t.Fatalf("old password still matched=%#v err=%v", oldPassword, err)
	}
	newPassword, err := repository.VerifyPassword(t.Context(), "org-a", "user-a", []byte("second-password"))
	if err != nil || !newPassword.Match || newPassword.Revision != 2 {
		t.Fatalf("new password verification=%#v err=%v", newPassword, err)
	}
	assertCredentialCount(t, database, 1)
	assertPlaintextAbsent(t, database, "first-password")
	assertPlaintextAbsent(t, database, "second-password")
	assertPlaintextAbsent(t, database, "stale-password")
}

func TestYU18RepositoryJoinsExistingRootTransactionAndRollsBack(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganizationAndUser(t, database, "org-a", "user-a")
	repository, err := NewSQLiteRepository(database, WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x44}, 16))))
	if err != nil {
		t.Fatal(err)
	}
	executor := operation.NewExecutorWithOptions(nil, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	plan := operationplan.Plan{OperationID: "identity.local-credentials.set", Execution: operationplan.Execution{Transaction: "local", Idempotency: "none"}}
	forced := errors.New("force root rollback")
	_, err = executor.Execute(t.Context(), plan, nil, func(ctx context.Context) (any, error) {
		if _, setErr := repository.SetPassword(ctx, "org-a", "user-a", []byte("rollback-password"), 0); setErr != nil {
			return nil, setErr
		}
		return nil, forced
	})
	if !errors.Is(err, forced) {
		t.Fatalf("root rollback error=%v, want forced error", err)
	}
	if _, err := repository.Metadata(t.Context(), "org-a", "user-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back credential metadata error=%v, want ErrNotFound", err)
	}
	assertCredentialCount(t, database, 0)
	assertPlaintextAbsent(t, database, "rollback-password")
}

func TestYU18PolicyVersionMakesRehashUpgradeExplicit(t *testing.T) {
	database := migratedDatabase(t)
	seedOrganizationAndUser(t, database, "org-a", "user-a")
	v1 := DefaultPolicy()
	v1Set, err := NewPolicySet(1, v1)
	if err != nil {
		t.Fatal(err)
	}
	v1Repository, err := NewSQLiteRepository(database, WithPolicySet(v1Set), WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x55}, 16))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1Repository.SetPassword(t.Context(), "org-a", "user-a", []byte("upgrade-password"), 0); err != nil {
		t.Fatal(err)
	}
	v2 := v1
	v2.PolicyVersion = 2
	v2.Iterations = 3
	v2Set, err := NewPolicySet(2, v1, v2)
	if err != nil {
		t.Fatal(err)
	}
	v2Repository, err := NewSQLiteRepository(database, WithPolicySet(v2Set), WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x66}, 16))))
	if err != nil {
		t.Fatal(err)
	}
	before, err := v2Repository.VerifyPassword(t.Context(), "org-a", "user-a", []byte("upgrade-password"))
	if err != nil || !before.Match || !before.NeedsRehash || before.Revision != 1 {
		t.Fatalf("old-policy verification=%#v err=%v", before, err)
	}
	metadata, err := v2Repository.SetPassword(t.Context(), "org-a", "user-a", []byte("upgrade-password"), 1)
	if err != nil || metadata.PolicyVersion != 2 || metadata.Iterations != 3 || metadata.Revision != 2 {
		t.Fatalf("rehash metadata=%#v err=%v", metadata, err)
	}
	after, err := v2Repository.VerifyPassword(t.Context(), "org-a", "user-a", []byte("upgrade-password"))
	if err != nil || !after.Match || after.NeedsRehash || after.Revision != 2 {
		t.Fatalf("current-policy verification=%#v err=%v", after, err)
	}
}

func TestYU18PolicyRejectsBelowFloorAndPasswordErrorsDoNotEchoSecrets(t *testing.T) {
	weak := DefaultPolicy()
	weak.MemoryKiB = 4096
	weak.Iterations = 1
	if _, err := NewPolicySet(weak.PolicyVersion, weak); err == nil {
		t.Fatal("below-floor Argon2id policy was accepted")
	}
	database := migratedDatabase(t)
	seedOrganizationAndUser(t, database, "org-a", "user-a")
	repository, err := NewSQLiteRepository(database, WithRandomSource(errorReader{}))
	if err != nil {
		t.Fatal(err)
	}
	const sentinel = "YU18-password-must-never-echo"
	if _, err := repository.SetPassword(t.Context(), "org-a", "user-a", []byte(sentinel), 0); err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("hash failure error=%q", err)
	}
	assertPlaintextAbsent(t, database, sentinel)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }

func openDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "local-credentials.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database := openDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("apply local credential migrations: %v", err)
	}
	return database
}

func seedOrganizationAndUser(t *testing.T, database *sql.DB, organizationID, userID string) {
	t.Helper()
	if _, err := database.Exec(`INSERT OR IGNORE INTO organizations (id, slug, name) VALUES (?, ?, ?)`, organizationID, organizationID, organizationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name) VALUES (?, ?, ?)`, userID, organizationID, userID); err != nil {
		t.Fatal(err)
	}
}

func assertCredentialCount(t *testing.T, database *sql.DB, want int) {
	t.Helper()
	var got int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_local_user_credentials`).Scan(&got); err != nil || got != want {
		t.Fatalf("credential rows=%d err=%v, want %d", got, err, want)
	}
}

func assertPlaintextAbsent(t *testing.T, database *sql.DB, sentinel string) {
	t.Helper()
	var leaked int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_local_user_credentials
WHERE instr(CAST(salt AS TEXT), ?) > 0
   OR instr(CAST(password_hash AS TEXT), ?) > 0`, sentinel, sentinel).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatalf("plaintext sentinel %q leaked into credential storage", sentinel)
	}
}
