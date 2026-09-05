package localmemberadmin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localcredential"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
)

var (
	ErrInvalidInput           = errors.New("local member admin input is invalid")
	ErrMemberNotFound         = errors.New("local member was not found")
	ErrMemberRevisionConflict = errors.New("local member revision conflict")
	ErrMemberDisabled         = errors.New("local member is disabled")
	ErrLastAdministrator      = errors.New("last active system administrator cannot be disabled")
)

const (
	memberEventTopic           = "identity.members"
	memberCreatedEvent         = "identity.member.created"
	memberDisabledEvent        = "identity.member.disabled"
	memberCredentialResetEvent = "identity.member.credential-reset"
	correlationIDAttribute     = "correlation_id"
	lastAdministratorAbort     = "cannot disable last system administrator"
)

type CreateInput struct {
	DisplayName string
	Email       string
	Password    []byte
}

type DisableInput struct {
	UserID           string
	ExpectedRevision int64
}

type ResetCredentialInput struct {
	UserID                     string
	ExpectedUserRevision       int64
	ExpectedCredentialRevision int64
	Password                   []byte
}

type MemberResult struct {
	OrganizationID     string
	UserID             string
	UserRevision       int64
	CredentialRevision int64
}

type Manager struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	audit       audit.Store
	outbox      outbox.TransactionalStore
	executor    operation.Executor
	newID       func() (string, error)
	clock       func() time.Time
}

type Option func(*Manager) error

