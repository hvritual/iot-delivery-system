package locallogin

import (
	"errors"
	"testing"
)

func TestYU22JWTVersionTwoRequiresSessionRevisionAndRejectsLegacyV1(t *testing.T) {
	fixture := newLoginFixture(t, false)
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifyAccessTokenSignature(fixture.config, login.AccessToken, fixture.now)
	if err != nil || claims.Version != 2 || claims.SessionRevision != 1 {
		t.Fatalf("YU-22 JWT claims=%#v error=%v", claims, err)
	}
	legacy := signRawJWT(t, fixture.config, jwtClaims{
		Issuer: fixture.config.Issuer,
		Audience: fixture.config.Audience,
		Subject: "user-a",
		TenantID: "org-a",
		SessionID: login.SessionID,
		SessionRevision: 1,
		IssuedAt: fixture.now.Unix(),
		ExpiresAt: fixture.now.Add(fixture.config.AccessTTL).Unix(),
		Version: 1,
	})
	if _, err := fixture.manager.VerifyAccessToken(t.Context(), legacy); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("legacy v1 JWT error=%v", err)
	}
}
