package locallogin

import (
	"errors"
	"testing"
)

func TestYU21AuthenticationFailureRemainsRejectedWhenFailureAuditCannotPersist(t *testing.T) {
	fixture := newLoginFixture(t, false)
	if _, err := fixture.database.Exec(`CREATE TRIGGER yu21_fail_authentication_failure_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.local-login.authenticate' AND NEW.result = 'failure'
BEGIN SELECT RAISE(ABORT, 'forced YU21 authentication failure audit outage'); END`); err != nil {
		t.Fatal(err)
	}
	beforeSessions := fixture.sessionCount(t)
	_, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-definitely-wrong-password"),
	})
	if !errors.Is(err, ErrAuthenticationFailed) || err.Error() != ErrAuthenticationFailed.Error() {
		t.Fatalf("authentication error changed because failure audit was unavailable: %v", err)
	}
	if fixture.sessionCount(t) != beforeSessions {
		t.Fatal("failed authentication with unavailable audit created a session")
	}
}
