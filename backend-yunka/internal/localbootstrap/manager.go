package localbootstrap

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
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/pkg/operationplan"
)

var (
	ErrAlreadyInitialized  = errors.New("local administrator bootstrap is already initialized")
	ErrPreexistingIdentity = errors.New("local administrator bootstrap is permanently closed by preexisting identity")
	ErrOrganizationNotFound = errors.New("local administrator bootstrap organization was not found")
	ErrInvalidInput        = errors.New("local administrator bootstrap input is invalid")
)

const (
	initializeOperationID = "identity.local-admin-bootstrap.initialize"
	administratorRoleID   = "system-administrator"
	bootstrapActorID      = "local-admin-bootstrap"
)

var initializePlan = operationplan.Plan{
	OperationID: initializeOperationID,
	Domain:      "identity",
	Application: "local-admin-bootstrap",
	UseCase:     "initialize",
	Execution: operationplan.Execution{
		Transaction: "local",
		Idempotency: "none",
	},
	Composition: operationplan.Composition{Boundary: "local"},
}

// OperationPlan returns the internal, unprotected first-run operation contract.
// It intentionally declares no transport, authentication or permission because
// the durable one-time bootstrap latch is the authorization boundary before a
// first administrator exists.
func OperationPlan() operationplan.Plan { return initializePlan }

type InitializeInput struct {
	OrganizationID string
	DisplayName    string
	Email          string
	Password       []byte
}

type Result struct {
	OrganizationID     string
	UserID             string
	RoleBindingID      string
	CredentialRevision int64
}

type Manager struct {
	database    *sql.DB
	credentials *localcredential.SQLiteRepository
	audit       *audit.SQLiteStore
	executor    operation.Executor
	newID       func() (string, error)
	clock       func() time.Time
}

type Option func(*Manager) error

