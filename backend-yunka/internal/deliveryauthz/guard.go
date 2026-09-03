// Package deliveryauthz interprets durable delivery resource ownership at the
// operation boundary. It never trusts client-provided organization identifiers.
package deliveryauthz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
)

var (
	ErrLookupRequired   = errors.New("delivery authorization lookup is required")
	ErrDatabaseRequired = errors.New("delivery authorization database is required")
	ErrDenied           = errors.New("delivery authorization denied")
)

type resourceLookup interface {
	Get(context.Context, string) (delivery.WorkItem, error)
	GetProject(context.Context, string) (delivery.Project, error)
	ListProjects(context.Context) ([]delivery.Project, error)
}

type OperationGuard struct {
	lookup     resourceLookup
	database   *sql.DB
	operations map[authz.OperationID]authorization.Operation
}

var authorizedProjectsKey = authz.MustScopeKey[map[string]bool]("delivery.authorized-projects")
var organizationKey = authz.MustScopeKey[string]("delivery.authorized-organization")

func NewOperationGuard(lookup resourceLookup, database *sql.DB) (*OperationGuard, error) {
	if lookup == nil {
		return nil, ErrLookupRequired
	}
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return nil, fmt.Errorf("load permission dictionary: %w", err)
	}
	return NewOperationGuardWithDictionary(lookup, database, dictionary)
}

// NewOperationGuardWithDictionary constructs an OperationGuard from a validated
// dictionary. Production uses NewOperationGuard, which loads the embedded
// versioned dictionary.
func NewOperationGuardWithDictionary(lookup resourceLookup, database *sql.DB, dictionary authorization.Dictionary) (*OperationGuard, error) {
	if lookup == nil {
		return nil, ErrLookupRequired
	}
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	permissions := make(map[string]authorization.Permission, len(dictionary.Permissions))
	for _, permission := range dictionary.Permissions {
		if !canonicalID(permission.ID) || permission.ID != strings.TrimSpace(permission.ID) {
			return nil, errors.New("delivery authorization dictionary permission is incomplete")
		}
		if _, exists := permissions[permission.ID]; exists {
			return nil, errors.New("delivery authorization dictionary permission is duplicated")
		}
		permissions[permission.ID] = permission
	}
	operations := make(map[authz.OperationID]authorization.Operation, len(dictionary.Operations))
	for _, operation := range dictionary.Operations {
		if !canonicalID(operation.ID) || !canonicalID(operation.Permission) || !supportedScope(operation.RequiredScope) {
			return nil, errors.New("delivery authorization dictionary operation is incomplete")
		}
		permission, exists := permissions[operation.Permission]
		if !exists || !containsScope(permission.AllowedScopes, operation.RequiredScope) {
			return nil, errors.New("delivery authorization dictionary operation references an unsupported permission scope")
		}
		if _, exists := operations[authz.OperationID(operation.ID)]; exists {
			return nil, errors.New("delivery authorization dictionary operation is duplicated")
		}
		operations[authz.OperationID(operation.ID)] = operation
	}
	return &OperationGuard{lookup: lookup, database: database, operations: operations}, nil
}

