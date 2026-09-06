package locallogin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/execution"
)

var (
	ErrSessionRevisionConflict = errors.New("local session revision conflict")
	ErrUserRevisionConflict    = errors.New("local user revision conflict")
	ErrCurrentPasswordInvalid  = errors.New("current local password is invalid")
)

type CurrentMemberInput struct {
	AccessToken string
}

type CurrentMember struct {
	OrganizationID     string
	UserID             string
	DisplayName        string
	Email              string
	UserRevision       int64
	CredentialRevision int64
	SessionID          string
	SessionRevision    int64
	SessionExpiresAt   time.Time
}

type LogoutInput struct {
	SessionToken            string
	ExpectedSessionRevision int64
}

type LogoutResult struct {
	SessionID       string
	SessionRevision int64
	RevokedAt       time.Time
}

type ChangePasswordInput struct {
	SessionToken               string
	ExpectedSessionRevision    int64
	ExpectedUserRevision       int64
	ExpectedCredentialRevision int64
	CurrentPassword            []byte
	NewPassword                []byte
}

type ChangePasswordResult struct {
	OrganizationID     string
	UserID             string
	UserRevision       int64
	CredentialRevision int64
	RevokedSessions    int64
	ChangedAt          time.Time
}

// CurrentMember constructs self identity only after YU-21 JWT signature/claim
// verification and exact live session-revision verification. Caller-supplied
// claims are never accepted as member facts.
func (manager *Manager) CurrentMember(ctx context.Context, input CurrentMemberInput) (CurrentMember, error) {
	if err := manager.ready(); err != nil {
		return CurrentMember{}, err
	}
	value, err := manager.executor.Execute(ctx, currentMemberPlan, &input, func(callContext context.Context) (any, error) {
		verified, err := manager.verifyAccessIdentity(callContext, input.AccessToken)
		if err != nil {
			return nil, ErrAccessTokenInvalid
		}
		member, err := manager.currentMemberForSession(callContext, verified.Session)
		if err != nil {
			if errors.Is(err, ErrSessionInvalid) {
				return nil, ErrAccessTokenInvalid
			}
			return nil, err
		}
		return member, nil
	})
	if err != nil {
		return CurrentMember{}, err
	}
	member, ok := value.(CurrentMember)
	if !ok {
		return CurrentMember{}, errors.New("current local member returned an unexpected result")
	}
	return member, nil
}

// CurrentMemberFromSessionToken is the equivalent opaque-session boundary for
// future BFF composition. It still derives every identity field from a verified
// server-side session rather than from client claims.
func (manager *Manager) CurrentMemberFromSessionToken(ctx context.Context, sessionToken string) (CurrentMember, error) {
	session, err := manager.VerifySessionToken(ctx, sessionToken)
	if err != nil {
		return CurrentMember{}, err
	}
	return manager.currentMemberForSession(ctx, session)
}

func (manager *Manager) currentMemberForSession(ctx context.Context, session SessionIdentity) (CurrentMember, error) {
	var member CurrentMember
	err := manager.database.QueryRowContext(ctx, `SELECT users.organization_id, users.id, users.display_name,
       COALESCE(users.email, ''), users.revision
FROM users
JOIN organizations ON organizations.id = users.organization_id AND organizations.status = 'active'
WHERE users.organization_id = ? AND users.id = ? AND users.status = 'active'`,
		session.OrganizationID, session.UserID).Scan(
		&member.OrganizationID, &member.UserID, &member.DisplayName, &member.Email, &member.UserRevision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CurrentMember{}, ErrSessionInvalid
	}
	if err != nil {
		return CurrentMember{}, errors.New("read current local member")
	}
	if member.UserRevision < 1 {
		return CurrentMember{}, ErrSessionInvalid
	}
	member.CredentialRevision = session.CredentialRevision
	member.SessionID = session.SessionID
	member.SessionRevision = session.Revision
	member.SessionExpiresAt = session.ExpiresAt
	return member, nil
}

