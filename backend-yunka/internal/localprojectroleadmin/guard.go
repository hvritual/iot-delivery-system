package localprojectroleadmin

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

var (
	ErrAuthorizationDatabaseRequired = errors.New("project role authorization database is required")
	ErrAuthorizationDenied           = errors.New("project role administration denied")
)

var projectRoleOrganizationKey = authz.MustScopeKey[string]("identity.project-role-admin.organization")

func denied() error {
	return errors.Join(authz.Denied(authz.Decision{Reason: authz.ReasonPermissionDenied}), ErrAuthorizationDenied)
}

type OperationGuard struct {
	database *sql.DB
}

func NewOperationGuard(database *sql.DB) (*OperationGuard, error) {
	if database == nil {
		return nil, ErrAuthorizationDatabaseRequired
	}
	return &OperationGuard{database: database}, nil
}

func (guard *OperationGuard) GuardResolver() authz.GuardResolver {
	if guard == nil {
		return nil
	}
	return projectRoleGuardResolver{guard: guard}
}

type projectRoleGuardResolver struct{ guard *OperationGuard }

func (resolver projectRoleGuardResolver) ResolveGuard(operation authz.OperationID) (authz.OperationGuard, bool) {
	if resolver.guard == nil || !isProjectRoleOperation(string(operation)) {
		return nil, false
	}
	return resolver.guard, true
}

func (guard *OperationGuard) Prepare(ctx context.Context, authorized authz.AuthorizedOperation, input any) (context.Context, error) {
	if guard == nil || guard.database == nil || !validOperationInput(string(authorized.Policy.Operation), input) {
		return nil, denied()
	}
	principal := authorized.Principal
	if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || !canonicalIdentifier(principal.TenantID) || !canonicalIdentifier(principal.UserID) {
		return nil, denied()
	}
	if !authorized.Decision.Allowed || authorized.Decision.Operation != authorized.Policy.Operation ||
		!authorized.Policy.TenantRequired || !sameStrings(authorized.Policy.Authentication, []string{"jwt"}) ||
		authorized.Policy.Mode != authz.PermissionAll || !samePermissionKeys(authorized.Policy.Permissions, []authz.PermissionKey{PermissionManageRoleBindings}) ||
		!samePermissionKeys(authorized.Decision.Permissions, []authz.PermissionKey{PermissionManageRoleBindings}) {
		return nil, denied()
	}
	wantScope := "organization:" + principal.TenantID
	hasSystemAdministrator := false
	for _, grant := range authorized.Decision.Grants {
		if grant.Permission != authz.PermissionKey(PermissionManageRoleBindings) {
			return nil, denied()
		}
		if grant.RoleID == "system-administrator" && grant.Scope == wantScope {
			hasSystemAdministrator = true
		}
	}
	if !hasSystemAdministrator {
		return nil, denied()
	}
	var found int
	err := guard.database.QueryRowContext(ctx, activeSystemAdministratorGrantQuery,
		principal.UserID, principal.TenantID, principal.UserID, principal.UserID, PermissionManageRoleBindings,
	).Scan(&found)
	if err != nil || found != 1 {
		return nil, denied()
	}
	return projectRoleOrganizationKey.With(ctx, principal.TenantID), nil
}

const activeSystemAdministratorGrantQuery = `
SELECT 1
FROM organizations
JOIN users
  ON users.organization_id = organizations.id
 AND users.id = ?
 AND users.status = 'active'
WHERE organizations.id = ?
  AND organizations.status = 'active'
  AND EXISTS (
    SELECT 1
    FROM role_bindings binding
    JOIN role_permission_grants role_grant
      ON role_grant.role_id = binding.role_id
    JOIN permissions permission
      ON permission.id = role_grant.permission_id
     AND permission.status = 'active'
    JOIN role_permission_grant_allowed_scopes allowed
      ON allowed.role_id = role_grant.role_id
     AND allowed.permission_id = role_grant.permission_id
     AND allowed.scope_type = 'project'
    WHERE binding.organization_id = organizations.id
      AND binding.role_id = 'system-administrator'
      AND binding.scope_type = 'organization'
      AND binding.scope_id = organizations.id
      AND binding.status = 'active'
      AND role_grant.permission_id = ?5
      AND (
        binding.user_id = ?3
        OR EXISTS (
          SELECT 1
          FROM teams
          JOIN team_memberships membership
            ON membership.team_id = teams.id
           AND membership.organization_id = teams.organization_id
          WHERE teams.id = binding.team_id
            AND teams.organization_id = organizations.id
            AND teams.status = 'active'
            AND membership.user_id = ?4
        )
      )
  )
LIMIT 1`

func isProjectRoleOperation(operation string) bool {
	return operation == OperationAssignProjectRole || operation == OperationRevokeProjectRole
}

func validOperationInput(operation string, input any) bool {
	switch operation {
	case OperationAssignProjectRole:
		value, ok := input.(*AssignInput)
		return ok && value != nil && canonicalIdentifier(value.ProjectID) && canonicalIdentifier(value.UserID) && canonicalIdentifier(value.RoleID)
	case OperationRevokeProjectRole:
		value, ok := input.(*RevokeInput)
		return ok && value != nil && canonicalIdentifier(value.BindingID) && value.ExpectedRevision > 0
	default:
		return false
	}
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	if len(seen) != len(left) {
		return false
	}
	for _, value := range right {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

func samePermissionKeys(left, right []authz.PermissionKey) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[authz.PermissionKey]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	if len(seen) != len(left) {
		return false
	}
	for _, value := range right {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}