func canonicalID(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

func supportedScope(scope string) bool {
	return scope == "organization" || scope == "project" || scope == "object"
}

func containsScope(scopes []string, expected string) bool {
	for _, scope := range scopes {
		if scope == expected {
			return true
		}
	}
	return false
}

func (guard *OperationGuard) GuardResolver() authz.GuardResolver {
	if guard == nil {
		return nil
	}
	return operationGuardResolver{guard: guard}
}

type operationGuardResolver struct{ guard *OperationGuard }

func (resolver operationGuardResolver) ResolveGuard(authz.OperationID) (authz.OperationGuard, bool) {
	return resolver.guard, resolver.guard != nil
}

func (guard *OperationGuard) Prepare(ctx context.Context, authorized authz.AuthorizedOperation, input any) (context.Context, error) {
	if guard == nil || guard.lookup == nil || guard.database == nil {
		return nil, ErrDenied
	}
	if !validInput(authorized.Policy.Operation, input) {
		return nil, ErrDenied
	}
	operation, ok := guard.operations[authorized.Policy.Operation]
	if !ok || !authorized.Decision.Allowed ||
		authorized.Decision.Operation != authorized.Policy.Operation ||
		operation.Permission != string(singlePermission(authorized.Policy)) ||
		!singlePermissionMatches(authorized.Decision.Permissions, operation.Permission) {
		return nil, ErrDenied
	}
	tenantID := authorized.Principal.TenantID
	if tenantID == "" || tenantID != strings.TrimSpace(tenantID) {
		return nil, ErrDenied
	}
	grants, err := guard.verifyGrants(ctx, authorized.Principal, authorized.Decision.Grants, operation)
	if err != nil {
		return nil, err
	}
	secured := organizationKey.With(ctx, tenantID)
	if operation.RequiredScope == "organization" {
		if !guard.allowsOrganization(grants, operation, tenantID) {
			return nil, ErrDenied
		}
		if authorized.Policy.Operation == "delivery.dashboard.get" {
			projects, err := guard.organizationProjects(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			return authorizedProjectsKey.With(secured, projects), nil
		}
		return secured, nil
	}
	if authorized.Policy.Operation == "delivery.items.list" {
		projects, err := guard.allowedProjects(ctx, grants, operation, tenantID)
		if err != nil {
			return nil, err
		}
		return authorizedProjectsKey.With(secured, projects), nil
	}
	projectID, objectID, err := guard.projectAndObjectID(ctx, authorized.Policy.Operation, input)
	if err != nil {
		return nil, err
	}
	project, err := guard.ownedProject(ctx, projectID, tenantID)
	if err != nil {
		return nil, err
	}
	if !guard.allowsProject(grants, operation, tenantID, project.ID, objectID) {
		return nil, ErrDenied
	}
	return secured, nil
}

func singlePermission(policy authz.Policy) authz.PermissionKey {
	if len(policy.Permissions) != 1 {
		return ""
	}
	return policy.Permissions[0]
}

func singlePermissionMatches(permissions []authz.PermissionKey, expected string) bool {
	return len(permissions) == 1 && string(permissions[0]) == expected
}

func (guard *OperationGuard) verifyGrants(ctx context.Context, principal identity.Principal, grants []authz.Grant, operation authorization.Operation) ([]authz.Grant, error) {
	if serviceAccountID, ok := serviceAccountIDFromPrincipal(principal); ok {
		return guard.verifyServiceGrants(ctx, principal, serviceAccountID, grants, operation)
	}
	if !isHumanJWTPrincipal(principal) {
		return nil, ErrDenied
	}
	if len(grants) == 0 {
		return nil, ErrDenied
	}
	verified := make([]authz.Grant, 0, len(grants))
	seen := make(map[authz.Grant]struct{}, len(grants))
	for _, grant := range grants {
		scopeType, scopeID, ok := bindingScope(grant.Scope, principal.TenantID)
		if !ok || string(grant.Permission) != operation.Permission || !canonicalID(grant.RoleID) {
			return nil, ErrDenied
		}
		if _, exists := seen[grant]; exists {
			return nil, ErrDenied
		}
		seen[grant] = struct{}{}
		var found int
		err := guard.database.QueryRowContext(ctx, activeBindingGrantQuery, principal.UserID, grant.RoleID, scopeType, scopeID, operation.Permission, operation.RequiredScope, principal.TenantID, principal.UserID).Scan(&found)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("verify delivery grant: %w", err)
		}
		if err != nil || found != 1 {
			return nil, ErrDenied
		}
		verified = append(verified, grant)
	}
	return verified, nil
}

func (guard *OperationGuard) verifyServiceGrants(ctx context.Context, principal identity.Principal, serviceAccountID string, grants []authz.Grant, operation authorization.Operation) ([]authz.Grant, error) {
	if len(grants) == 0 {
		return nil, ErrDenied
	}
	verified := make([]authz.Grant, 0, len(grants))
	seen := make(map[authz.Grant]struct{}, len(grants))
	for _, grant := range grants {
		projectID, ok := serviceProjectScope(grant.Scope)
		if !ok || string(grant.Permission) != operation.Permission || grant.RoleID != "service-account:"+serviceAccountID {
			return nil, ErrDenied
		}
		if _, exists := seen[grant]; exists {
			return nil, ErrDenied
		}
		seen[grant] = struct{}{}
		var found int
		err := guard.database.QueryRowContext(ctx, activeServiceGrantQuery, principal.TenantID, serviceAccountID, operation.ID, operation.Permission, projectID).Scan(&found)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("verify delivery service grant: %w", err)
		}
		if err != nil || found != 1 {
			return nil, ErrDenied
		}
		verified = append(verified, grant)
	}
	return verified, nil
}

