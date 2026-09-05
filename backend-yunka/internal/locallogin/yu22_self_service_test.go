package locallogin

import (
	"errors"
	"strings"
	"testing"
)

func TestYU22CurrentMemberRequiresVerifiedJWTAndExactSessionRevision(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := fixture.manager.CurrentMember(fixture.context(t), CurrentMemberInput{AccessToken: login.AccessToken})
	if err != nil {
		t.Fatal(err)
	}
	if member.OrganizationID != "org-a" || member.UserID != "user-a" || member.UserRevision != 1 || member.CredentialRevision != 1 || member.SessionID != login.SessionID || member.SessionRevision != 1 {
		t.Fatalf("current member=%#v", member)
	}
	parts := strings.Split(login.AccessToken, ".")
	parts[2] = strings.Repeat("A", len(parts[2]))
	if _, err := fixture.manager.CurrentMember(fixture.context(t), CurrentMemberInput{AccessToken: strings.Join(parts, ".")}); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("tampered JWT current-member error=%v", err)
	}
	wrongRevision, _, err := signAccessTokenForSession(fixture.config, "org-a", "user-a", login.SessionID, 2, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.CurrentMember(fixture.context(t), CurrentMemberInput{AccessToken: wrongRevision}); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("wrong session-revision JWT current-member error=%v", err)
	}
	fromSession, err := fixture.manager.CurrentMemberFromSessionToken(t.Context(), login.SessionToken)
	if err != nil || fromSession.UserID != member.UserID || fromSession.SessionRevision != member.SessionRevision {
		t.Fatalf("opaque-session current member=%#v error=%v", fromSession, err)
	}
}

func TestYU22LogoutRevokesOpaqueSessionAndBoundJWTWithCAS(t *testing.T) {
	fixture := newLoginFixture(t, false)
	first, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Logout(fixture.context(t), LogoutInput{SessionToken: second.SessionToken, ExpectedSessionRevision: 2}); !errors.Is(err, ErrSessionRevisionConflict) {
		t.Fatalf("stale logout CAS error=%v", err)
	}
	logout, err := fixture.manager.Logout(fixture.context(t), LogoutInput{SessionToken: first.SessionToken, ExpectedSessionRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if logout.SessionID != first.SessionID || logout.SessionRevision != 2 {
		t.Fatalf("logout result=%#v", logout)
	}
	if _, err := fixture.manager.VerifySessionToken(t.Context(), first.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked opaque session error=%v", err)
	}
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), first.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("revoked-session JWT principal=%#v error=%v", principal, err)
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), second.SessionToken); err != nil || session.Revision != 1 {
		t.Fatalf("unrelated session=%#v error=%v", session, err)
	}
	page, err := fixture.auditStore.Query(t.Context(), auditQueryForOperation("org-a", OperationLogout))
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ReasonCode != "authentication.local_logout" {
		t.Fatalf("logout audit=%#v error=%v", page, err)
	}
	fixture.assertSecretsAbsent(t, []string{first.SessionToken, first.AccessToken, second.SessionToken})
}

