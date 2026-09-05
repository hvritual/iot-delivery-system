package localbootstrap

import (
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
)

func TestYU19BootstrapMigrationIsRepeatableAndLeavesEmptyIdentityOpen(t *testing.T) {
	database := openDatabase(t, 1)
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
	var ledger, states, triggers int
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_local_admin_bootstrap_state`).Scan(&states); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ('iotd_local_admin_bootstrap_state_immutable_update', 'iotd_local_admin_bootstrap_state_immutable_delete')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 || states != 0 || triggers != 2 {
		t.Fatalf("repeat migration ledger=%d states=%d triggers=%d, want 1/0/2", ledger, states, triggers)
	}
}

func TestYU19BootstrapMigrationDoesNotTrustForgedLedgerOverMalformedStateSchema(t *testing.T) {
	database := openDatabase(t, 1)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_local_admin_bootstrap_state (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id) VALUES (?)`, MigrationID); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged migration ledger hid malformed bootstrap state schema")
	}
}