const activeBindingGrantQuery = `
SELECT 1
FROM organizations
JOIN users
  ON users.organization_id = organizations.id
 AND users.id = ?
 AND users.status = 'active'
JOIN role_bindings
  ON role_bindings.organization_id = organizations.id
 AND role_bindings.status = 'active'
 AND role_bindings.role_id = ?
 AND role_bindings.scope_type = ?
 AND role_bindings.scope_id = ?
JOIN role_permission_grants
  ON role_permission_grants.role_id = role_bindings.role_id
 AND role_permission_grants.permission_id = ?
JOIN permissions
  ON permissions.id = role_permission_grants.permission_id
 AND permissions.status = 'active'
JOIN role_permission_grant_allowed_scopes
  ON role_permission_grant_allowed_scopes.role_id = role_permission_grants.role_id
 AND role_permission_grant_allowed_scopes.permission_id = role_permission_grants.permission_id
 AND role_permission_grant_allowed_scopes.scope_type = ?
WHERE organizations.id = ?
  AND organizations.status = 'active'
  AND (
    role_bindings.user_id = users.id
    OR EXISTS (
      SELECT 1
      FROM teams
      JOIN team_memberships
        ON team_memberships.team_id = teams.id
       AND team_memberships.organization_id = teams.organization_id
      WHERE teams.id = role_bindings.team_id
        AND teams.organization_id = organizations.id
        AND teams.status = 'active'
        AND team_memberships.user_id = ?
    )
  )`

const activeServiceGrantQuery = `
SELECT 1
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
  AND grants.project_id = ?`

func isHumanJWTPrincipal(principal identity.Principal) bool {
	return principal.Authenticated && principal.AuthMethod == identity.AuthMethodJWT && canonicalID(principal.UserID)
}

func serviceAccountIDFromPrincipal(principal identity.Principal) (string, bool) {
	if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodServiceToken || principal.UserID != "" || len(principal.Roles) != 0 {
		return "", false
	}
	serviceAccountID, ok := strings.CutPrefix(principal.Subject, "service-account/")
	return serviceAccountID, ok && canonicalID(serviceAccountID)
}

func serviceProjectScope(scope string) (string, bool) {
	projectID, ok := strings.CutPrefix(scope, "project:")
	return projectID, ok && canonicalID(projectID)
}

func bindingScope(scope, tenantID string) (string, string, bool) {
	parts := strings.Split(scope, ":")
	if len(parts) != 2 {
		return "", "", false
	}
	switch parts[0] {
	case "organization":
		return "organization", parts[1], parts[1] == tenantID && canonicalID(parts[1])
	case "project":
		return "project", parts[1], canonicalID(parts[1])
	default:
		return "", "", false
	}
}

func (guard *OperationGuard) organizationProjects(ctx context.Context, tenantID string) (map[string]bool, error) {
	projects, err := guard.lookup.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list delivery projects: %w", err)
	}
	allowed := make(map[string]bool)
	for _, project := range projects {
		if project.OrganizationID == tenantID {
			allowed[project.ID] = true
		}
	}
	return allowed, nil
}