func TestYU22PasswordChangeUsesUserCredentialCASAndRevokesAllOldSessions(t *testing.T) {
	fixture := newLoginFixture(t, false)
	first, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	const newPassword = "YU22-new-password-secret"
	result, err := fixture.manager.ChangePassword(fixture.context(t), ChangePasswordInput{
		SessionToken: first.SessionToken,
		ExpectedSessionRevision: 1,
		ExpectedUserRevision: 1,
		ExpectedCredentialRevision: 1,
		CurrentPassword: []byte("YU21-password-secret"),
		NewPassword: []byte(newPassword),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.UserRevision != 2 || result.CredentialRevision != 2 || result.RevokedSessions != 2 {
		t.Fatalf("password change result=%#v", result)
	}
	for _, session := range []LoginResult{first, second} {
		if _, err := fixture.manager.VerifySessionToken(t.Context(), session.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("old opaque session %s error=%v", session.SessionID, err)
		}
		if _, err := fixture.manager.VerifyAccessToken(t.Context(), session.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) {
			t.Fatalf("old JWT %s error=%v", session.SessionID, err)
		}
	}
	oldPassword, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU21-password-secret"))
	if err != nil || oldPassword.Match {
		t.Fatalf("old password verification=%#v error=%v", oldPassword, err)
	}
	newPasswordVerification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte(newPassword))
	if err != nil || !newPasswordVerification.Match || newPasswordVerification.Revision != 2 {
		t.Fatalf("new password verification=%#v error=%v", newPasswordVerification, err)
	}
	var userRevision int64
	if err := fixture.database.QueryRow(`SELECT revision FROM users WHERE organization_id = 'org-a' AND id = 'user-a'`).Scan(&userRevision); err != nil || userRevision != 2 {
		t.Fatalf("user revision=%d error=%v", userRevision, err)
	}
	page, err := fixture.auditStore.Query(t.Context(), auditQueryForOperation("org-a", OperationChangePassword))
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ReasonCode != "authentication.local_password_changed" {
		t.Fatalf("password change audit=%#v error=%v", page, err)
	}
	fixture.assertSecretsAbsent(t, []string{"YU21-password-secret", newPassword, first.SessionToken, second.SessionToken, first.AccessToken, second.AccessToken})
}

func TestYU22WrongCurrentPasswordAndStaleCASLeaveCredentialAndSessionUntouched(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	base := ChangePasswordInput{
		SessionToken: login.SessionToken,
		ExpectedSessionRevision: 1,
		ExpectedUserRevision: 1,
		ExpectedCredentialRevision: 1,
		CurrentPassword: []byte("wrong-current-password"),
		NewPassword: []byte("never-committed-password"),
	}
	if _, err := fixture.manager.ChangePassword(fixture.context(t), base); !errors.Is(err, ErrCurrentPasswordInvalid) {
		t.Fatalf("wrong current password error=%v", err)
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil || session.Revision != 1 {
		t.Fatalf("wrong-current-password session=%#v error=%v", session, err)
	}
	verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU21-password-secret"))
	if err != nil || !verification.Match || verification.Revision != 1 {
		t.Fatalf("wrong-current-password credential=%#v error=%v", verification, err)
	}
	base.CurrentPassword = []byte("YU21-password-secret")
	base.ExpectedUserRevision = 7
	if _, err := fixture.manager.ChangePassword(fixture.context(t), base); !errors.Is(err, ErrUserRevisionConflict) {
		t.Fatalf("stale user CAS error=%v", err)
	}
	base.ExpectedUserRevision = 1
	base.ExpectedCredentialRevision = 7
	if _, err := fixture.manager.ChangePassword(fixture.context(t), base); !errors.Is(err, localcredential.ErrRevisionConflict) {
		t.Fatalf("stale credential CAS error=%v", err)
	}
	base.ExpectedCredentialRevision = 1
	base.ExpectedSessionRevision = 7
	if _, err := fixture.manager.ChangePassword(fixture.context(t), base); !errors.Is(err, ErrSessionRevisionConflict) {
		t.Fatalf("stale session CAS error=%v", err)
	}
	if _, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil {
		t.Fatalf("stale CAS invalidated session: %v", err)
	}
}

func TestYU22AuditFailureRollsBackLogoutAndPasswordChange(t *testing.T) {
	t.Run("logout", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.Exec(`CREATE TRIGGER yu22_fail_logout_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.local-session.logout' AND NEW.result = 'success'
BEGIN SELECT RAISE(ABORT, 'forced YU22 logout audit failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Logout(fixture.context(t), LogoutInput{SessionToken: login.SessionToken, ExpectedSessionRevision: 1}); err == nil {
			t.Fatal("forced logout audit failure committed")
		}
		if session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil || session.Revision != 1 {
			t.Fatalf("logout audit failure session=%#v error=%v", session, err)
		}
		if _, err := fixture.manager.VerifyAccessToken(t.Context(), login.AccessToken); err != nil {
			t.Fatalf("logout audit failure invalidated JWT: %v", err)
		}
	})

	t.Run("password change", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.Exec(`CREATE TRIGGER yu22_fail_password_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.local-session.change-password' AND NEW.result = 'success'
BEGIN SELECT RAISE(ABORT, 'forced YU22 password audit failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.ChangePassword(fixture.context(t), ChangePasswordInput{
			SessionToken: login.SessionToken,
			ExpectedSessionRevision: 1,
			ExpectedUserRevision: 1,
			ExpectedCredentialRevision: 1,
			CurrentPassword: []byte("YU21-password-secret"),
			NewPassword: []byte("YU22-rollback-password"),
		}); err == nil {
			t.Fatal("forced password-change audit failure committed")
		}
		verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU21-password-secret"))
		if err != nil || !verification.Match || verification.Revision != 1 {
			t.Fatalf("password audit failure credential=%#v error=%v", verification, err)
		}
		if session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil || session.Revision != 1 {
			t.Fatalf("password audit failure session=%#v error=%v", session, err)
		}
		var userRevision int64
		if err := fixture.database.QueryRow(`SELECT revision FROM users WHERE id = 'user-a'`).Scan(&userRevision); err != nil || userRevision != 1 {
			t.Fatalf("password audit failure user revision=%d error=%v", userRevision, err)
		}
	})
}

func auditQueryForOperation(organizationID, operation string) audit.Query {
	return audit.Query{OrganizationID: organizationID, Operation: operation}
}
