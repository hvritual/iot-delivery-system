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
	Revision           int64
	CredentialRevision int64
	ExpiresAt          time.Time
}

type AccessTokenResult struct {
	AccessToken     string
	AccessExpiresAt time.Time
}

// VerifySessionToken resolves only the one-way digest of the opaque session
// bearer. The raw token is never sent to SQLite. Active Organization/User state
// and a non-revoked persisted session revision are required.
func (manager *Manager) VerifySessionToken(ctx context.Context, token string) (SessionIdentity, error) {
	if err := manager.ready(); err != nil {
		return SessionIdentity{}, err
	}
	digest, err := sessionDigest(token)
	if err != nil {
		return SessionIdentity{}, ErrSessionInvalid
	}
	now := manager.clock().UTC().Truncate(time.Second)
	if now.IsZero() {
		return SessionIdentity{}, ErrSessionInvalid
	}
	var session SessionIdentity
	var expiresAt string
	err = manager.database.QueryRowContext(ctx, `SELECT sessions.id, sessions.organization_id, sessions.user_id,
       sessions.revision, sessions.credential_revision, sessions.expires_at
FROM iotd_local_sessions sessions
JOIN organizations ON organizations.id = sessions.organization_id AND organizations.status = 'active'
JOIN users ON users.organization_id = sessions.organization_id AND users.id = sessions.user_id AND users.status = 'active'
WHERE sessions.secret_digest = ? AND sessions.status = 'active' AND sessions.expires_at > ?`,
		digest[:], formatTime(now)).Scan(
		&session.SessionID, &session.OrganizationID, &session.UserID,
		&session.Revision, &session.CredentialRevision, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionIdentity{}, ErrSessionInvalid
	}
	if err != nil {
		return SessionIdentity{}, errors.New("verify local opaque session")
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil || session.Revision < 1 || session.CredentialRevision < 1 || !canonicalIdentifier(session.SessionID) || !canonicalIdentifier(session.OrganizationID) || !canonicalIdentifier(session.UserID) {
		return SessionIdentity{}, ErrSessionInvalid
	}
	return session, nil
}

// IssueAccessTokenFromSession is the internal bridge from the longer-lived
// opaque server session to a short-lived signed JWT. It never trusts caller
// identity fields: tenant, User, session ID and session revision come from the
// verified server-side session.
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
	token, expiresAt, err := signAccessTokenForSession(manager.config, session.OrganizationID, session.UserID, session.SessionID, session.Revision, now)
	if err != nil {
		return AccessTokenResult{}, err
	}
	return AccessTokenResult{AccessToken: token, AccessExpiresAt: expiresAt}, nil
}

func sessionDigest(token string) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if len(token) != 43 {
		return empty, ErrSessionInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != sessionSecretBytes || base64.RawURLEncoding.EncodeToString(raw) != token {
		zeroBytes(raw)
		return empty, ErrSessionInvalid
	}
	digest := sha256.Sum256(raw)
	zeroBytes(raw)
	return digest, nil
}
