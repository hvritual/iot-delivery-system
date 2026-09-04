// Package serviceauthz owns explicit, per-operation service-account grants.
// Service identities are never human roles and receive no implicit authority.
package serviceauthz

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

var (
	ErrDatabaseRequired      = errors.New("service grant resolver database is required")
	ErrProjectLookupRequired = errors.New("service grant project lookup is required")
	ErrInvalidGrantRequest   = errors.New("service grant request is invalid")
	ErrGrantNotFound         = errors.New("service grant was not found")
)

type projectLookup interface {
	GetProject(context.Context, string) (delivery.Project, error)
}

// GrantInput expresses the only supported service authorization shape: one
// service account, one registered operation, one matching permission, and one
// project. It deliberately has no organization-wide or wildcard fields.
type GrantInput struct {
	ID               string
	ServiceAccountID string
	OperationID      string
	Permission       string
	ProjectID        string
}

type Manager struct {
	database      *sql.DB
	projects      projectLookup
	now           func() time.Time
	auditRecorder *audit.SecurityRecorder
}

type ManagerOption func(*Manager) error

func WithAuditRecorder(recorder *audit.SecurityRecorder) ManagerOption {
	return func(manager *Manager) error {
		if recorder == nil {
			return errors.New("service grant audit recorder is required")
		}
		manager.auditRecorder = recorder
		return nil
	}
}

type Resolver struct{ database *sql.DB }

var _ authz.GrantResolver = (*Resolver)(nil)

func NewManager(database *sql.DB, projects projectLookup, options ...ManagerOption) (*Manager, error) {
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	if projects == nil {
		return nil, ErrProjectLookupRequired
	}
	manager := &Manager{database: database, projects: projects, now: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("service grant manager option is required")
		}
		if err := option(manager); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func NewGrantResolver(database *sql.DB) (*Resolver, error) {
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	return &Resolver{database: database}, nil
}

func (manager *Manager) Grant(ctx context.Context, input GrantInput) error {
	if manager == nil || manager.database == nil || manager.projects == nil || manager.now == nil || !validGrantInput(input) {
		return ErrInvalidGrantRequest
	}
	organizationID, err := activeServiceGrantOrganization(ctx, manager.database, input)
	if err != nil {
		return err
	}
	project, err := manager.projects.GetProject(ctx, input.ProjectID)
	if err != nil || project.ID != input.ProjectID || project.OrganizationID != organizationID {
		return ErrInvalidGrantRequest
	}
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin service grant: %w", err)
	}
	defer transaction.Rollback()
	verifiedOrganizationID, err := activeServiceGrantOrganization(ctx, transaction, input)
	if err != nil {
		return err
	}
	if verifiedOrganizationID != organizationID {
		return ErrInvalidGrantRequest
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO service_operation_grants (
    id, organization_id, service_account_id, operation_id, permission_id, project_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`, input.ID, organizationID, input.ServiceAccountID, input.OperationID, input.Permission, input.ProjectID, formatTime(manager.now()), formatTime(manager.now()))
	if err != nil {
		return fmt.Errorf("persist service grant: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit service grant: %w", err)
	}
	return nil
}

func (manager *Manager) Revoke(ctx context.Context, grantID string) error {
	if manager == nil || manager.database == nil || manager.now == nil || !canonicalID(grantID) {
		return ErrInvalidGrantRequest
	}
	if manager.auditRecorder != nil {
		return manager.revokeWithAudit(ctx, grantID)
	}
	result, err := manager.database.ExecContext(ctx, `UPDATE service_operation_grants SET status = 'revoked', revoked_at = ?, updated_at = ? WHERE id = ? AND status = 'active'`, formatTime(manager.now()), formatTime(manager.now()), grantID)
	if err != nil {
		return fmt.Errorf("revoke service grant: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check service grant revocation: %w", err)
	}
	if changed == 1 {
		return nil
	}
	var found int
	err = manager.database.QueryRowContext(ctx, `SELECT 1 FROM service_operation_grants WHERE id = ?`, grantID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrGrantNotFound
	}
	if err != nil {
		return fmt.Errorf("read service grant for revocation: %w", err)
	}
	return nil
}

func (manager *Manager) revokeWithAudit(ctx context.Context, grantID string) error {
	transaction, err := manager.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin service grant revocation: %w", err)
	}
	defer transaction.Rollback()
	var targetGrantID, targetOrganizationID, targetServiceAccountID, targetProjectID, targetOperationID string
	if err := transaction.QueryRowContext(ctx, `
SELECT id, organization_id, service_account_id, project_id, operation_id
FROM service_operation_grants WHERE id = ?`, grantID).Scan(&targetGrantID, &targetOrganizationID, &targetServiceAccountID, &targetProjectID, &targetOperationID); errors.Is(err, sql.ErrNoRows) {
		return ErrGrantNotFound
	} else if err != nil {
		return fmt.Errorf("read service grant for revocation: %w", err)
	}
	if !canonicalID(targetGrantID) || !canonicalID(targetOrganizationID) || !canonicalID(targetServiceAccountID) || !canonicalID(targetProjectID) || !canonicalID(targetOperationID) {
		return ErrInvalidGrantRequest
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE service_operation_grants SET status = 'revoked', revoked_at = ?, updated_at = ? WHERE id = ? AND status = 'active'`, formatTime(manager.now()), formatTime(manager.now()), grantID); err != nil {
		return fmt.Errorf("revoke service grant: %w", err)
	}
	if err := manager.auditRecorder.RecordRevocationInTransaction(ctx, transaction, "configuration.service_grant.revoke", "service.grant", targetGrantID, targetOrganizationID); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit service grant revocation: %w", err)
	}
	return nil
}

