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
