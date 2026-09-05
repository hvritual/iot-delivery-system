package locallogin

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	_ "modernc.org/sqlite"
)

func TestYU21SessionMigrationIsRepeatableAndKeepsNoPlainSessionColumn(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var ledger int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("migration ledger=%d error=%v, want 1", ledger, err)
	}
	rows, err := database.Query(`PRAGMA table_info('iotd_local_sessions')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		switch name {
		case "session_token", "session_secret", "token", "secret":
			t.Fatalf("plaintext session field %q exists", name)
		}
	}
	var sessions int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_local_sessions`).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("initial session rows=%d error=%v, want 0", sessions, err)
	}
}

func TestYU21MigrationRejectsForgedSessionSchemaWithoutCompositeUserFK(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_local_sessions (
 id TEXT PRIMARY KEY NOT NULL,
 organization_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 secret_digest BLOB NOT NULL UNIQUE CHECK (length(secret_digest) = 32),
 status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
 credential_revision INTEGER NOT NULL CHECK (credential_revision >= 1),
 created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
 expires_at TEXT NOT NULL CHECK (length(trim(expires_at)) > 0),
 revoked_at TEXT,
 CHECK (expires_at > created_at),
 CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))
)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged local session schema without composite User FK passed migration verification")
	}
}

func TestYU21MigrationRejectsForgedSessionSchemaWithoutDigestConstraint(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_local_sessions (
 id TEXT PRIMARY KEY NOT NULL,
 organization_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 secret_digest BLOB NOT NULL UNIQUE,
 status TEXT NOT NULL CHECK (status IN ('active', 'revoked')),
 credential_revision INTEGER NOT NULL CHECK (credential_revision >= 1),
 created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
 expires_at TEXT NOT NULL CHECK (length(trim(expires_at)) > 0),
 revoked_at TEXT,
 CHECK (expires_at > created_at),
 CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL)),
 FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT
)`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged local session schema without digest-length constraint passed migration verification")
	}
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "yu21-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}