func (manager *Manager) Logout(ctx context.Context, input LogoutInput) (LogoutResult, error) {
	if err := manager.ready(); err != nil {
		return LogoutResult{}, err
	}
	if input.ExpectedSessionRevision < 1 {
		return LogoutResult{}, ErrSessionRevisionConflict
	}
	digest, err := sessionDigest(input.SessionToken)
	if err != nil {
		return LogoutResult{}, ErrSessionInvalid
	}
	value, err := manager.executor.Execute(ctx, logoutPlan, &input, func(callContext context.Context) (any, error) {
		return manager.logout(callContext, digest, input.ExpectedSessionRevision)
	})
	if err != nil {
		return LogoutResult{}, err
	}
	result, ok := value.(LogoutResult)
	if !ok {
		return LogoutResult{}, errors.New("local logout returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) logout(ctx context.Context, digest [sha256.Size]byte, expectedRevision int64) (LogoutResult, error) {
	transaction, err := sqliteTransaction(ctx)
	if err != nil {
		return LogoutResult{}, err
	}
	now, err := manager.sessionControlNow()
	if err != nil {
		return LogoutResult{}, err
	}
	session, err := manager.activeSessionInTransaction(ctx, transaction, digest, now)
	if err != nil {
		return LogoutResult{}, err
	}
	if session.Revision != expectedRevision {
		return LogoutResult{}, ErrSessionRevisionConflict
	}
	result, err := transaction.ExecContext(ctx, `UPDATE iotd_local_sessions
SET status = 'revoked', revision = revision + 1, revoked_at = ?
WHERE id = ? AND status = 'active' AND revision = ?`, formatTime(now), session.SessionID, expectedRevision)
	if err != nil {
		return LogoutResult{}, errors.New("revoke current local session")
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return LogoutResult{}, errors.New("read local logout CAS result")
	}
	if changed != 1 {
		return LogoutResult{}, ErrSessionRevisionConflict
	}
	logout := LogoutResult{SessionID: session.SessionID, SessionRevision: expectedRevision + 1, RevokedAt: now}
	if err := manager.recordLogoutAudit(ctx, transaction, session, logout); err != nil {
		return LogoutResult{}, err
	}
	return logout, nil
}

func (manager *Manager) ChangePassword(ctx context.Context, input ChangePasswordInput) (ChangePasswordResult, error) {
	if err := manager.ready(); err != nil {
		return ChangePasswordResult{}, err
	}
	if len(input.CurrentPassword) > localcredential.MaxPasswordBytes || len(input.NewPassword) > localcredential.MaxPasswordBytes {
		return ChangePasswordResult{}, localcredential.ErrInvalidPassword
	}
	if _, active := execution.Current(ctx); active {
		return ChangePasswordResult{}, ErrThrottleUnavailable
	}
	session, err := manager.VerifySessionToken(ctx, input.SessionToken)
	if err != nil {
		return ChangePasswordResult{}, err
	}
	if err := manager.reservePasswordAttempt(ctx, session.OrganizationID, session.UserID); err != nil {
		return ChangePasswordResult{}, err
	}
	currentPassword := append([]byte(nil), input.CurrentPassword...)
	newPassword := append([]byte(nil), input.NewPassword...)
	defer zeroBytes(currentPassword)
	defer zeroBytes(newPassword)
	input.CurrentPassword = currentPassword
	input.NewPassword = newPassword
	if input.ExpectedSessionRevision < 1 || input.ExpectedUserRevision < 1 || input.ExpectedCredentialRevision < 1 || len(currentPassword) == 0 || len(newPassword) == 0 {
		return ChangePasswordResult{}, ErrCurrentPasswordInvalid
	}
	digest, err := sessionDigest(input.SessionToken)
	if err != nil {
		return ChangePasswordResult{}, ErrSessionInvalid
	}
	value, err := manager.executor.Execute(ctx, changePasswordPlan, &input, func(callContext context.Context) (any, error) {
		return manager.changePassword(callContext, digest, input)
	})
	if err != nil {
		return ChangePasswordResult{}, err
	}
	result, ok := value.(ChangePasswordResult)
	if !ok {
		return ChangePasswordResult{}, errors.New("local password change returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) changePassword(ctx context.Context, digest [sha256.Size]byte, input ChangePasswordInput) (ChangePasswordResult, error) {
	transaction, err := sqliteTransaction(ctx)
	if err != nil {
		return ChangePasswordResult{}, err
	}
	now, err := manager.sessionControlNow()
	if err != nil {
		return ChangePasswordResult{}, err
	}
	session, err := manager.activeSessionInTransaction(ctx, transaction, digest, now)
	if err != nil {
		return ChangePasswordResult{}, err
	}
	if session.Revision != input.ExpectedSessionRevision {
		return ChangePasswordResult{}, ErrSessionRevisionConflict
	}
	if session.CredentialRevision != input.ExpectedCredentialRevision {
		return ChangePasswordResult{}, localcredential.ErrRevisionConflict
	}
	userResult, err := transaction.ExecContext(ctx, `UPDATE users
SET revision = revision + 1, updated_at = ?
WHERE organization_id = ? AND id = ? AND status = 'active' AND revision = ?`,
		formatTime(now), session.OrganizationID, session.UserID, input.ExpectedUserRevision)
	if err != nil {
		return ChangePasswordResult{}, errors.New("advance local user revision for password change")
	}
	changed, err := userResult.RowsAffected()
	if err != nil {
		return ChangePasswordResult{}, errors.New("read local user password-change CAS result")
	}
	if changed != 1 {
		return ChangePasswordResult{}, ErrUserRevisionConflict
	}
	verification, err := manager.credentials.VerifyPassword(ctx, session.OrganizationID, session.UserID, input.CurrentPassword)
	if err != nil || !verification.Match || verification.Revision != input.ExpectedCredentialRevision {
		return ChangePasswordResult{}, ErrCurrentPasswordInvalid
	}
	credential, err := manager.credentials.SetPassword(ctx, session.OrganizationID, session.UserID, input.NewPassword, input.ExpectedCredentialRevision)
	if err != nil {
		return ChangePasswordResult{}, err
	}
	revocation, err := transaction.ExecContext(ctx, `UPDATE iotd_local_sessions
SET status = 'revoked', revision = revision + 1, revoked_at = ?
WHERE organization_id = ? AND user_id = ? AND status = 'active'`,
		formatTime(now), session.OrganizationID, session.UserID)
	if err != nil {
		return ChangePasswordResult{}, errors.New("revoke local sessions after password change")
	}
	revokedSessions, err := revocation.RowsAffected()
	if err != nil {
		return ChangePasswordResult{}, errors.New("read password-change session revocations")
	}
	if revokedSessions < 1 {
		return ChangePasswordResult{}, ErrSessionRevisionConflict
	}
	result := ChangePasswordResult{
		OrganizationID:     session.OrganizationID,
		UserID:             session.UserID,
		UserRevision:       input.ExpectedUserRevision + 1,
		CredentialRevision: credential.Revision,
		RevokedSessions:    revokedSessions,
		ChangedAt:          now,
	}
	if err := manager.recordPasswordChangeAudit(ctx, transaction, session, result); err != nil {
		return ChangePasswordResult{}, err
	}
	return result, nil
}

func (manager *Manager) activeSessionInTransaction(ctx context.Context, transaction *sql.Tx, digest [sha256.Size]byte, now time.Time) (SessionIdentity, error) {
	var session SessionIdentity
	var expiresAt string
	err := transaction.QueryRowContext(ctx, `SELECT sessions.id, sessions.organization_id, sessions.user_id,
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
		return SessionIdentity{}, errors.New("read active local session")
	}
	session.ExpiresAt, err = parseTime(expiresAt)
	if err != nil || session.Revision < 1 || session.CredentialRevision < 1 {
		return SessionIdentity{}, ErrSessionInvalid
	}
	return session, nil
}

func (manager *Manager) sessionControlNow() (time.Time, error) {
	now := manager.clock().UTC().Truncate(time.Second)
	if now.IsZero() {
		return time.Time{}, errors.New("local session control clock returned zero time")
	}
	return now, nil
}

func (manager *Manager) recordLogoutAudit(ctx context.Context, transaction *sql.Tx, session SessionIdentity, result LogoutResult) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	diffSummary, err := audit.BuildDiffSummary("revoked", []string{"session.revision", "session.status"})
	if err != nil {
		return errors.New("build local logout audit diff")
	}
	metadata, err := json.Marshal(struct {
		SessionRevision int64 `json:"session_revision"`
	}{SessionRevision: result.SessionRevision})
	if err != nil {
		return errors.New("encode local logout audit metadata")
	}
	runtimeMetadata, _ := runtimecontext.MetadataFrom(ctx)
	_, err = manager.audit.AppendInTransaction(ctx, transaction, audit.Entry{
		ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryAuthentication,
		OrganizationID: session.OrganizationID, ActorType: audit.ActorHuman, ActorID: session.UserID,
		Operation: OperationLogout, AuthorizationDecision: audit.DecisionNotEvaluated,
		ScopeType: audit.ScopeOrganization, ScopeID: session.OrganizationID,
		TargetType: "identity.session", TargetID: session.SessionID,
		Result: audit.ResultSuccess, ReasonCode: "authentication.local_logout",
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: runtimeMetadata.RequestID,
		DiffSummary: diffSummary, Metadata: string(metadata), OccurredAt: result.RevokedAt,
	})
	if err != nil {
		return errors.New("record local logout audit")
	}
	return nil
}

func (manager *Manager) recordPasswordChangeAudit(ctx context.Context, transaction *sql.Tx, session SessionIdentity, result ChangePasswordResult) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	diffSummary, err := audit.BuildDiffSummary("changed", []string{"credential", "sessions", "user.revision"})
	if err != nil {
		return errors.New("build local password change audit diff")
	}
	metadata, err := json.Marshal(struct {
		UserRevision       int64 `json:"user_revision"`
		CredentialRevision int64 `json:"credential_revision"`
		RevokedSessions    int64 `json:"revoked_sessions"`
	}{
		UserRevision:       result.UserRevision,
		CredentialRevision: result.CredentialRevision,
		RevokedSessions:    result.RevokedSessions,
	})
	if err != nil {
		return errors.New("encode local password change audit metadata")
	}
	runtimeMetadata, _ := runtimecontext.MetadataFrom(ctx)
	_, err = manager.audit.AppendInTransaction(ctx, transaction, audit.Entry{
		ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryAuthentication,
		OrganizationID: session.OrganizationID, ActorType: audit.ActorHuman, ActorID: session.UserID,
		Operation: OperationChangePassword, AuthorizationDecision: audit.DecisionNotEvaluated,
		ScopeType: audit.ScopeOrganization, ScopeID: session.OrganizationID,
		TargetType: "identity.user", TargetID: session.UserID,
		Result: audit.ResultSuccess, ReasonCode: "authentication.local_password_changed",
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: runtimeMetadata.RequestID,
		DiffSummary: diffSummary, Metadata: string(metadata), OccurredAt: result.ChangedAt,
	})
	if err != nil {
		return errors.New("record local password change audit")
	}
	return nil
}
