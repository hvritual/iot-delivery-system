package locallogin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

var ErrSessionInvalid = errors.New("local session is invalid")

type SessionIdentity struct {
	SessionID          string
	OrganizationID     string
	UserID             string
	CredentialRevision int64
	ExpiresAt          time.Time
}

type AccessTokenResult struct {
	AccessToken     string
	AccessExpiresAt time.Time
}

// VerifySessionToken resolves only the one-way digest of the opaque session
// bearer. The raw token is never sent to SQLite. Active Organization/User state
// is required before a session identity can be returned.
func (manager *Manager) VerifySessionToken(ctx context.Context, token string) (SessionIdentity, error) {
	if err := manager.ready(); err != nil {
		return SessionIdentity{}, err
	}
	if len(token) != 43 {
		return SessionIdentity{}, ErrSessionInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != sessionSecretBytes || base64.RawURLEncoding.EncodeToString(raw) != token {
		return SessionIdentity{}, ErrSessionInvalid
	}
	digest := sha256.Sum256(raw)
	zeroBytes(raw)
	now := manager.clock().UTC().Truncate(time.Second)
	if now.IsZero() {
		return SessionIdentity{}, ErrSessionInvalid
	}
	var session SessionIdentity
	var expiresAt string
	err = manager.database.QueryRowContext(ctx, `SELECT sessions.id, sessions.organization_id, sessions.user_id,
       sessions.credential_revision, sessions.expires_at
FROM iotd_local_sessions sessions
JOIN organizations ON organizations.id = sessions.organization_id AND organizations.status = 'active'
JOIN users ON users.organization_id = sessions.organization_id AND users.id = sessions.user_id AND users.status = 'active'
WHERE sessions.secret_digest = ? AND sessions.status = 'active' AND sessions.expires_at > ?`,
		digest[:], formatTime(now)).Scan(
		&session.SessionID, &session.OrganizationID, &session.UserID, &session.CredentialRevision, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionIdentity{}, ErrSessionInvalid
	}
	if err != nil {
		return SessionIdentity{}, errors.New("verify local opaque session")
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil || session.CredentialRevision < 1 || !canonicalIdentifier(session.SessionID) || !canonicalIdentifier(session.OrganizationID) || !canonicalIdentifier(session.UserID) {
		return SessionIdentity{}, ErrSessionInvalid
	}
	return session, nil
}

// IssueAccessTokenFromSession is the internal bridge from the longer-lived
// opaque server session to a short-lived signed JWT. It never trusts caller
// identity fields: tenant, User and session IDs come from the verified session.
func (manager *Manager) IssueAccessTokenFromSession(ctx context.Context, sessionToken string) (AccessTokenResult, error) {
	session, err := manager.VerifySessionToken(ctx, sessionToken)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return AccessTokenResult{}, ErrSessionInvalid
		}
		return AccessTokenResult{}, err
	}
	now := manager.clock().UTC().Truncate(time.Second)
	if session.ExpiresAt.Sub(now) < manager.config.AccessTTL {
		return AccessTokenResult{}, ErrSessionInvalid
	}
	token, expiresAt, err := signAccessToken(manager.config, session.OrganizationID, session.UserID, session.SessionID, now)
	if err != nil {
		return AccessTokenResult{}, err
	}
	return AccessTokenResult{AccessToken: token, AccessExpiresAt: expiresAt}, nil
}
