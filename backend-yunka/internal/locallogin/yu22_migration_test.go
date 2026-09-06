package locallogin

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
)

func TestYU22MigrationUpgradesYU21SessionRowsToRevisionOne(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO organizations (id, slug, name, status) VALUES ('org-upgrade', 'org-upgrade', 'Upgrade', 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO users (id, organization_id, display_name, status) VALUES ('user-upgrade', 'org-upgrade', 'Upgrade User', 'active')`); err != nil {
		t.Fatal(err)
	}
	credentials, err := localcredential.NewSQLiteRepository(database, localcredential.WithRandomSource(bytes.NewReader(bytes.Repeat([]byte{0x41}, 64))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.SetPassword(t.Context(), "org-upgrade", "user-upgrade", []byte("upgrade-password"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(sessionSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO iotd_local_sessions (
id, organization_id, user_id, secret_digest, status, credential_revision, created_at, expires_at, revoked_at
) VALUES ('session-upgrade', 'org-upgrade', 'user-upgrade', ?, 'active', 1, ?, ?, NULL)`,
		bytes.Repeat([]byte{0x52}, 32),
		"2026-09-05T07:00:00.000000000Z",
		"2026-09-05T19:00:00.000000000Z",
	); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := database.QueryRow(`SELECT revision FROM iotd_local_sessions WHERE id = 'session-upgrade'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("upgraded session revision=%d error=%v", revision, err)
	}
	for _, migrationID := range []string{MigrationID, SessionControlMigrationID} {
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, migrationID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration %s count=%d error=%v", migrationID, count, err)
		}
	}
}

func TestYU22SessionRevocationRequiresCASAndCannotBeReactivated(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(fixture.now.Add(time.Minute))
	if _, err := fixture.database.Exec(`UPDATE iotd_local_sessions SET status = 'revoked', revoked_at = ? WHERE id = ?`, now, login.SessionID); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionRevocationCASAbort)) {
		t.Fatalf("revocation without session CAS error=%v", err)
	}
	if _, err := fixture.database.Exec(`UPDATE iotd_local_sessions SET status = 'revoked', revision = revision + 1, revoked_at = ? WHERE id = ?`, now, login.SessionID); err != nil {
		t.Fatalf("CAS-correct direct revocation failed: %v", err)
	}
	if _, err := fixture.database.Exec(`UPDATE iotd_local_sessions SET status = 'active', revoked_at = NULL WHERE id = ?`, login.SessionID); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionResurrectionAbort)) {
		t.Fatalf("revoked session reactivation error=%v", err)
	}
}

func TestYU22SessionInsertRejectsStaleCredentialRevision(t *testing.T) {
	fixture := newLoginFixture(t, false)
	if _, err := fixture.credentials.SetPassword(t.Context(), "org-a", "user-a", []byte("credential-v2-fixture"), 1); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.database.Exec(`INSERT INTO iotd_local_sessions (
id, organization_id, user_id, secret_digest, status, credential_revision, created_at, expires_at, revoked_at, revision
) VALUES ('stale-session', 'org-a', 'user-a', ?, 'active', 1, ?, ?, NULL, 1)`,
		bytes.Repeat([]byte{0x73}, 32),
		formatTime(fixture.now),
		formatTime(fixture.now.Add(time.Hour)),
	)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionCredentialStaleAbort)) {
		t.Fatalf("stale credential session insert error=%v", err)
	}
}

func TestYU22MigrationRejectsForgedNoOpSessionControlTrigger(t *testing.T) {
	database := openMigrationDatabase(t)
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := localcredential.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(sessionSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`ALTER TABLE iotd_local_sessions ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER iotd_local_sessions_require_revocation_revision BEFORE UPDATE ON iotd_local_sessions BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err == nil {
		t.Fatal("forged no-op session-control trigger passed migration verification")
	}
}
