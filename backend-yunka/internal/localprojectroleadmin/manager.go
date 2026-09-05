package localprojectroleadmin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/event"
	"github.com/hvritual/yunka.io/framework/event/outbox"
	"github.com/hvritual/yunka.io/framework/execution"
	"github.com/hvritual/yunka.io/framework/operation"
)

var (
	ErrInvalidInput            = errors.New("project role administration input is invalid")
	ErrProjectNotFound         = errors.New("project role project was not found")
	ErrMemberNotFound          = errors.New("project role member was not found")
	ErrMemberDisabled          = errors.New("project role member is disabled")
	ErrRoleNotAssignable       = errors.New("project role is not assignable")
	ErrRoleContractDrift       = errors.New("project role durable contract has drifted")
	ErrBindingAlreadyActive    = errors.New("project role binding is already active")
	ErrBindingNotFound         = errors.New("project role binding was not found")
	ErrBindingRevisionConflict = errors.New("project role binding revision conflict")
	ErrBindingRevoked          = errors.New("project role binding is revoked")
)

const (
	roleBindingEventTopic  = "identity.project-role-bindings"
	roleAssignedEvent      = "identity.project-role-binding.assigned"
	roleRevokedEvent       = "identity.project-role-binding.revoked"
	correlationIDAttribute = "correlation_id"
)

type AssignInput struct {
	ProjectID string
	UserID    string
	RoleID    string
}

type RevokeInput struct {
	BindingID        string
	ExpectedRevision int64
}

type BindingResult struct {
	BindingID      string
	OrganizationID string
	ProjectID      string
	UserID         string
	RoleID         string
	Status         string
	Revision       int64
}

type projectLookup interface {
	GetProject(context.Context, string) (delivery.Project, error)
}

type roleContract struct {
	bindingScope string
	grants       map[string][]string
}

type Manager struct {
	database     *sql.DB
	projects     projectLookup
	audit        audit.Store
	outbox       outbox.TransactionalStore
	executor     operation.Executor
	projectRoles map[string]roleContract
	permissions  map[string]authorization.Permission
	newID        func() (string, error)
	clock        func() time.Time
}

type Option func(*Manager) error

func WithIDGenerator(generator func() (string, error)) Option {
	return func(manager *Manager) error {
		if generator == nil {
			return errors.New("project role ID generator is required")
		}
		manager.newID = generator
		return nil
	}
}

func WithClock(clock func() time.Time) Option {
	return func(manager *Manager) error {
		if clock == nil {
			return errors.New("project role clock is required")
		}
		manager.clock = clock
		return nil
	}
}