func WithIDGenerator(generator func() (string, error)) Option {
	return func(manager *Manager) error {
		if generator == nil {
			return errors.New("local bootstrap ID generator is required")
		}
		manager.newID = generator
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(manager *Manager) error {
		if clock == nil {
			return errors.New("local bootstrap clock is required")
		}
		manager.clock = clock
		return nil
	}
}

func NewManager(database *sql.DB, credentials *localcredential.SQLiteRepository, auditStore *audit.SQLiteStore, executor operation.Executor, options ...Option) (*Manager, error) {
	if database == nil || credentials == nil || auditStore == nil || executor == nil {
		return nil, errors.New("local administrator bootstrap dependencies are required")
	}
	manager := &Manager{database: database, credentials: credentials, audit: auditStore, executor: executor, newID: randomID, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("local administrator bootstrap option is required")
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

type initializeOutcome struct {
	result       Result
	blocked      bool
	closeReason  string
}

func (manager *Manager) Initialize(ctx context.Context, input InitializeInput) (Result, error) {
	if err := manager.ready(); err != nil {
		return Result{}, err
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if !canonicalIdentifier(input.OrganizationID) || input.DisplayName == "" || len(input.DisplayName) > 255 || len(input.Email) > 320 || len(input.Password) == 0 {
		return Result{}, ErrInvalidInput
	}
	value, err := manager.executor.Execute(ctx, initializePlan, &input, func(callContext context.Context) (any, error) {
		return manager.initialize(callContext, input)
	})
	if err != nil {
		return Result{}, err
	}
	outcome, ok := value.(initializeOutcome)
	if !ok {
		return Result{}, errors.New("local administrator bootstrap returned an unexpected result")
	}
	if outcome.blocked {
		if outcome.closeReason == "preexisting_identity" {
			return Result{}, ErrPreexistingIdentity
		}
		return Result{}, ErrAlreadyInitialized
	}
	return outcome.result, nil
}

func (manager *Manager) ready() error {
	if manager == nil || manager.database == nil || manager.credentials == nil || manager.audit == nil || manager.executor == nil || manager.newID == nil || manager.clock == nil {
		return errors.New("local administrator bootstrap manager is not configured")
	}
	return nil
}

func (manager *Manager) initialize(ctx context.Context, input InitializeInput) (initializeOutcome, error) {
	transaction, err := sqliteTransaction(ctx)
	if err != nil {
		return initializeOutcome{}, err
	}
	if reason, closed, err := closedReason(ctx, transaction); err != nil {
		return initializeOutcome{}, err
	} else if closed {
		return initializeOutcome{blocked: true, closeReason: reason}, nil
	}
	var userCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&userCount); err != nil {
		return initializeOutcome{}, errors.New("inspect identities before local administrator bootstrap")
	}
	if userCount != 0 {
		if err := insertPreexistingIdentityClosure(ctx, transaction); err != nil {
			return initializeOutcome{}, err
		}
		return initializeOutcome{blocked: true, closeReason: "preexisting_identity"}, nil
	}
	if err := requireActiveOrganization(ctx, transaction, input.OrganizationID); err != nil {
		return initializeOutcome{}, err
	}
	now := manager.clock().UTC()
	if now.IsZero() {
		return initializeOutcome{}, errors.New("local administrator bootstrap clock returned zero time")
	}
	userID, err := manager.nextID()
	if err != nil {
		return initializeOutcome{}, err
	}
	bindingID, err := manager.nextID()
	if err != nil {
		return initializeOutcome{}, err
	}
	auditID, err := manager.nextID()
	if err != nil {
		return initializeOutcome{}, err
	}
	claimed, err := claimBootstrap(ctx, transaction, input.OrganizationID, userID, now)
	if err != nil {
		return initializeOutcome{}, err
	}
	if !claimed {
		reason, closed, readErr := closedReason(ctx, transaction)
		if readErr != nil {
			return initializeOutcome{}, readErr
		}
		if !closed {
			return initializeOutcome{}, errors.New("local administrator bootstrap claim was not persisted")
		}
		return initializeOutcome{blocked: true, closeReason: reason}, nil
	}
	formatted := formatTime(now)
	if _, err := transaction.ExecContext(ctx, `INSERT INTO users (id, organization_id, display_name, email, status, created_at, updated_at)
VALUES (?, ?, ?, NULLIF(?, ''), 'active', ?, ?)`, userID, input.OrganizationID, input.DisplayName, input.Email, formatted, formatted); err != nil {
		return initializeOutcome{}, errors.New("create local bootstrap administrator user")
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, team_id, status, created_at, updated_at)
VALUES (?, ?, ?, 'organization', ?, ?, NULL, 'active', ?, ?)`, bindingID, input.OrganizationID, administratorRoleID, input.OrganizationID, userID, formatted, formatted); err != nil {
		return initializeOutcome{}, errors.New("bind local bootstrap system administrator role")
	}
	credential, err := manager.credentials.SetPassword(ctx, input.OrganizationID, userID, input.Password, 0)
	if err != nil {
		return initializeOutcome{}, err
	}
	if err := manager.appendAudit(ctx, transaction, auditID, input.OrganizationID, userID, now); err != nil {
		return initializeOutcome{}, err
	}
	return initializeOutcome{result: Result{
		OrganizationID: input.OrganizationID,
		UserID: userID,
		RoleBindingID: bindingID,
		CredentialRevision: credential.Revision,
	}}, nil
}

func sqliteTransaction(ctx context.Context) (*sql.Tx, error) {
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, errors.New("get local administrator bootstrap transaction handle")
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("local administrator bootstrap requires a SQLite root transaction")
	}
	return transaction, nil
}

func closedReason(ctx context.Context, transaction *sql.Tx) (string, bool, error) {
	var reason string
	err := transaction.QueryRowContext(ctx, `SELECT close_reason FROM iotd_local_admin_bootstrap_state WHERE id = ?`, stateID).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.New("read local administrator bootstrap state")
	}
	if reason != "initialized" && reason != "preexisting_identity" {
		return "", false, errors.New("local administrator bootstrap state is invalid")
	}
	return reason, true, nil
}

func insertPreexistingIdentityClosure(ctx context.Context, transaction *sql.Tx) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO iotd_local_admin_bootstrap_state (id, state, close_reason, organization_id, initialized_user_id, closed_at, created_at)
VALUES (?, 'closed', 'preexisting_identity', NULL, NULL, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT(id) DO NOTHING`, stateID)
	if err != nil {
		return errors.New("permanently close local administrator bootstrap for preexisting identity")
	}
	return nil
}

func requireActiveOrganization(ctx context.Context, transaction *sql.Tx, organizationID string) error {
	var status string
	if err := transaction.QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = ?`, organizationID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOrganizationNotFound
		}
		return errors.New("read local administrator bootstrap organization")
	}
	if status != "active" {
		return ErrOrganizationNotFound
	}
	return nil
}

func claimBootstrap(ctx context.Context, transaction *sql.Tx, organizationID, userID string, now time.Time) (bool, error) {
	result, err := transaction.ExecContext(ctx, `INSERT INTO iotd_local_admin_bootstrap_state (id, state, close_reason, organization_id, initialized_user_id, closed_at, created_at)
VALUES (?, 'closed', 'initialized', ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING`, stateID, organizationID, userID, formatTime(now), formatTime(now))
	if err != nil {
		return false, errors.New("claim local administrator bootstrap")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, errors.New("read local administrator bootstrap claim")
	}
	return rows == 1, nil
}

func (manager *Manager) appendAudit(ctx context.Context, transaction *sql.Tx, auditID, organizationID, userID string, occurredAt time.Time) error {
	diffSummary, err := audit.BuildDiffSummary("created", []string{"bootstrap.state", "credential", "role.binding", "user"})
	if err != nil {
		return errors.New("build local administrator bootstrap audit diff")
	}
	metadata, err := json.Marshal(map[string]string{"bootstrap_state": "closed", "role": administratorRoleID})
	if err != nil {
		return errors.New("encode local administrator bootstrap audit metadata")
	}
	_, err = manager.audit.AppendInTransaction(ctx, transaction, audit.Entry{
		ID:                    auditID,
		SchemaVersion:         audit.SchemaVersion,
		EventCategory:         audit.EventCategorySystem,
		OrganizationID:        organizationID,
		ActorType:             audit.ActorSystem,
		ActorID:               bootstrapActorID,
		Operation:             initializeOperationID,
		AuthorizationDecision: audit.DecisionNotEvaluated,
		ScopeType:             audit.ScopeOrganization,
		ScopeID:               organizationID,
		TargetType:            "identity.user",
		TargetID:              userID,
		Result:                audit.ResultSuccess,
		ReasonCode:            "bootstrap.initialized",
		DiffSummary:           diffSummary,
		Metadata:              string(metadata),
		OccurredAt:            occurredAt,
	})
	if err != nil {
		return errors.New("record local administrator bootstrap audit")
	}
	return nil
}

func (manager *Manager) nextID() (string, error) {
	id, err := manager.newID()
	if err != nil {
		return "", errors.New("generate local administrator bootstrap ID")
	}
	if !canonicalIdentifier(id) {
		return "", errors.New("local administrator bootstrap ID is invalid")
	}
	return id, nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read random identifier: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