func WithIDGenerator(generator func() (string, error)) Option {
	return func(manager *Manager) error {
		if generator == nil {
			return errors.New("local member admin ID generator is required")
		}
		manager.newID = generator
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(manager *Manager) error {
		if clock == nil {
			return errors.New("local member admin clock is required")
		}
		manager.clock = clock
		return nil
	}
}

func NewManager(database *sql.DB, credentials *localcredential.SQLiteRepository, auditStore audit.Store, outboxStore outbox.TransactionalStore, executor operation.Executor, options ...Option) (*Manager, error) {
	if database == nil || credentials == nil || auditStore == nil || outboxStore == nil || executor == nil {
		return nil, errors.New("local member admin dependencies are required")
	}
	manager := &Manager{database: database, credentials: credentials, audit: auditStore, outbox: outboxStore, executor: executor, newID: randomID, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("local member admin option is required")
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (manager *Manager) Create(ctx context.Context, input CreateInput) (MemberResult, error) {
	if err := manager.ready(); err != nil {
		return MemberResult{}, err
	}
	value, err := manager.executor.Execute(ctx, createMemberPlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actorID, err := trustedActor(callContext, OperationCreateMember)
		if err != nil {
			return nil, err
		}
		displayName := strings.TrimSpace(input.DisplayName)
		email := strings.TrimSpace(input.Email)
		if displayName == "" || len(displayName) > 255 || len(email) > 320 || len(input.Password) == 0 {
			return nil, ErrInvalidInput
		}
		transaction, err := sqliteTransaction(callContext)
		if err != nil {
			return nil, err
		}
		now, err := manager.now()
		if err != nil {
			return nil, err
		}
		userID, err := manager.nextID()
		if err != nil {
			return nil, err
		}
		formatted := formatTime(now)
		if _, err := transaction.ExecContext(callContext, `INSERT INTO users (id, organization_id, display_name, email, status, created_at, updated_at, revision)
VALUES (?, ?, ?, NULLIF(?, ''), 'active', ?, ?, 1)`, userID, organizationID, displayName, email, formatted, formatted); err != nil {
			return nil, errors.New("create local member user")
		}
		credential, err := manager.credentials.SetPassword(callContext, organizationID, userID, input.Password, 0)
		if err != nil {
			return nil, err
		}
		result := MemberResult{OrganizationID: organizationID, UserID: userID, UserRevision: 1, CredentialRevision: credential.Revision}
		if err := manager.appendAudit(callContext, actorID, OperationCreateMember, "identity.member.created", "created", []string{"credential", "user"}, result, now); err != nil {
			return nil, err
		}
		if err := manager.stageOutbox(callContext, memberCreatedEvent, result, now); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return MemberResult{}, err
	}
	result, ok := value.(MemberResult)
	if !ok {
		return MemberResult{}, errors.New("local member create returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) Disable(ctx context.Context, input DisableInput) (MemberResult, error) {
	if err := manager.ready(); err != nil {
		return MemberResult{}, err
	}
	value, err := manager.executor.Execute(ctx, disableMemberPlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actorID, err := trustedActor(callContext, OperationDisableMember)
		if err != nil {
			return nil, err
		}
		if !canonicalIdentifier(input.UserID) || input.ExpectedRevision < 1 {
			return nil, ErrInvalidInput
		}
		transaction, err := sqliteTransaction(callContext)
		if err != nil {
			return nil, err
		}
		if err := ensureNotLastAdministrator(callContext, transaction, organizationID, input.UserID); err != nil {
			return nil, err
		}
		now, err := manager.now()
		if err != nil {
			return nil, err
		}
		result, err := transaction.ExecContext(callContext, `UPDATE users
SET status = 'disabled', revision = revision + 1, updated_at = ?
WHERE organization_id = ? AND id = ? AND status = 'active' AND revision = ?`, formatTime(now), organizationID, input.UserID, input.ExpectedRevision)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), lastAdministratorAbort) {
				return nil, ErrLastAdministrator
			}
			return nil, errors.New("disable local member")
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, errors.New("read local member disable result")
		}
		if changed != 1 {
			return nil, classifyMemberCAS(callContext, transaction, organizationID, input.UserID, input.ExpectedRevision)
		}
		member := MemberResult{OrganizationID: organizationID, UserID: input.UserID, UserRevision: input.ExpectedRevision + 1}
		if credential, credentialErr := manager.credentials.Metadata(callContext, organizationID, input.UserID); credentialErr == nil {
			member.CredentialRevision = credential.Revision
		} else if !errors.Is(credentialErr, localcredential.ErrNotFound) {
			return nil, credentialErr
		}
		if err := manager.appendAudit(callContext, actorID, OperationDisableMember, "identity.member.disabled", "changed", []string{"status"}, member, now); err != nil {
			return nil, err
		}
		if err := manager.stageOutbox(callContext, memberDisabledEvent, member, now); err != nil {
			return nil, err
		}
		return member, nil
	})
	if err != nil {
		return MemberResult{}, err
	}
	result, ok := value.(MemberResult)
	if !ok {
		return MemberResult{}, errors.New("local member disable returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) ResetCredential(ctx context.Context, input ResetCredentialInput) (MemberResult, error) {
	if err := manager.ready(); err != nil {
		return MemberResult{}, err
	}
	value, err := manager.executor.Execute(ctx, resetCredentialPlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actorID, err := trustedActor(callContext, OperationResetCredential)
		if err != nil {
			return nil, err
		}
		if !canonicalIdentifier(input.UserID) || input.ExpectedUserRevision < 1 || input.ExpectedCredentialRevision < 0 || len(input.Password) == 0 {
			return nil, ErrInvalidInput
		}
		transaction, err := sqliteTransaction(callContext)
		if err != nil {
			return nil, err
		}
		now, err := manager.now()
		if err != nil {
			return nil, err
		}
		result, err := transaction.ExecContext(callContext, `UPDATE users
SET revision = revision + 1, updated_at = ?
WHERE organization_id = ? AND id = ? AND status = 'active' AND revision = ?`, formatTime(now), organizationID, input.UserID, input.ExpectedUserRevision)
		if err != nil {
			return nil, errors.New("advance local member revision for credential reset")
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, errors.New("read local member credential reset CAS result")
		}
		if changed != 1 {
			return nil, classifyMemberCAS(callContext, transaction, organizationID, input.UserID, input.ExpectedUserRevision)
		}
		credential, err := manager.credentials.SetPassword(callContext, organizationID, input.UserID, input.Password, input.ExpectedCredentialRevision)
		if err != nil {
			return nil, err
		}
		member := MemberResult{OrganizationID: organizationID, UserID: input.UserID, UserRevision: input.ExpectedUserRevision + 1, CredentialRevision: credential.Revision}
		if err := manager.appendAudit(callContext, actorID, OperationResetCredential, "identity.member.credential_reset", "changed", []string{"credential", "revision"}, member, now); err != nil {
			return nil, err
		}
		if err := manager.stageOutbox(callContext, memberCredentialResetEvent, member, now); err != nil {
			return nil, err
		}
		return member, nil
	})
	if err != nil {
		return MemberResult{}, err
	}
	result, ok := value.(MemberResult)
	if !ok {
		return MemberResult{}, errors.New("local member credential reset returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) ready() error {
	if manager == nil || manager.database == nil || manager.credentials == nil || manager.audit == nil || manager.outbox == nil || manager.executor == nil || manager.newID == nil || manager.clock == nil {
		return errors.New("local member admin manager is not configured")
	}
	return nil
}

func trustedActor(ctx context.Context, operationID string) (string, string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || !canonicalIdentifier(principal.TenantID) || !canonicalIdentifier(principal.UserID) {
		return "", "", errors.New("local member admin requires a trusted JWT human principal")
	}
	organizationID := OrganizationIDFromContext(ctx)
	if !canonicalIdentifier(organizationID) || organizationID != principal.TenantID {
		return "", "", errors.New("local member admin requires an authorized organization scope")
	}
	metadata, ok := runtimecontext.MetadataFrom(ctx)
	if !ok || metadata.Operation != operationID {
		return "", "", errors.New("local member admin execution metadata does not match operation")
	}
	return organizationID, principal.UserID, nil
}

func sqliteTransaction(ctx context.Context) (*sql.Tx, error) {
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, errors.New("get local member admin transaction handle")
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("local member admin requires a SQLite root transaction")
	}
	return transaction, nil
}

func classifyMemberCAS(ctx context.Context, transaction *sql.Tx, organizationID, userID string, expectedRevision int64) error {
	var status string
	var revision int64
	err := transaction.QueryRowContext(ctx, `SELECT status, revision FROM users WHERE organization_id = ? AND id = ?`, organizationID, userID).Scan(&status, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return errors.New("read local member after CAS conflict")
	}
	if status != "active" {
		return ErrMemberDisabled
	}
	if revision != expectedRevision {
		return ErrMemberRevisionConflict
	}
	return ErrMemberRevisionConflict
}

func ensureNotLastAdministrator(ctx context.Context, transaction *sql.Tx, organizationID, userID string) error {
	var targetAdministrator int
	if err := transaction.QueryRowContext(ctx, administratorMembershipQuery, organizationID, userID).Scan(&targetAdministrator); err != nil {
		return errors.New("inspect local member administrator binding")
	}
	if targetAdministrator == 0 {
		return nil
	}
	var activeAdministrators int
	if err := transaction.QueryRowContext(ctx, activeAdministratorCountQuery, organizationID).Scan(&activeAdministrators); err != nil {
		return errors.New("count active local member administrators")
	}
	if activeAdministrators <= 1 {
		return ErrLastAdministrator
	}
	return nil
}

const administratorMembershipQuery = `
SELECT COUNT(*)
FROM users candidate
WHERE candidate.organization_id = ?
  AND candidate.id = ?
  AND candidate.status = 'active'
  AND EXISTS (
    SELECT 1 FROM role_bindings binding
    WHERE binding.organization_id = candidate.organization_id
      AND binding.role_id = 'system-administrator'
      AND binding.scope_type = 'organization'
      AND binding.scope_id = candidate.organization_id
      AND binding.status = 'active'
      AND (
        binding.user_id = candidate.id
        OR EXISTS (
          SELECT 1 FROM teams
          JOIN team_memberships
            ON team_memberships.team_id = teams.id
           AND team_memberships.organization_id = teams.organization_id
          WHERE teams.id = binding.team_id
            AND teams.organization_id = candidate.organization_id
            AND teams.status = 'active'
            AND team_memberships.user_id = candidate.id
        )
      )
  )`

const activeAdministratorCountQuery = `
SELECT COUNT(*)
FROM users candidate
WHERE candidate.organization_id = ?
  AND candidate.status = 'active'
  AND EXISTS (
    SELECT 1 FROM role_bindings binding
    WHERE binding.organization_id = candidate.organization_id
      AND binding.role_id = 'system-administrator'
      AND binding.scope_type = 'organization'
      AND binding.scope_id = candidate.organization_id
      AND binding.status = 'active'
      AND (
        binding.user_id = candidate.id
        OR EXISTS (
          SELECT 1 FROM teams
          JOIN team_memberships
            ON team_memberships.team_id = teams.id
           AND team_memberships.organization_id = teams.organization_id
          WHERE teams.id = binding.team_id
            AND teams.organization_id = candidate.organization_id
            AND teams.status = 'active'
            AND team_memberships.user_id = candidate.id
        )
      )
  )`

func (manager *Manager) appendAudit(ctx context.Context, actorID, operationID, reasonCode, change string, fields []string, result MemberResult, occurredAt time.Time) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	summary, err := audit.BuildDiffSummary(change, fields)
	if err != nil {
		return errors.New("build local member admin audit diff")
	}
	metadata, present := runtimecontext.MetadataFrom(ctx)
	if !present || metadata.Operation != operationID {
		return errors.New("local member admin audit metadata does not match operation")
	}
	attributes := map[string]any{
		"user_revision":       result.UserRevision,
		"credential_revision": result.CredentialRevision,
	}
	if metadata.Transport != "" {
		attributes["transport"] = metadata.Transport
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return errors.New("encode local member admin audit metadata")
	}
	_, err = manager.audit.Append(ctx, audit.Entry{
		ID:                    id,
		SchemaVersion:         audit.SchemaVersion,
		EventCategory:         audit.EventCategoryConfiguration,
		OrganizationID:        result.OrganizationID,
		ActorType:             audit.ActorHuman,
		ActorID:               actorID,
		Operation:             operationID,
		AuthorizationDecision: audit.DecisionAllowed,
		ScopeType:             audit.ScopeOrganization,
		ScopeID:               result.OrganizationID,
		TargetType:            "identity.user",
		TargetID:              result.UserID,
		Result:                audit.ResultSuccess,
		ReasonCode:            reasonCode,
		TraceID:               runtimecontext.TraceIDFrom(ctx),
		RequestID:             metadata.RequestID,
		CorrelationID:         metadata.Attributes[correlationIDAttribute],
		DiffSummary:           summary,
		Metadata:              string(encoded),
		OccurredAt:            occurredAt,
	})
	if err != nil {
		return errors.New("record local member admin audit")
	}
	return nil
}

func (manager *Manager) stageOutbox(ctx context.Context, eventType string, result MemberResult, occurredAt time.Time) error {
	transaction, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return errors.New("get local member admin Outbox transaction handle")
	}
	payload := struct {
		OrganizationID     string `json:"organizationId"`
		UserID             string `json:"userId"`
		UserRevision       int64  `json:"userRevision"`
		CredentialRevision int64  `json:"credentialRevision,omitzero"`
	}{
		OrganizationID:     result.OrganizationID,
		UserID:             result.UserID,
		UserRevision:       result.UserRevision,
		CredentialRevision: result.CredentialRevision,
	}
	envelope, err := event.NewJSON(memberEventTopic, eventType, "iot-delivery-system/local", payload)
	if err != nil {
		return errors.New("create local member admin Outbox event")
	}
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	envelope.ID = id
	envelope.Subject = result.UserID
	envelope.OccurredAt = occurredAt
	if envelope, err = envelope.Normalize(); err != nil {
		return errors.New("normalize local member admin Outbox event")
	}
	if err := manager.outbox.EnqueueTx(ctx, transaction, envelope); err != nil {
		return errors.New("stage local member admin Outbox event")
	}
	return nil
}

func (manager *Manager) nextID() (string, error) {
	value, err := manager.newID()
	if err != nil {
		return "", errors.New("generate local member admin ID")
	}
	if !canonicalIdentifier(value) {
		return "", errors.New("local member admin ID is invalid")
	}
	return value, nil
}

func (manager *Manager) now() (time.Time, error) {
	value := manager.clock().UTC()
	if value.IsZero() {
		return time.Time{}, errors.New("local member admin clock returned zero time")
	}
	return value, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read local member admin random identifier: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
