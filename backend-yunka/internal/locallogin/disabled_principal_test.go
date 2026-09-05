package locallogin

import (
	"errors"
	"testing"
)

func TestYU21DisabledUserCannotCreatePrincipalFromPreviouslyValidJWT(t *testing.T) {
	fixture := newLoginFixture(t, false)
	result, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), result.AccessToken); err != nil {
		t.Fatalf("fresh token did not verify before disable: %v", err)
	}
	if _, err := fixture.database.Exec(`UPDATE users SET status = 'disabled', revision = revision + 1 WHERE organization_id = 'org-a' AND id = 'user-a'`); err != nil {
		t.Fatal(err)
	}
	principal, err := fixture.manager.VerifyAccessToken(t.Context(), result.AccessToken)
	if !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("disabled user token principal=%#v error=%v", principal, err)
	}
}