func (guard *OperationGuard) allowedProjects(ctx context.Context, grants []authz.Grant, operation authorization.Operation, tenantID string) (map[string]bool, error) {
	projects, err := guard.organizationProjects(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for id := range projects {
		if !guard.allowsProject(grants, operation, tenantID, id, "") {
			delete(projects, id)
		}
	}
	return projects, nil
}

func (guard *OperationGuard) ownedProject(ctx context.Context, projectID, tenantID string) (delivery.Project, error) {
	project, err := guard.lookup.GetProject(ctx, projectID)
	if err != nil || project.OrganizationID == "" || project.OrganizationID != tenantID {
		return delivery.Project{}, ErrDenied
	}
	return project, nil
}

func (guard *OperationGuard) allowsOrganization(grants []authz.Grant, operation authorization.Operation, tenantID string) bool {
	for _, grant := range grants {
		if string(grant.Permission) == operation.Permission && grant.Scope == "organization:"+tenantID {
			return true
		}
	}
	return false
}

func (guard *OperationGuard) allowsProject(grants []authz.Grant, operation authorization.Operation, tenantID, projectID, objectID string) bool {
	for _, grant := range grants {
		if string(grant.Permission) != operation.Permission {
			continue
		}
		switch grant.Scope {
		case "organization:" + tenantID:
			return true
		case "project:" + projectID:
			return true
		case "object:work-item:" + objectID:
			return objectID != ""
		}
	}
	return false
}

func (guard *OperationGuard) projectAndObjectID(ctx context.Context, operation authz.OperationID, input any) (string, string, error) {
	projectID := ""
	objectID := ""
	switch request := input.(type) {
	case *deliveryv1.CreateItemRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateReleaseRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateSprintRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateMilestoneRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.UpdateItemRequest:
		objectID = request.GetId()
	case *deliveryv1.CreateItemCommentRequest:
		objectID = request.GetId()
	case *deliveryv1.UpdateItemContextRequest:
		objectID = request.GetId()
	case *deliveryv1.AdvanceGateRequest:
		objectID = request.GetId()
	case *deliveryv1.CloseItemRequest:
		objectID = request.GetId()
	default:
		return "", "", ErrDenied
	}
	if objectID != "" {
		projectID, err := guard.itemProjectID(ctx, objectID)
		return projectID, objectID, err
	}
	if strings.TrimSpace(projectID) == "" {
		return "", "", ErrDenied
	}
	return projectID, "", nil
}

func validInput(operation authz.OperationID, input any) bool {
	switch operation {
	case "delivery.dashboard.get":
		request, ok := input.(*deliveryv1.GetDashboardRequest)
		return ok && request != nil
	case "delivery.items.list":
		request, ok := input.(*deliveryv1.ListItemsRequest)
		return ok && request != nil
	case "delivery.projects.create":
		request, ok := input.(*deliveryv1.CreateProjectRequest)
		return ok && request != nil
	case "delivery.items.create":
		request, ok := input.(*deliveryv1.CreateItemRequest)
		return ok && request != nil
	case "delivery.items.update":
		request, ok := input.(*deliveryv1.UpdateItemRequest)
		return ok && request != nil
	case "delivery.items.comment.create":
		request, ok := input.(*deliveryv1.CreateItemCommentRequest)
		return ok && request != nil
	case "delivery.items.update-context":
		request, ok := input.(*deliveryv1.UpdateItemContextRequest)
		return ok && request != nil
	case "delivery.items.advance-gate":
		request, ok := input.(*deliveryv1.AdvanceGateRequest)
		return ok && request != nil
	case "delivery.items.close":
		request, ok := input.(*deliveryv1.CloseItemRequest)
		return ok && request != nil
	case "delivery.releases.create":
		request, ok := input.(*deliveryv1.CreateReleaseRequest)
		return ok && request != nil
	case "delivery.sprints.create":
		request, ok := input.(*deliveryv1.CreateSprintRequest)
		return ok && request != nil
	case "delivery.milestones.create":
		request, ok := input.(*deliveryv1.CreateMilestoneRequest)
		return ok && request != nil
	default:
		return false
	}
}

func (guard *OperationGuard) itemProjectID(ctx context.Context, itemID string) (string, error) {
	if strings.TrimSpace(itemID) == "" {
		return "", ErrDenied
	}
	item, err := guard.lookup.Get(ctx, itemID)
	if err != nil || strings.TrimSpace(item.ProjectID) == "" {
		return "", ErrDenied
	}
	return item.ProjectID, nil
}

func AuthorizedProjectsFromContext(ctx context.Context) (map[string]bool, bool) {
	return authorizedProjectsKey.From(ctx)
}
func OrganizationIDFromContext(ctx context.Context) string {
	value, _ := organizationKey.From(ctx)
	return value
}
