// Package humanauthz resolves durable human authorization grants. It does not
// wire grants into the runtime: OperationGuard owns resource interpretation.
package humanauthz

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
)

var (
	// ErrDatabaseRequired indicates a resolver cannot be constructed without
	// its application-owned authorization store.
	ErrDatabaseRequired = errors.New("human grant resolver database is required")

	// ErrGrantResolution identifies an operational failure without encoding
	// principal or permission values in an error message.
	ErrGrantResolution = errors.New("human grant resolution failed")
)

// Resolver projects active human RoleBinding records into Yunka grants.
// It intentionally leaves resource ownership and scope inheritance to the
// OperationGuard introduced by the following task.
type Resolver struct {
	database *sql.DB
}

var _ authz.GrantResolver = (*Resolver)(nil)

// NewGrantResolver constructs a read-only durable human grant resolver.
func NewGrantResolver(database *sql.DB) (*Resolver, error) {
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	return &Resolver{database: database}, nil
}

// ResolveGrants returns only requested permissions held by an active JWT human
// principal. Principal.Roles is deliberately not read: it is a compatibility
// snapshot, not an authorization source.
func (resolver *Resolver) ResolveGrants(ctx context.Context, request authz.GrantRequest) ([]authz.Grant, error) {
	if !isHumanJWTPrincipal(request.Principal) {
		return nil, nil
	}
	requested, valid := requestedPermissions(request.Permissions)
	if !valid {
		return nil, nil
	}
	if ctx == nil {
		return nil, ErrGrantResolution
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(ErrGrantResolution, err)
	}
	if resolver == nil || resolver.database == nil {
		return nil, ErrGrantResolution
	}

	rows, err := resolver.database.QueryContext(ctx, activeHumanGrantsQuery, request.Principal.UserID, request.Principal.TenantID, request.Principal.UserID, request.Principal.UserID)
	if err != nil {
		return nil, errors.Join(ErrGrantResolution, err)
	}
	defer rows.Close()

	grants := make([]authz.Grant, 0, len(requested))
	seen := make(map[grantKey]struct{}, len(requested))
	for rows.Next() {
		var permission, roleID, scopeType, scopeID string
		if err := rows.Scan(&permission, &roleID, &scopeType, &scopeID); err != nil {
			return nil, errors.Join(ErrGrantResolution, err)
		}
		if _, wanted := requested[authz.PermissionKey(permission)]; !wanted {
			continue
		}
		scope, ok := canonicalBindingScope(request.Principal.TenantID, scopeType, scopeID)
		if !ok || !canonicalIdentifier(roleID) {
			continue
		}
		grant := authz.Grant{Permission: authz.PermissionKey(permission), RoleID: roleID, Scope: scope}
		key := grantKey{permission: grant.Permission, roleID: grant.RoleID, scope: grant.Scope}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(ErrGrantResolution, err)
	}
	slices.SortFunc(grants, compareGrant)
	return grants, nil
}

const activeHumanGrantsQuery = `
SELECT DISTINCT permissions.id, role_bindings.role_id, role_bindings.scope_type, role_bindings.scope_id
FROM organizations
JOIN users
  ON users.organization_id = organizations.id
 AND users.id = ?
 AND users.status = 'active'
JOIN role_bindings
  ON role_bindings.organization_id = organizations.id
 AND role_bindings.status = 'active'
JOIN role_permission_grants
  ON role_permission_grants.role_id = role_bindings.role_id
JOIN permissions
  ON permissions.id = role_permission_grants.permission_id
 AND permissions.status = 'active'
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
  )
  AND EXISTS (
    SELECT 1
    FROM role_permission_grant_allowed_scopes
    WHERE role_permission_grant_allowed_scopes.role_id = role_permission_grants.role_id
      AND role_permission_grant_allowed_scopes.permission_id = role_permission_grants.permission_id
  )`

type grantKey struct {
	permission authz.PermissionKey
	roleID     string
	scope      string
}

func isHumanJWTPrincipal(principal identity.Principal) bool {
	return principal.Authenticated &&
		principal.AuthMethod == identity.AuthMethodJWT &&
		canonicalIdentifier(principal.TenantID) &&
		canonicalIdentifier(principal.UserID)
}

func requestedPermissions(permissions []authz.PermissionKey) (map[authz.PermissionKey]struct{}, bool) {
	if len(permissions) == 0 {
		return nil, false
	}
	requested := make(map[authz.PermissionKey]struct{}, len(permissions))
	for _, permission := range permissions {
		if !canonicalIdentifier(string(permission)) {
			return nil, false
		}
		requested[permission] = struct{}{}
	}
	return requested, true
}

func canonicalBindingScope(tenantID, scopeType, scopeID string) (string, bool) {
	if !canonicalIdentifier(scopeID) {
		return "", false
	}
	switch scopeType {
	case "organization":
		if scopeID != tenantID {
			return "", false
		}
		return "organization:" + scopeID, true
	case "project":
		return "project:" + scopeID, true
	default:
		return "", false
	}
}

func canonicalIdentifier(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func compareGrant(left, right authz.Grant) int {
	return cmp.Or(
		cmp.Compare(string(left.Permission), string(right.Permission)),
		cmp.Compare(left.RoleID, right.RoleID),
		cmp.Compare(left.Scope, right.Scope),
	)
}
