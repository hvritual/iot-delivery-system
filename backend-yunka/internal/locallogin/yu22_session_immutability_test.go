package locallogin

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestYU22PersistedSessionIdentityCredentialAndLifetimeAreImmutable(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		statement string
		args      []any
	}{
		{name: "secret digest", statement: `UPDATE iotd_local_sessions SET secret_digest = ? WHERE id = ?`, args: []any{bytes.Repeat([]byte{0x7d}, 32), login.SessionID}},
		{name: "credential revision", statement: `UPDATE iotd_local_sessions SET credential_revision = credential_revision + 1 WHERE id = ?`, args: []any{login.SessionID}},
		{name: "expiry extension", statement: `UPDATE iotd_local_sessions SET expires_at = ? WHERE id = ?`, args: []any{formatTime(login.SessionExpiresAt.Add(time.Hour)), login.SessionID}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := fixture.database.Exec(test.statement, test.args...)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionIdentityMutationAbort)) {
				t.Fatalf("session immutable mutation %s error=%v", test.name, err)
			}
		})
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil || session.Revision != 1 || session.CredentialRevision != 1 || !session.ExpiresAt.Equal(login.SessionExpiresAt) {
		t.Fatalf("session changed after mutation attempts: %#v error=%v", session, err)
	}
}

func TestYU22RevokedSessionTimestampCannotBeRewritten(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	logout, err := fixture.manager.Logout(fixture.context(t), LogoutInput{SessionToken: login.SessionToken, ExpectedSessionRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.database.Exec(`UPDATE iotd_local_sessions SET revoked_at = ? WHERE id = ?`, formatTime(logout.RevokedAt.Add(time.Minute)), login.SessionID)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionRevokedAtMutationAbort)) {
		t.Fatalf("revoked-at rewrite error=%v", err)
	}
	var revokedAt string
	if err := fixture.database.QueryRow(`SELECT revoked_at FROM iotd_local_sessions WHERE id = ?`, login.SessionID).Scan(&revokedAt); err != nil || revokedAt != formatTime(logout.RevokedAt) {
		t.Fatalf("revoked-at changed to %q error=%v", revokedAt, err)
	}
}
