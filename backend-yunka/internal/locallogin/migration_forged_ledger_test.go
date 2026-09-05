package locallogin

import (
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
)

func TestYU21MigrationDoesNotTrustForgedLedgerOverMalformedSessionSchema(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE iotd_local_sessions (id TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_schema_migrations (migration_id) VALUES (?)`, MigrationID); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged migration ledger hid malformed local session schema")
	}
}
