package locallogin

import (
	"context"
	"database/sql"
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
	var session SessionIdentity
	var expiresAt string
	err = manager.database.QueryRowContext(ctx, `SELECT sessions.id, sessions.organization_id, sessions.user_id,
       sessions.revision, sessions.credential_revision, sessions.expires_at
FROM iotd_local_sessions sessions
JOIN organizations ON organizations.id = sessions.organization_id AND organizations.status = 'active'
JOIN users ON users.organization_id = sessions.organization_id AND users.id = sessions.user_id AND users.status = 'active'
WHERE sessions.id = ? AND sessions.organization_id = ? AND sessions.user_id = ?
  AND sessions.revision = ? AND sessions.status = 'active' AND sessions.expires_at > ?`,
		claims.SessionID, claims.TenantID, claims.Subject, claims.SessionRevision, formatTime(now)).Scan(
		&session.SessionID, &session.OrganizationID, &session.UserID,
		&session.Revision, &session.CredentialRevision, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return verifiedAccessIdentity{}, ErrAccessTokenInvalid
	}
	if err != nil {
		return verifiedAccessIdentity{}, errors.New("verify local access token session")
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil || session.Revision != claims.SessionRevision || session.CredentialRevision < 1 || session.ExpiresAt.Before(time.Unix(claims.ExpiresAt, 0).UTC()) {
		return verifiedAccessIdentity{}, ErrAccessTokenInvalid
	}
	return verifiedAccessIdentity{Principal: principalFromVerifiedClaims(claims), Session: session}, nil
}
