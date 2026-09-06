package locallogin

import (
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
)

func TestYU32HThrottlingPreservesResetCASWithoutAuthenticatingStaleSession(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	admin, ctx := newYU22AdminManager(t, fixture)
	_, err = admin.ResetCredential(ctx, localmemberadmin.ResetCredentialInput{
		UserID: "user-a", ExpectedUserRevision: 1, ExpectedCredentialRevision: 1,
		Password: []byte("YU32H-admin-reset-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertInvalid := func() {
		t.Helper()
		if _, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("stale session authenticated: %v", err)
		}
		if _, err := fixture.manager.VerifyAccessToken(t.Context(), login.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) {
			t.Fatalf("stale JWT authenticated: %v", err)
		}
		if _, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("stale session issued JWT: %v", err)
		}
	}
	assertInvalid()
	_, err = fixture.manager.ChangePassword(fixture.context(t), ChangePasswordInput{
		SessionToken: login.SessionToken, ExpectedSessionRevision: 1,
		ExpectedUserRevision: 1, ExpectedCredentialRevision: 1,
		CurrentPassword: []byte("YU21-password-secret"), NewPassword: []byte("YU32H-must-not-replace-reset"),
	})
	if !errors.Is(err, ErrUserRevisionConflict) {
		t.Fatalf("reset CAS error = %v", err)
	}
	assertInvalid()
	verified, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU32H-admin-reset-password"))
	if err != nil || !verified.Match || verified.Revision != 2 {
		t.Fatalf("reset credential changed: %#v %v", verified, err)
	}
	var attempts int
	err = fixture.database.QueryRow(`SELECT attempts FROM iotd_local_password_attempts WHERE bucket = ?`, throttleKey("account", "org-a", "user-a")).Scan(&attempts)
	if err != nil || attempts != 2 {
		t.Fatalf("CAS denial lost the security reservation: %d %v", attempts, err)
	}
}