func NewManager(database *sql.DB, projects projectLookup, auditStore audit.Store, outboxStore outbox.TransactionalStore, executor operation.Executor, options ...Option) (*Manager, error) {
	if database == nil || projects == nil || auditStore == nil || outboxStore == nil || executor == nil {
		return nil, errors.New("project role administration dependencies are required")
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return nil, fmt.Errorf("load project role dictionary: %w", err)
	}
	permissions := make(map[string]authorization.Permission, len(dictionary.Permissions))
	for _, permission := range dictionary.Permissions {
		if !canonicalIdentifier(permission.ID) || !canonicalIdentifier(permission.Resource) || !canonicalIdentifier(permission.Action) || len(permission.AllowedScopes) == 0 {
			return nil, errors.New("project role dictionary contains an invalid permission")
		}
		if _, duplicate := permissions[permission.ID]; duplicate {
			return nil, errors.New("project role dictionary contains a duplicated permission")
		}
		permission.AllowedScopes = append([]string(nil), permission.AllowedScopes...)
		sort.Strings(permission.AllowedScopes)
		// YU-24 activates the predeclared RoleBinding management permission in
		// durable storage without rewriting the versioned JSON dictionary.
		if permission.ID == PermissionManageRoleBindings {
			permission.Status = "active"
		}
		permissions[permission.ID] = permission
	}
	projectRoles := make(map[string]roleContract)
	for _, role := range dictionary.Roles {
		if role.BindingScope != "project" {
			continue
		}
		contract := roleContract{bindingScope: role.BindingScope, grants: make(map[string][]string)}
		for _, grant := range role.Grants {
			if !canonicalIdentifier(grant.Permission) || len(grant.AllowedScopes) == 0 {
				return nil, errors.New("project role dictionary contains an invalid grant")
			}
			if _, exists := permissions[grant.Permission]; !exists {
				return nil, errors.New("project role dictionary grant references an unknown permission")
			}
			scopes := append([]string(nil), grant.AllowedScopes...)
			sort.Strings(scopes)
			if _, duplicate := contract.grants[grant.Permission]; duplicate {
				return nil, errors.New("project role dictionary contains a duplicated grant")
			}
			contract.grants[grant.Permission] = scopes
		}
		if len(contract.grants) == 0 {
			return nil, errors.New("project role dictionary contains an empty project role")
		}
		projectRoles[role.ID] = contract
	}
	if len(projectRoles) == 0 {
		return nil, errors.New("project role dictionary has no project-scoped roles")
	}
	manager := &Manager{
		database: database, projects: projects, audit: auditStore, outbox: outboxStore,
		executor: executor, projectRoles: projectRoles, permissions: permissions, newID: randomID, clock: time.Now,
	}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("project role administration option is required")
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (manager *Manager) Assign(ctx context.Context, input AssignInput) (BindingResult, error) {
	if err := manager.ready(); err != nil {
		return BindingResult{}, err
	}
	value, err := manager.executor.Execute(ctx, assignProjectRolePlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actorID, err := trustedActor(callContext, OperationAssignProjectRole)
		if err != nil {
			return nil, err
		}
		if !canonicalIdentifier(input.ProjectID) || !canonicalIdentifier(input.UserID) || !canonicalIdentifier(input.RoleID) {
			return nil, ErrInvalidInput
		}
		transaction, err := sqliteTransaction(callContext)
		if err != nil {
			return nil, err
		}
		project, err := manager.projects.GetProject(callContext, input.ProjectID)
		if errors.Is(err, delivery.ErrNotFound) || (err == nil && project.OrganizationID != organizationID) {
			return nil, ErrProjectNotFound
		}
		if err != nil {
			return nil, errors.New("read project for role assignment")
		}
		if !canonicalIdentifier(project.ID) || project.ID != input.ProjectID || project.OrganizationID != organizationID {
			return nil, ErrProjectNotFound
		}
		if err := ensureActiveMember(callContext, transaction, organizationID, input.UserID); err != nil {
			return nil, err
		}
		if err := manager.ensureAssignableRole(callContext, transaction, input.RoleID); err != nil {
			return nil, err
		}
		if active, err := activeBindingForTuple(callContext, transaction, organizationID, input.ProjectID, input.UserID, input.RoleID); err != nil {
			return nil, err
		} else if active.BindingID != "" {
			return nil, ErrBindingAlreadyActive
		}
		now, err := manager.now()
		if err != nil {
			return nil, err
		}
		bindingID, err := manager.nextID()
		if err != nil {
			return nil, err
		}
		formatted := formatTime(now)
		_, err = transaction.ExecContext(callContext, `INSERT INTO role_bindings
(id, organization_id, role_id, scope_type, scope_id, user_id, team_id, status, created_at, updated_at, revision)
VALUES (?, ?, ?, 'project', ?, ?, NULL, 'active', ?, ?, 1)`,
			bindingID, organizationID, input.RoleID, input.ProjectID, input.UserID, formatted, formatted)
		if err != nil {
			if active, classifyErr := activeBindingForTuple(callContext, transaction, organizationID, input.ProjectID, input.UserID, input.RoleID); classifyErr == nil && active.BindingID != "" {
				return nil, ErrBindingAlreadyActive
			}
			return nil, errors.New("assign project role binding")
		}
		result := BindingResult{
			BindingID: bindingID, OrganizationID: organizationID, ProjectID: input.ProjectID,
			UserID: input.UserID, RoleID: input.RoleID, Status: "active", Revision: 1,
		}
		if err := manager.appendAudit(callContext, actorID, OperationAssignProjectRole, "identity.project_role_binding.assigned", "created", []string{"role_binding"}, result, now); err != nil {
			return nil, err
		}
		if err := manager.stageOutbox(callContext, roleAssignedEvent, result, now); err != nil {
			return nil, err
		}
		return result, nil
	})
	if err != nil {
		return BindingResult{}, err
	}
	result, ok := value.(BindingResult)
	if !ok {
		return BindingResult{}, errors.New("project role assignment returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) Revoke(ctx context.Context, input RevokeInput) (BindingResult, error) {
	if err := manager.ready(); err != nil {
		return BindingResult{}, err
	}
	value, err := manager.executor.Execute(ctx, revokeProjectRolePlan, &input, func(callContext context.Context) (any, error) {
		organizationID, actorID, err := trustedActor(callContext, OperationRevokeProjectRole)
		if err != nil {
			return nil, err
		}
		if !canonicalIdentifier(input.BindingID) || input.ExpectedRevision < 1 {
			return nil, ErrInvalidInput
		}
		transaction, err := sqliteTransaction(callContext)
		if err != nil {
			return nil, err
		}
		binding, err := readBinding(callContext, transaction, organizationID, input.BindingID)
		if err != nil {
			return nil, err
		}
		if binding.Status != "active" {
			return nil, ErrBindingRevoked
		}
		if binding.Revision != input.ExpectedRevision {
			return nil, ErrBindingRevisionConflict
		}
		project, err := manager.projects.GetProject(callContext, binding.ProjectID)
		if errors.Is(err, delivery.ErrNotFound) || (err == nil && project.OrganizationID != organizationID) {
			return nil, ErrProjectNotFound
		}
		if err != nil {
			return nil, errors.New("read project for role revocation")
		}
		if project.ID != binding.ProjectID || project.OrganizationID != organizationID {
			return nil, ErrProjectNotFound
		}
		now, err := manager.now()
		if err != nil {
			return nil, err
		}
		update, err := transaction.ExecContext(callContext, `UPDATE role_bindings
SET status = 'disabled', revision = revision + 1, updated_at = ?
WHERE id = ? AND organization_id = ? AND scope_type = 'project' AND user_id IS NOT NULL AND team_id IS NULL AND status = 'active' AND revision = ?`,
			formatTime(now), binding.BindingID, organizationID, input.ExpectedRevision)
		if err != nil {
			return nil, errors.New("revoke project role binding")
		}
		changed, err := update.RowsAffected()
		if err != nil {
			return nil, errors.New("read project role revoke CAS result")
		}
		if changed != 1 {
			return nil, classifyBindingCAS(callContext, transaction, organizationID, input.BindingID, input.ExpectedRevision)
		}
		binding.Status = "disabled"
		binding.Revision = input.ExpectedRevision + 1
		if err := manager.appendAudit(callContext, actorID, OperationRevokeProjectRole, "identity.project_role_binding.revoked", "changed", []string{"role_binding.revision", "role_binding.status"}, binding, now); err != nil {
			return nil, err
		}
		if err := manager.stageOutbox(callContext, roleRevokedEvent, binding, now); err != nil {
			return nil, err
		}
		return binding, nil
	})
	if err != nil {
		return BindingResult{}, err
	}
	result, ok := value.(BindingResult)
	if !ok {
		return BindingResult{}, errors.New("project role revocation returned an unexpected result")
	}
	return result, nil
}

func (manager *Manager) ready() error {
	if manager == nil || manager.database == nil || manager.projects == nil || manager.audit == nil || manager.outbox == nil || manager.executor == nil || len(manager.projectRoles) == 0 || len(manager.permissions) == 0 || manager.newID == nil || manager.clock == nil {
		return errors.New("project role administration manager is not configured")
	}
	return nil
}

func ensureActiveMember(ctx context.Context, transaction *sql.Tx, organizationID, userID string) error {
	var status string
	err := transaction.QueryRowContext(ctx, `SELECT status FROM users WHERE organization_id = ? AND id = ?`, organizationID, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return errors.New("read project role member")
	}
	if status != "active" {
		return ErrMemberDisabled
	}
	return nil
}

func (manager *Manager) ensureAssignableRole(ctx context.Context, transaction *sql.Tx, roleID string) error {
	expected, ok := manager.projectRoles[roleID]
	if !ok || expected.bindingScope != "project" {
		return ErrRoleNotAssignable
	}
	var bindingScope string
	if err := transaction.QueryRowContext(ctx, `SELECT binding_scope FROM roles WHERE id = ?`, roleID).Scan(&bindingScope); errors.Is(err, sql.ErrNoRows) {
		return ErrRoleNotAssignable
	} else if err != nil {
		return errors.New("read assignable project role")
	} else if bindingScope != "project" {
		return ErrRoleContractDrift
	}
	rows, err := transaction.QueryContext(ctx, `SELECT role_grant.permission_id, allowed.scope_type
FROM role_permission_grants role_grant
JOIN role_permission_grant_allowed_scopes allowed
  ON allowed.role_id = role_grant.role_id
 AND allowed.permission_id = role_grant.permission_id
WHERE role_grant.role_id = ?
ORDER BY role_grant.permission_id, allowed.scope_type`, roleID)
	if err != nil {
		return errors.New("read assignable project role grants")
	}
	actual := make(map[string][]string)
	for rows.Next() {
		var permission, scope string
		if err := rows.Scan(&permission, &scope); err != nil {
			_ = rows.Close()
			return errors.New("scan assignable project role grant")
		}
		actual[permission] = append(actual[permission], scope)
	}
	if err := rows.Close(); err != nil {
		return errors.New("close assignable project role grants")
	}
	if len(actual) != len(expected.grants) {
		return ErrRoleContractDrift
	}
	for permission, expectedGrantScopes := range expected.grants {
		actualGrantScopes, exists := actual[permission]
		if !exists {
			return ErrRoleContractDrift
		}
		sort.Strings(actualGrantScopes)
		if !sameStrings(actualGrantScopes, expectedGrantScopes) {
			return ErrRoleContractDrift
		}
		expectedPermission, exists := manager.permissions[permission]
		if !exists {
			return ErrRoleContractDrift
		}
		var resource, action, status string
		if err := transaction.QueryRowContext(ctx, `SELECT resource, action, status FROM permissions WHERE id = ?`, permission).Scan(&resource, &action, &status); err != nil {
			return ErrRoleContractDrift
		}
		if resource != expectedPermission.Resource || action != expectedPermission.Action || status != expectedPermission.Status {
			return ErrRoleContractDrift
		}
		scopeRows, err := transaction.QueryContext(ctx, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = ? ORDER BY scope_type`, permission)
		if err != nil {
			return ErrRoleContractDrift
		}
		actualPermissionScopes := make([]string, 0, len(expectedPermission.AllowedScopes))
		for scopeRows.Next() {
			var scope string
			if err := scopeRows.Scan(&scope); err != nil {
				_ = scopeRows.Close()
				return ErrRoleContractDrift
			}
			actualPermissionScopes = append(actualPermissionScopes, scope)
		}
		if err := scopeRows.Err(); err != nil {
			_ = scopeRows.Close()
			return ErrRoleContractDrift
		}
		if err := scopeRows.Close(); err != nil {
			return ErrRoleContractDrift
		}
		if !sameStrings(actualPermissionScopes, expectedPermission.AllowedScopes) {
			return ErrRoleContractDrift
		}
	}
	return nil
}

func activeBindingForTuple(ctx context.Context, transaction *sql.Tx, organizationID, projectID, userID, roleID string) (BindingResult, error) {
	var result BindingResult
	err := transaction.QueryRowContext(ctx, `SELECT id, revision
FROM role_bindings
WHERE organization_id = ? AND role_id = ? AND scope_type = 'project' AND scope_id = ? AND user_id = ? AND team_id IS NULL AND status = 'active'`,
		organizationID, roleID, projectID, userID).Scan(&result.BindingID, &result.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return BindingResult{}, nil
	}
	if err != nil {
		return BindingResult{}, errors.New("read active project role binding")
	}
	result.OrganizationID = organizationID
	result.ProjectID = projectID
	result.UserID = userID
	result.RoleID = roleID
	result.Status = "active"
	return result, nil
}

func readBinding(ctx context.Context, transaction *sql.Tx, organizationID, bindingID string) (BindingResult, error) {
	var result BindingResult
	var scopeType string
	var teamID sql.NullString
	var userID sql.NullString
	err := transaction.QueryRowContext(ctx, `SELECT id, organization_id, role_id, scope_type, scope_id, user_id, team_id, status, revision
FROM role_bindings WHERE id = ? AND organization_id = ?`, bindingID, organizationID).Scan(
		&result.BindingID, &result.OrganizationID, &result.RoleID, &scopeType, &result.ProjectID,
		&userID, &teamID, &result.Status, &result.Revision,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return BindingResult{}, ErrBindingNotFound
	}
	if err != nil {
		return BindingResult{}, errors.New("read project role binding")
	}
	if scopeType != "project" || !userID.Valid || teamID.Valid || !canonicalIdentifier(userID.String) || !canonicalIdentifier(result.ProjectID) || !canonicalIdentifier(result.RoleID) || result.Revision < 1 {
		return BindingResult{}, ErrBindingNotFound
	}
	result.UserID = userID.String
	return result, nil
}

func classifyBindingCAS(ctx context.Context, transaction *sql.Tx, organizationID, bindingID string, expectedRevision int64) error {
	binding, err := readBinding(ctx, transaction, organizationID, bindingID)
	if err != nil {
		return err
	}
	if binding.Status != "active" {
		return ErrBindingRevoked
	}
	if binding.Revision != expectedRevision {
		return ErrBindingRevisionConflict
	}
	return ErrBindingRevisionConflict
}

func trustedActor(ctx context.Context, operationID string) (string, string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || !canonicalIdentifier(principal.TenantID) || !canonicalIdentifier(principal.UserID) {
		return "", "", errors.New("project role administration requires a trusted JWT human principal")
	}
	organizationID := OrganizationIDFromContext(ctx)
	if !canonicalIdentifier(organizationID) || organizationID != principal.TenantID {
		return "", "", errors.New("project role administration requires an authorized organization scope")
	}
	metadata, ok := runtimecontext.MetadataFrom(ctx)
	if !ok || metadata.Operation != operationID {
		return "", "", errors.New("project role execution metadata does not match operation")
	}
	return organizationID, principal.UserID, nil
}

func sqliteTransaction(ctx context.Context) (*sql.Tx, error) {
	handle, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return nil, errors.New("get project role transaction handle")
	}
	transaction, ok := handle.(*sql.Tx)
	if !ok || transaction == nil {
		return nil, errors.New("project role administration requires a SQLite root transaction")
	}
	return transaction, nil
}

func (manager *Manager) appendAudit(ctx context.Context, actorID, operationID, reasonCode, change string, fields []string, result BindingResult, occurredAt time.Time) error {
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	summary, err := audit.BuildDiffSummary(change, fields)
	if err != nil {
		return errors.New("build project role audit diff")
	}
	metadata, present := runtimecontext.MetadataFrom(ctx)
	if !present || metadata.Operation != operationID {
		return errors.New("project role audit metadata does not match operation")
	}
	attributes := map[string]any{
		"binding_revision": result.Revision,
		"role_id":          result.RoleID,
		"user_id":          result.UserID,
	}
	if metadata.Transport != "" {
		attributes["transport"] = metadata.Transport
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return errors.New("encode project role audit metadata")
	}
	_, err = manager.audit.Append(ctx, audit.Entry{
		ID: id, SchemaVersion: audit.SchemaVersion, EventCategory: audit.EventCategoryConfiguration,
		OrganizationID: result.OrganizationID, ProjectID: result.ProjectID,
		ActorType: audit.ActorHuman, ActorID: actorID, Operation: operationID,
		AuthorizationDecision: audit.DecisionAllowed,
		ScopeType: audit.ScopeProject, ScopeID: result.ProjectID,
		TargetType: "identity.role-binding", TargetID: result.BindingID,
		Result: audit.ResultSuccess, ReasonCode: reasonCode,
		TraceID: runtimecontext.TraceIDFrom(ctx), RequestID: metadata.RequestID,
		CorrelationID: metadata.Attributes[correlationIDAttribute],
		DiffSummary: summary, Metadata: string(encoded), OccurredAt: occurredAt,
	})
	if err != nil {
		return errors.New("record project role audit")
	}
	return nil
}

func (manager *Manager) stageOutbox(ctx context.Context, eventType string, result BindingResult, occurredAt time.Time) error {
	transaction, err := execution.TransactionHandleFrom(ctx)
	if err != nil {
		return errors.New("get project role Outbox transaction handle")
	}
	payload := struct {
		BindingID      string `json:"bindingId"`
		OrganizationID string `json:"organizationId"`
		ProjectID      string `json:"projectId"`
		UserID         string `json:"userId"`
		RoleID         string `json:"roleId"`
		Status         string `json:"status"`
		Revision       int64  `json:"revision"`
	}{
		BindingID: result.BindingID, OrganizationID: result.OrganizationID, ProjectID: result.ProjectID,
		UserID: result.UserID, RoleID: result.RoleID, Status: result.Status, Revision: result.Revision,
	}
	envelope, err := event.NewJSON(roleBindingEventTopic, eventType, "iot-delivery-system/local", payload)
	if err != nil {
		return errors.New("create project role Outbox event")
	}
	id, err := manager.nextID()
	if err != nil {
		return err
	}
	envelope.ID = id
	envelope.Subject = result.BindingID
	envelope.OccurredAt = occurredAt
	if envelope, err = envelope.Normalize(); err != nil {
		return errors.New("normalize project role Outbox event")
	}
	if err := manager.outbox.EnqueueTx(ctx, transaction, envelope); err != nil {
		return errors.New("stage project role Outbox event")
	}
	return nil
}

func (manager *Manager) nextID() (string, error) {
	value, err := manager.newID()
	if err != nil {
		return "", errors.New("generate project role ID")
	}
	if !canonicalIdentifier(value) {
		return "", errors.New("project role ID is invalid")
	}
	return value, nil
}

func (manager *Manager) now() (time.Time, error) {
	value := manager.clock().UTC()
	if value.IsZero() {
		return time.Time{}, errors.New("project role clock returned zero time")
	}
	return value, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read project role random identifier: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
