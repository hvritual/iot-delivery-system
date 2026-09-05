package locallogin

import (
	"errors"
	"testing"
)

func TestYU21SessionCannotIssueAccessTokenPastItsRemainingLifetime(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.now = login.SessionExpiresAt.Add(-fixture.config.AccessTTL).Add(time.Second)
	if _, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), login.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("near-expiry session renewal error=%v, want ErrSessionInvalid", err)
	}
}
