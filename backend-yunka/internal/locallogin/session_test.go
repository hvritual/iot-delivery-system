package locallogin

import (
	"errors"
	"testing"
)

func TestYU21OpaqueSessionVerifiesAndCanIssueFreshAccessToken(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken)
	if err != nil {
		t.Fatalf("verify opaque session: %v", err)
	}
	if session.SessionID != login.SessionID || session.OrganizationID != "org-a" || session.UserID != "user-a" || session.CredentialRevision != 1 || !session.ExpiresAt.Equal(login.SessionExpiresAt) {
		t.Fatalf("verified session=%#v login=%#v", session, login)
	}
	fresh, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), login.SessionToken)
	if err != nil {
		t.Fatalf("issue access token from session: %v", err)
	}
	principal, err := fixture.manager.VerifyAccessToken(t.Context(), fresh.AccessToken)
	if err != nil || !principal.Authenticated || principal.UserID != "user-a" || principal.TenantID != "org-a" {
		t.Fatalf("fresh access token principal=%#v error=%v", principal, err)
	}
	if _, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken+"x"); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("malformed opaque session token error=%v", err)
	}
	if _, err := fixture.database.Exec(`DELETE FROM iotd_local_sessions WHERE id = ?`, login.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("deleted server session token error=%v", err)
	}
	if _, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("deleted server session issued JWT error=%v", err)
	}
}
