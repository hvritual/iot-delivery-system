package locallogin

import (
	"context"
	"crypto/sha256"
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
// bearer. The raw token is never sent to SQLite. Central validity requires the
// live session, active Organization/User, and the exact current credential
// revision captured by the session.
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
	session, err := readValidSessionByDigest(ctx, manager.database, digest, now)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return SessionIdentity{}, ErrSessionInvalid
		}
		return SessionIdentity{}, err
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
