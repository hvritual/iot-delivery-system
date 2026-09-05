package locallogin

import (
	"bytes"
	"strings"
	"testing"
)

func TestYU22PersistedSessionIdentityAndCredentialRevisionAreImmutable(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"secret digest": `UPDATE iotd_local_sessions SET secret_digest = ? WHERE id = ?`,
		"credential revision": `UPDATE iotd_local_sessions SET credential_revision = credential_revision + 1 WHERE id = ?`,
	} {
		t.Run(name, func(t *testing.T) {
			var err error
			if name == "secret digest" {
				_, err = fixture.database.Exec(statement, bytes.Repeat([]byte{0x7d}, 32), login.SessionID)
			} else {
				_, err = fixture.database.Exec(statement, login.SessionID)
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(sessionIdentityMutationAbort)) {
				t.Fatalf("session identity mutation %s error=%v", name, err)
			}
		})
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil || session.Revision != 1 || session.CredentialRevision != 1 {
		t.Fatalf("session changed after mutation attempts: %#v error=%v", session, err)
	}
}
