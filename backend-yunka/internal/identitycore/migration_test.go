package identitycore

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApplyMigrationsRollsBackOnConflictingIdentityTable(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE external_identities (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("precreate conflicting identity table: %v", err)
	}

	if err := ApplyMigrations(context.Background(), database); err == nil {
		t.Fatal("migration with a conflicting identity table unexpectedly succeeded")
	}
	for _, table := range []string{"iotd_schema_migrations", "organizations", "users"} {
		var name string
		if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != sql.ErrNoRows {
			t.Fatalf("failed migration must roll back %q, error=%v", table, err)
		}
	}
	var preserved string
	if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'external_identities'`).Scan(&preserved); err != nil || preserved != "external_identities" {
		t.Fatalf("preexisting conflicting table must remain, name=%q error=%v", preserved, err)
	}
}

func TestApplyMigrationsCreatesServiceCredentialSchemaWithoutPlaintextSecret(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	defer database.Close()

	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply identity migrations: %v", err)
	}

	var table string
	if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'service_account_credentials'`).Scan(&table); err != nil {
		t.Fatalf("service credential table is required: %v", err)
	}
	for _, column := range []string{"id", "service_account_id", "credential_hash", "expires_at", "revoked_at"} {
		var found string
		if err := database.QueryRow(`SELECT name FROM pragma_table_info('service_account_credentials') WHERE name = ?`, column).Scan(&found); err != nil {
			t.Fatalf("service credential column %q is required: %v", column, err)
		}
	}
	var plaintextColumns int
	if err := database.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('service_account_credentials') WHERE lower(name) IN ('secret', 'token', 'credential')`).Scan(&plaintextColumns); err != nil {
		t.Fatalf("inspect service credential schema: %v", err)
	}
	if plaintextColumns != 0 {
		t.Fatalf("service credential schema has %d plaintext credential columns", plaintextColumns)
	}
}

func TestApplyMigrationsUpgradesExistingS00201LedgerWithoutChangingIdentityData(t *testing.T) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open temporary SQLite database: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("create pre-S0-02-07 migration ledger: %v", err)
	}
	if _, err := database.Exec(identitySchema); err != nil {
		t.Fatalf("create S0-02-01 identity schema: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, '2026-09-03T00:00:00Z')`, MigrationID); err != nil {
		t.Fatalf("record S0-02-01 migration: %v", err)
	}
	if _, err := database.Exec(`
INSERT INTO organizations (id, slug, name) VALUES ('org-a', 'org-a', 'Organization A');
INSERT INTO users (id, organization_id, display_name) VALUES ('user-a', 'org-a', 'Alice');
INSERT INTO external_identities (id, organization_id, user_id, issuer, subject) VALUES ('identity-a', 'org-a', 'user-a', 'https://issuer.example.test', 'subject-a');
INSERT INTO service_accounts (id, organization_id, name) VALUES ('service-a', 'org-a', 'ci');`); err != nil {
		t.Fatalf("seed S0-02-01 identity data: %v", err)
	}

	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("upgrade S0-02-01 database: %v", err)
	}
	rows, err := database.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list upgraded identity tables: %v", err)
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("read upgraded identity table: %v", err)
		}
		tables[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate upgraded identity tables: %v", err)
	}
	for _, table := range []string{"iotd_schema_migrations", "organizations", "users", "external_identities", "service_accounts", "service_account_credentials"} {
		if !tables[table] {
			t.Fatalf("upgraded identity table set is missing %q: %v", table, tables)
		}
	}
	if len(tables) != 6 {
		t.Fatalf("upgrade added unexpected tables: %v", tables)
	}
	for table, want := range map[string]int{"organizations": 1, "users": 1, "external_identities": 1, "service_accounts": 1} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != want {
			t.Fatalf("upgraded table %s count = %d error=%v, want %d", table, count, err, want)
		}
	}
	for _, migrationID := range []string{MigrationID, ServiceCredentialMigrationID} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, migrationID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration ledger %q count = %d error=%v, want 1", migrationID, count, err)
		}
	}
	if err := ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("repeat upgraded migration: %v", err)
	}
	var ledgerCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations`).Scan(&ledgerCount); err != nil || ledgerCount != 2 {
		t.Fatalf("repeat migration ledger count = %d error=%v, want 2", ledgerCount, err)
	}
}