func (resolver *Resolver) ResolveGrants(ctx context.Context, request authz.GrantRequest) ([]authz.Grant, error) {
	if resolver == nil || resolver.database == nil {
		return nil, ErrDatabaseRequired
	}
	serviceAccountID, ok := serviceAccountIDFromPrincipal(request.Principal)
	if !ok || !canonicalID(string(request.Operation)) || len(request.Permissions) != 1 || !canonicalID(string(request.Permissions[0])) {
		return nil, nil
	}
	rows, err := resolver.database.QueryContext(ctx, activeServiceGrantsQuery, request.Principal.TenantID, serviceAccountID, string(request.Operation), string(request.Permissions[0]))
	if err != nil {
		return nil, fmt.Errorf("resolve service grants: %w", err)
	}
	defer rows.Close()
	grants := make([]authz.Grant, 0)
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("scan service grant: %w", err)
		}
		if !canonicalID(projectID) {
			return nil, nil
		}
		grants = append(grants, authz.Grant{Permission: request.Permissions[0], RoleID: "service-account:" + serviceAccountID, Scope: "project:" + projectID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service grants: %w", err)
	}
	configRows, err := resolver.database.QueryContext(ctx, activeConfigServiceGrantsQuery, request.Principal.TenantID, serviceAccountID, string(request.Operation), string(request.Permissions[0]))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			slices.SortFunc(grants, func(left, right authz.Grant) int { return cmp.Compare(left.Scope, right.Scope) })
			return grants, nil
		}
		return nil, fmt.Errorf("resolve config service grants: %w", err)
	}
	defer configRows.Close()
	for configRows.Next() {
		var organizationID string
		if err := configRows.Scan(&organizationID); err != nil {
			return nil, fmt.Errorf("scan config service grant: %w", err)
		}
		if !canonicalID(organizationID) {
			return nil, nil
		}
		grants = append(grants, authz.Grant{Permission: request.Permissions[0], RoleID: "service-account:" + serviceAccountID, Scope: "organization:" + organizationID})
	}
	if err := configRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate config service grants: %w", err)
	}
	slices.SortFunc(grants, func(left, right authz.Grant) int { return cmp.Compare(left.Scope, right.Scope) })
	return grants, nil
}

const activeServiceGrantsQuery = `
SELECT grants.project_id
FROM service_operation_grants grants
JOIN service_accounts accounts
  ON accounts.id = grants.service_account_id
 AND accounts.organization_id = grants.organization_id
 AND accounts.status = 'active'
JOIN organizations organizations
  ON organizations.id = grants.organization_id
 AND organizations.status = 'active'
JOIN service_operations operations
  ON operations.id = grants.operation_id
 AND operations.permission_id = grants.permission_id
 AND operations.required_scope IN ('project', 'object')
 AND operations.status = 'active'
JOIN permissions permissions
  ON permissions.id = grants.permission_id
 AND permissions.status = 'active'
WHERE grants.status = 'active'
  AND grants.organization_id = ?
  AND grants.service_account_id = ?
  AND grants.operation_id = ?
  AND grants.permission_id = ?
ORDER BY grants.project_id ASC`

const activeConfigServiceGrantsQuery = `
SELECT grants.organization_id
FROM iotd_config_service_grants grants
JOIN service_accounts accounts ON accounts.id = grants.service_account_id AND accounts.organization_id = grants.organization_id AND accounts.status = 'active'
JOIN organizations organizations ON organizations.id = grants.organization_id AND organizations.status = 'active'
JOIN service_operations operations ON operations.id = grants.operation_id AND operations.id GLOB 'config.revisions.*' AND operations.permission_id = grants.permission_id AND operations.required_scope = 'organization' AND operations.status = 'active'
JOIN permissions permissions ON permissions.id = grants.permission_id AND permissions.status = 'active'
WHERE grants.status = 'active' AND grants.organization_id = ? AND grants.service_account_id = ? AND grants.operation_id = ? AND grants.permission_id = ?
ORDER BY grants.organization_id ASC`

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func activeServiceGrantOrganization(ctx context.Context, queryer rowQueryer, input GrantInput) (string, error) {
	var organizationID string
	err := queryer.QueryRowContext(ctx, `
SELECT accounts.organization_id
FROM service_accounts accounts
JOIN organizations organizations
  ON organizations.id = accounts.organization_id
 AND organizations.status = 'active'
JOIN service_operations operations
  ON operations.id = ?
 AND operations.permission_id = ?
 AND operations.required_scope IN ('project', 'object')
 AND operations.status = 'active'
JOIN permissions permissions
  ON permissions.id = operations.permission_id
 AND permissions.status = 'active'
WHERE accounts.id = ? AND accounts.status = 'active'`, input.OperationID, input.Permission, input.ServiceAccountID).Scan(&organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidGrantRequest
	}
	if err != nil {
		return "", fmt.Errorf("read service grant authority: %w", err)
	}
	return organizationID, nil
}

func serviceAccountIDFromPrincipal(principal identity.Principal) (string, bool) {
	if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodServiceToken || !canonicalID(principal.TenantID) || principal.UserID != "" || len(principal.Roles) != 0 {
		return "", false
	}
	serviceAccountID, ok := strings.CutPrefix(principal.Subject, "service-account/")
	return serviceAccountID, ok && canonicalID(serviceAccountID)
}

func validGrantInput(input GrantInput) bool {
	return canonicalID(input.ID) && canonicalID(input.ServiceAccountID) && canonicalID(input.OperationID) && canonicalID(input.Permission) && canonicalID(input.ProjectID)
}

func canonicalID(value string) bool { return value != "" && value == strings.TrimSpace(value) }

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
