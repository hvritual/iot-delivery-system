package locallogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
)

var (
	ErrAuthenticationFailed = errors.New("local authentication failed")
	ErrSessionPersistence   = errors.New("local session persistence failed")
)

const sessionSecretBytes = 32

type LoginInput struct {
	OrganizationID string
	UserID         string
	Password       []byte
}

type LoginResult struct {
	OrganizationID   string
	UserID           string
	SessionID        string
	SessionRevision  int64
	SessionToken     string
	SessionExpiresAt time.Time
	AccessToken      string
	AccessExpiresAt  time.Time
}

type Manager struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	audit       *audit.SQLiteStore
	executor    operation.Executor
	config      Config
	random      io.Reader
	newID       func() (string, error)
	clock       func() time.Time
	throttle    ThrottlePolicy
}

type Option func(*Manager) error

func WithRandomSource(source io.Reader) Option {
	return func(manager *Manager) error {
		if source == nil {
			return errors.New("local login random source is required")
		}
		manager.random = source
		return nil
	}
}

func WithIDGenerator(generator func() (string, error)) Option {
	return func(manager *Manager) error {
		if generator == nil {
			return errors.New("local login ID generator is required")
		}
		manager.newID = generator
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(manager *Manager) error {
		if clock == nil {
			return errors.New("local login clock is required")
		}
		manager.clock = clock
		return nil
	}
}

func NewManager(database *sql.DB, credentials *localcredential.SQLiteRepository, auditStore *audit.SQLiteStore, executor operation.Executor, config Config, options ...Option) (*Manager, error) {
	if database == nil || credentials == nil || auditStore == nil || executor == nil {
		return nil, errors.New("local login dependencies are required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	config.SigningKey = append([]byte(nil), config.SigningKey...)
	manager := &Manager{
		database: database, credentials: credentials, audit: auditStore, executor: executor,
		config: config, random: rand.Reader, newID: randomID, clock: time.Now,
		throttle: DefaultThrottlePolicy(),
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("local login option is required")
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (manager *Manager) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	if err := manager.ready(); err != nil {
		return LoginResult{}, err
	}
	if err := manager.reservePasswordAttempt(ctx, input.OrganizationID, input.UserID); err != nil {
		return LoginResult{}, err
	}
	if len(input.Password) > localcredential.MaxPasswordBytes {
		return LoginResult{}, ErrAuthenticationFailed
	}
	password := append([]byte(nil), input.Password...)
	defer zeroBytes(password)
	input.Password = password
	if !canonicalIdentifier(input.OrganizationID) || !canonicalIdentifier(input.UserID) || len(password) == 0 {
		_ = manager.recordFailure(ctx)
		return LoginResult{}, ErrAuthenticationFailed
	}
	value, err := manager.executor.Execute(ctx, loginPlan, &input, func(callContext context.Context) (any, error) {
		return manager.login(callContext, input)
	})
	if err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			_ = manager.recordFailure(ctx)
			return LoginResult{}, ErrAuthenticationFailed
		}
		return LoginResult{}, err
	}
	result, ok := value.(LoginResult)
	if !ok {
		return LoginResult{}, errors.New("local login returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) login(ctx context.Context, input LoginInput) (LoginResult, error) {
	transaction, err := sqliteTransaction(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if err := requireActiveUser(ctx, transaction, input.OrganizationID, input.UserID); err != nil {
		if errors.Is(err, ErrAuthenticationFailed) {
			manager.consumeSyntheticPasswordWork(input.Password)
			return LoginResult{}, ErrAuthenticationFailed
		}
		return LoginResult{}, err
	}
	verification, err := manager.credentials.VerifyPassword(ctx, input.OrganizationID, input.UserID, input.Password)
	if err != nil {
		switch {
		case errors.Is(err, localcredential.ErrNotFound),
			errors.Is(err, localcredential.ErrUserNotFound),
			errors.Is(err, localcredential.ErrInvalidPassword),
			errors.Is(err, localcredential.ErrUnsupportedCredential),
			errors.Is(err, localcredential.ErrCredentialCorrupt):
			manager.consumeSyntheticPasswordWork(input.Password)
			return LoginResult{}, ErrAuthenticationFailed
		default:
			return LoginResult{}, err
		}
	}
	if !verification.Match {
		return LoginResult{}, ErrAuthenticationFailed
	}
	credentialRevision := verification.Revision
	rehashed := false
	if verification.NeedsRehash {
		metadata, err := manager.credentials.RehashPassword(ctx, input.OrganizationID, input.UserID, input.Password, verification.Revision)
		if err != nil {
			if errors.Is(err, localcredential.ErrRevisionConflict) {
				return LoginResult{}, ErrAuthenticationFailed
			}
			return LoginResult{}, err
		}
		credentialRevision = metadata.Revision
		rehashed = true
	}
	now := manager.clock().UTC().Truncate(time.Second)
	if now.IsZero() {
		return LoginResult{}, errors.New("local login clock returned zero time")
	}
	sessionID, err := manager.nextID()
	if err != nil {
		return LoginResult{}, err
	}
	sessionToken, digest, err := manager.newSessionSecret()
	if err != nil {
		return LoginResult{}, err
	}
	sessionExpiresAt := now.Add(manager.config.SessionTTL)
	const sessionRevision int64 = 1
	if _, err := transaction.ExecContext(ctx, `INSERT INTO iotd_local_sessions (
id, organization_id, user_id, secret_digest, status, credential_revision, created_at, expires_at, revoked_at, revision
) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, NULL, ?)`,
		sessionID, input.OrganizationID, input.UserID, digest[:], credentialRevision, formatTime(now), formatTime(sessionExpiresAt), sessionRevision); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), sessionCredentialStaleAbort) {
			return LoginResult{}, ErrAuthenticationFailed
		}
		return LoginResult{}, ErrSessionPersistence
	}
	accessToken, accessExpiresAt, err := signAccessTokenForSession(manager.config, input.OrganizationID, input.UserID, sessionID, sessionRevision, now)
	if err != nil {
		return LoginResult{}, err
	}
	result := LoginResult{
		OrganizationID:   input.OrganizationID,
		UserID:           input.UserID,
		SessionID:        sessionID,
		SessionRevision:  sessionRevision,
		SessionToken:     sessionToken,
		SessionExpiresAt: sessionExpiresAt,
		AccessToken:      accessToken,
		AccessExpiresAt:  accessExpiresAt,
	}
	if err := manager.recordSuccess(ctx, transaction, result, credentialRevision, rehashed, now); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// VerifyAccessToken establishes a human Principal only after strict JWT
// signature/claim verification, a live server-side session lookup, an exact
// session revision match, and an active tenant/User check.
func (manager *Manager) VerifyAccessToken(ctx context.Context, token string) (identity.Principal, error) {
	if err := manager.ready(); err != nil {
		return identity.Principal{}, err
	}
	verified, err := manager.verifyAccessIdentity(ctx, token)
	if err != nil {
		if errors.Is(err, ErrAccessTokenInvalid) {
			return identity.Principal{}, ErrAccessTokenInvalid
		}
		return identity.Principal{}, err
	}
	return verified.Principal, nil
}

func requireActiveUser(ctx context.Context, transaction *sql.Tx, organizationID, userID string) error {
	var found int
	err := transaction.QueryRowContext(ctx, `SELECT 1
FROM organizations
JOIN users ON users.organization_id = organizations.id
WHERE organizations.id = ? AND organizations.status = 'active'
  AND users.id = ? AND users.status = 'active'`, organizationID, userID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAuthenticationFailed
	}
	if err != nil {
		return errors.New("read local login user")
	}
	if found != 1 {
		return ErrAuthenticationFailed
	}
	return nil
}

func (manager *Manager) consumeSyntheticPasswordWork(password []byte) {
	_ = manager.credentials.VerifyPasswordAgainstSyntheticCredential(password)
}

func (manager *Manager) recordSuccess(ctx context.Context, transaction *sql.Tx, result LoginResult, credentialRevision int64, rehashed bool, occurredAt time.Time) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	diffSummary, err := audit.BuildDiffSummary("created", []string{"session"})
	if err != nil {
		return errors.New("build local login success audit diff")
	}
	metadata, err := json.Marshal(struct {
		CredentialRevision int64  `json:"credential_revision"`
		CredentialRehashed bool   `json:"credential_rehashed"`
		SessionRevision    int64  `json:"session_revision"`
		JWTVersion         int    `json:"jwt_version"`
		KeyID              string `json:"key_id"`
		AccessTTLSeconds   int64  `json:"access_ttl_seconds"`
		SessionTTLSeconds  int64  `json:"session_ttl_seconds"`
	}{
		CredentialRevision: credentialRevision,
		CredentialRehashed: rehashed,
		SessionRevision:    result.SessionRevision,
		JWTVersion:         JWTVersion,
		KeyID:              manager.config.KeyID,
		AccessTTLSeconds:   int64(manager.config.AccessTTL / time.Second),
		SessionTTLSeconds:  int64(manager.config.SessionTTL / time.Second),
	})
	if err != nil {
		return errors.New("encode local login success audit metadata")
	}
	runtimeMetadata, _ := runtimecontext.MetadataFrom(ctx)
	_, err = manager.audit.AppendInTransaction(ctx, transaction, audit.Entry{
		ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryAuthentication,
		OrganizationID: result.OrganizationID, ActorType: audit.ActorHuman, ActorID: result.UserID,
		Operation: OperationLogin, AuthorizationDecision: audit.DecisionNotEvaluated,
		ScopeType: audit.ScopeOrganization, ScopeID: result.OrganizationID,
		TargetType: "identity.session", TargetID: result.SessionID,
		Result: audit.ResultSuccess, ReasonCode: "authentication.local_login_accepted",
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: runtimeMetadata.RequestID,
		DiffSummary: diffSummary, Metadata: string(metadata), OccurredAt: occurredAt,
	})
	if err != nil {
		return errors.New("record local login success audit")
	}
	return nil
}

func (manager *Manager) recordFailure(ctx context.Context) error {
	return manager.recordFailureReason(ctx, "authentication.local_login_failed")
}

func (manager *Manager) recordThrottle(ctx context.Context) error {
	return manager.recordFailureReason(ctx, "authentication.local_login_throttled")
}

func (manager *Manager) recordFailureReason(ctx context.Context, reason string) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	now := manager.clock().UTC().Truncate(time.Second)
	if now.IsZero() {
		return errors.New("local login clock returned zero time")
	}
	diffSummary, err := audit.BuildDiffSummary("rejected", nil)
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(struct {
		FailureClass string `json:"failure_class"`
	}{FailureClass: "credential"})
	runtimeMetadata, _ := runtimecontext.MetadataFrom(ctx)
	_, err = manager.audit.Append(ctx, audit.Entry{
		ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryAuthentication,
		ActorType: audit.ActorAnonymous, Operation: OperationLogin,
		AuthorizationDecision: audit.DecisionNotEvaluated, ScopeType: audit.ScopeSystem,
		Result: audit.ResultFailure, ReasonCode: reason,
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: runtimeMetadata.RequestID,
		DiffSummary: diffSummary, Metadata: string(metadata), OccurredAt: now,
	})
	return err
}

func (manager *Manager) newSessionSecret() (string, [sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	secret := make([]byte, sessionSecretBytes)
	if _, err := io.ReadFull(manager.random, secret); err != nil {
		return "", empty, errors.New("generate local session secret")
	}
	defer zeroBytes(secret)
	token := base64.RawURLEncoding.EncodeToString(secret)
	if token == "" || len(token) != 43 {
		return "", empty, errors.New("encode local session secret")
	}
	return token, sha256.Sum256(secret), nil
}

func (manager *Manager) nextID() (string, error) {
	id, err := manager.newID()
	if err != nil {
		return "", errors.New("generate local login ID")
	}
	if !canonicalIdentifier(id) {
		return "", errors.New("local login ID is invalid")
	}
	return id, nil
}

func (manager *Manager) ready() error {
	if manager == nil || manager.database == nil || manager.credentials == nil || manager.audit == nil || manager.executor == nil || manager.random == nil || manager.newID == nil || manager.clock == nil {
		return errors.New("local login manager is not configured")
	}
	return manager.config.validate()
}

func sqliteTransaction(ctx context.Context) (*sql.Tx, error) {
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, errors.New("get local login transaction handle")
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("local login requires a SQLite root transaction")
	}
	return transaction, nil
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read local login random identifier: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", value)
	if err != nil || parsed.Location() != time.UTC || formatTime(parsed) != value {
		return time.Time{}, errors.New("invalid local session time")
	}
	return parsed, nil
}
