package locallogin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

// sessionQueryer is satisfied by both *sql.DB and *sql.Tx. Central session
// validity is intentionally a database fact so every local credential surface
// reaches the same fail-closed decision without caching user or credential
// state in a token.
type sessionQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const validSessionSelect = `SELECT sessions.id, sessions.organization_id, sessions.user_id,
       sessions.revision, sessions.credential_revision, sessions.expires_at
FROM iotd_local_sessions sessions
JOIN organizations
  ON organizations.id = sessions.organization_id
 AND organizations.status = 'active'
JOIN users
  ON users.organization_id = sessions.organization_id
 AND users.id = sessions.user_id
 AND users.status = 'active'
JOIN iotd_local_user_credentials credentials
  ON credentials.organization_id = sessions.organization_id
 AND credentials.user_id = sessions.user_id
 AND credentials.revision = sessions.credential_revision
`

// readValidSessionByDigest is the opaque-session validity boundary. A session
// is valid only while its server row is active/unexpired, its organization and
// user remain active, and the credential revision captured at login still
// matches the current durable credential revision.
func readValidSessionByDigest(ctx context.Context, queryer sessionQueryer, digest [sha256.Size]byte, now time.Time) (SessionIdentity, error) {
	if queryer == nil || now.IsZero() {
		return SessionIdentity{}, ErrSessionInvalid
	}
	return scanValidSession(queryer.QueryRowContext(ctx, validSessionSelect+`
WHERE sessions.secret_digest = ?
  AND sessions.status = 'active'
  AND sessions.expires_at > ?`, digest[:], formatTime(now)), ErrSessionInvalid)
}

// readValidSessionByClaims is the access-token validity boundary. The signed
// session identity/revision must still name the exact current valid server
// session. The credential-revision join additionally makes administrator
// password reset invalidate already-issued access JWTs on their next request.
func readValidSessionByClaims(ctx context.Context, queryer sessionQueryer, claims jwtClaims, now time.Time) (SessionIdentity, error) {
	if queryer == nil || now.IsZero() {
		return SessionIdentity{}, ErrAccessTokenInvalid
	}
	return scanValidSession(queryer.QueryRowContext(ctx, validSessionSelect+`
WHERE sessions.id = ?
  AND sessions.organization_id = ?
  AND sessions.user_id = ?
  AND sessions.revision = ?
  AND sessions.status = 'active'
  AND sessions.expires_at > ?`,
		claims.SessionID, claims.TenantID, claims.Subject, claims.SessionRevision, formatTime(now)), ErrAccessTokenInvalid)
}

func scanValidSession(row *sql.Row, invalid error) (SessionIdentity, error) {
	if row == nil {
		return SessionIdentity{}, invalid
	}
	var session SessionIdentity
	var expiresAt string
	err := row.Scan(
		&session.SessionID, &session.OrganizationID, &session.UserID,
		&session.Revision, &session.CredentialRevision, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionIdentity{}, invalid
	}
	if err != nil {
		return SessionIdentity{}, errors.New("read centralized local session validity")
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil || session.Revision < 1 || session.CredentialRevision < 1 ||
		!canonicalIdentifier(session.SessionID) || !canonicalIdentifier(session.OrganizationID) || !canonicalIdentifier(session.UserID) {
		return SessionIdentity{}, invalid
	}
	return session, nil
}
