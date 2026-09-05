package locallogin

import (
	"errors"
	"testing"
)

func TestYU22CurrentMemberRejectsRevokedOrDisabledSessionIdentity(t *testing.T) {
	t.Run("revoked session", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Logout(fixture.context(t), LogoutInput{SessionToken: login.SessionToken, ExpectedSessionRevision: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.CurrentMember(fixture.context(t), CurrentMemberInput{AccessToken: login.AccessToken}); !errors.Is(err, ErrAccessTokenInvalid) {
			t.Fatalf("revoked-session current member error=%v", err)
		}
	})

	t.Run("disabled user", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.Exec(`UPDATE users SET status = 'disabled', revision = revision + 1 WHERE organization_id = 'org-a' AND id = 'user-a' AND revision = 1`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.CurrentMember(fixture.context(t), CurrentMemberInput{AccessToken: login.AccessToken}); !errors.Is(err, ErrAccessTokenInvalid) {
			t.Fatalf("disabled-user current member error=%v", err)
		}
		if _, err := fixture.manager.CurrentMemberFromSessionToken(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("disabled-user opaque current member error=%v", err)
		}
	})
}
