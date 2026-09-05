package localmemberadmin

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

var (
	ErrAuthorizationDatabaseRequired = errors.New("local member admin authorization database is required")
	ErrAuthorizationDenied           = errors.New("local member admin authorization denied")
)

var memberOrganizationKey = authz.MustScopeKey[string]("identity.member-admin.organization")

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
	return memberGuardResolver{guard: guard}
}

type memberGuardResolver struct{ guard *OperationGuard }

func (resolver memberGuardResolver) ResolveGuard(operation authz.OperationID) (authz.OperationGuard, bool) {
	if resolver.guard == nil || !isMemberAdminOperation(string(operation)) {
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
		authorized.Policy.Mode != authz.PermissionAll || !samePermissionKeys(authorized.Policy.Permissions, []authz.PermissionKey{authz.PermissionKey(PermissionManageUsers)}) ||
		!samePermissionKeys(authorized.Decision.Permissions, []authz.PermissionKey{authz.PermissionKey(PermissionManageUsers)}) {
		return nil, denied()
	}
	if len(authorized.Decision.Grants) == 0 {
		return nil, denied()
	}
	wantScope := "organization:" + principal.TenantID
	for _, grant := range authorized.Decision.Grants {
		if grant.Permission != authz.PermissionKey(PermissionManageUsers) || grant.RoleID != "system-administrator" || grant.Scope != wantScope {
			return nil, denied()
		}
	}
	var found int
	err := guard.database.QueryRowContext(ctx, activeSystemAdministratorGrantQuery,
		principal.UserID,
		principal.TenantID,
		principal.UserID,
		PermissionManageUsers,
	).Scan(&found)
	if err != nil || found != 1 {
		return nil, denied()
	}
	return memberOrganizationKey.With(ctx, principal.TenantID), nil
}

func OrganizationIDFromContext(ctx context.Context) string {
	value, ok := memberOrganizationKey.From(ctx)
	if !ok {
		return ""
	}
	return value
}

func isMemberAdminOperation(operation string) bool {
	switch operation {
	case OperationCreateMember, OperationDisableMember, OperationResetCredential:
		return true
	default:
		return false
	}
}

func validOperationInput(operation string, input any) bool {
	switch operation {
	case OperationCreateMember:
		_, ok := input.(*CreateInput)
		return ok
	case OperationDisableMember:
		_, ok := input.(*DisableInput)
		return ok
	case OperationResetCredential:
		_, ok := input.(*ResetCredentialInput)
		return ok
	default:
		return false
	}
}

func samePermissionKeys(left, right []authz.PermissionKey) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[authz.PermissionKey]struct{}, len(left))
	for _, value := range left {
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; !ok {
			return false
		}
	}
	return true
}

func canonicalIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255
}

const activeSystemAdministratorGrantQuery = `
SELECT 1
FROM organizations
JOIN users
  ON users.organization_id = organizations.id
 AND users.id = ?
 AND users.status = 'active'
JOIN role_bindings
  ON role_bindings.organization_id = organizations.id
 AND role_bindings.role_id = 'system-administrator'
 AND role_bindings.scope_type = 'organization'
 AND role_bindings.scope_id = organizations.id
 AND role_bindings.status = 'active'
JOIN role_permission_grants
  ON role_permission_grants.role_id = role_bindings.role_id
 AND role_permission_grants.permission_id = ?
JOIN permissions
  ON permissions.id = role_permission_grants.permission_id
 AND permissions.status = 'active'
JOIN role_permission_grant_allowed_scopes
  ON role_permission_grant_allowed_scopes.role_id = role_permission_grants.role_id
 AND role_permission_grant_allowed_scopes.permission_id = role_permission_grants.permission_id
 AND role_permission_grant_allowed_scopes.scope_type = 'organization'
WHERE organizations.id = ?
  AND organizations.status = 'active'
  AND (
    role_bindings.user_id = ?
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
