package locallogin

import (
	"context"
	"errors"
	"time"

	"github.com/hvritual/yunka.io/framework/core/identity"
)

type verifiedAccessIdentity struct {
	Principal identity.Principal
	Session   SessionIdentity
}

func (manager *Manager) verifyAccessIdentity(ctx context.Context, token string) (verifiedAccessIdentity, error) {
	now := manager.clock().UTC().Truncate(time.Second)
	claims, err := verifyAccessTokenSignature(manager.config, token, now)
	if err != nil {
		return verifiedAccessIdentity{}, ErrAccessTokenInvalid
	}
	session, err := readValidSessionByClaims(ctx, manager.database, claims, now)
	if err != nil {
		if errors.Is(err, ErrAccessTokenInvalid) {
			return verifiedAccessIdentity{}, ErrAccessTokenInvalid
		}
		return verifiedAccessIdentity{}, errors.New("verify centralized local access token validity")
	}
	if session.Revision != claims.SessionRevision || session.ExpiresAt.Before(time.Unix(claims.ExpiresAt, 0).UTC()) {
		return verifiedAccessIdentity{}, ErrAccessTokenInvalid
	}
	return verifiedAccessIdentity{Principal: principalFromVerifiedClaims(claims), Session: session}, nil
}
