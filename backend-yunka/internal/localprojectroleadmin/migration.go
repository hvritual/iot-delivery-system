package localprojectroleadmin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
)

const MigrationID = "YU-24_project_role_binding_admin_v1"

const (
	bindingRevisionAbort     = "project role binding revocation requires revision CAS"
	bindingReactivationAbort = "revoked project role binding cannot be reactivated"
	bindingIdentityAbort     = "project role binding identity is immutable"
	bindingRevisionOnlyAbort = "project role binding revision may change only on revocation"
)

const bindingRevisionTrigger = `
CREATE TRIGGER IF NOT EXISTS role_bindings_require_revision_on_disable
BEFORE UPDATE ON role_bindings
WHEN OLD.status = 'active'
 AND NEW.status = 'disabled'
 AND NEW.revision <> OLD.revision + 1
BEGIN
    SELECT RAISE(ABORT, 'project role binding revocation requires revision CAS');
END;`

const bindingReactivationTrigger = `
CREATE TRIGGER IF NOT EXISTS role_bindings_prevent_reactivation
BEFORE UPDATE OF status ON role_bindings
WHEN OLD.status = 'disabled' AND NEW.status <> 'disabled'
BEGIN
    SELECT RAISE(ABORT, 'revoked project role binding cannot be reactivated');
END;`

const bindingRevisionOnlyTrigger = `
CREATE TRIGGER IF NOT EXISTS role_bindings_revision_only_on_revocation
BEFORE UPDATE OF revision ON role_bindings
WHEN OLD.status = NEW.status AND NEW.revision <> OLD.revision
BEGIN
    SELECT RAISE(ABORT, 'project role binding revision may change only on revocation');
END;`

const bindingIdentityTrigger = `
CREATE TRIGGER IF NOT EXISTS role_bindings_identity_immutable
BEFORE UPDATE OF id, organization_id, role_id, scope_type, scope_id, user_id, team_id, created_at ON role_bindings
WHEN NEW.id <> OLD.id
  OR NEW.organization_id <> OLD.organization_id
  OR NEW.role_id <> OLD.role_id
  OR NEW.scope_type <> OLD.scope_type
  OR NEW.scope_id <> OLD.scope_id
  OR NOT (NEW.user_id IS OLD.user_id)
  OR NOT (NEW.team_id IS OLD.team_id)
  OR NEW.created_at <> OLD.created_at
BEGIN
    SELECT RAISE(ABORT, 'project role binding identity is immutable');
END;`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("project role administration SQLite database is required")
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return fmt.Errorf("load project role authorization dictionary: %w", err)
	}
	permission, grantRoles, err := projectRoleAuthorizationContract(dictionary)
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable project role foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin project role migration: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{
		"organizations", "users", "roles", "permissions", "permission_allowed_scopes",
		"role_permission_grants", "role_permission_grant_allowed_scopes", "role_bindings",
		"teams", "team_memberships", "iotd_schema_migrations",
	} {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect project role dependency %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("project role migration requires %s", table)
		}
	}
	if err := ensureRoleBindingRevision(ctx, tx); err != nil {
		return err
	}
	if err := ensureManagePermission(ctx, tx, permission); err != nil {
		return err
	}
	if err := ensureCanonicalRoleGrants(ctx, tx, grantRoles); err != nil {
		return err
	}
	for name, statement := range map[string]string{
		"revocation revision": bindingRevisionTrigger,
		"reactivation":        bindingReactivationTrigger,
		"revision mutation":   bindingRevisionOnlyTrigger,
		"identity mutation":   bindingIdentityTrigger,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install project role %s trigger: %w", name, err)
		}
	}
	if err := verifyBindingTriggers(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read project role migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record project role migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("project role migration ledger is invalid")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project role migration: %w", err)
	}
	return nil
}

func projectRoleAuthorizationContract(dictionary authorization.Dictionary) (authorization.Permission, []string, error) {
	var permission authorization.Permission
	for _, candidate := range dictionary.Permissions {
		if candidate.ID == PermissionManageRoleBindings {
			permission = candidate
			break
		}
	}
	if permission.ID != PermissionManageRoleBindings || permission.Resource != "identity.role-bindings" || permission.Action != "manage" || permission.Status != "active" || !sameStrings(permission.AllowedScopes, []string{"project"}) {
		return authorization.Permission{}, nil, errors.New("identity.role-bindings.manage must be an active project-scoped identity.role-bindings/manage permission")
	}
	grantRoles := make([]string, 0, 2)
	for _, role := range dictionary.Roles {
		for _, grant := range role.Grants {
			if grant.Permission == PermissionManageRoleBindings {
				if !sameStrings(grant.AllowedScopes, []string{"project"}) {
					return authorization.Permission{}, nil, fmt.Errorf("role %s has an invalid identity.role-bindings.manage scope", role.ID)
				}
				grantRoles = append(grantRoles, role.ID)
			}
		}
	}
	sort.Strings(grantRoles)
	if !sameStrings(grantRoles, []string{"project-administrator", "system-administrator"}) {
		return authorization.Permission{}, nil, errors.New("identity.role-bindings.manage canonical role grants are invalid")
	}
	return permission, grantRoles, nil
}

func ensureRoleBindingRevision(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('role_bindings')`)
	if err != nil {
		return fmt.Errorf("inspect role binding revision: %w", err)
	}
	hasRevision := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan role binding revision: %w", err)
		}
		if name == "revision" {
			hasRevision = true
			if strings.ToUpper(typeName) != "INTEGER" || notNull != 1 || fmt.Sprint(defaultValue) != "1" {
				_ = rows.Close()
				return errors.New("role binding revision column has an invalid contract")
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate role binding revision: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close role binding revision inspection: %w", err)
	}
	if !hasRevision {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE role_bindings ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`); err != nil {
			return fmt.Errorf("add role binding revision: %w", err)
		}
	}
	var tableSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'role_bindings'`).Scan(&tableSQL); err != nil {
		return fmt.Errorf("read role binding schema after revision migration: %w", err)
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(tableSQL), " "))
	if !strings.Contains(normalized, "revision integer not null default 1 check (revision >= 1)") {
		return errors.New("role binding revision physical constraint is missing")
	}
	return nil
}

func ensureManagePermission(ctx context.Context, tx *sql.Tx, permission authorization.Permission) error {
	var resource, action, status string
	err := tx.QueryRowContext(ctx, `SELECT resource, action, status FROM permissions WHERE id = ?`, permission.ID).Scan(&resource, &action, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permissions (id, resource, action, status) VALUES (?, ?, ?, 'active')`, permission.ID, permission.Resource, permission.Action); err != nil {
			return fmt.Errorf("insert project role management permission: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read project role management permission: %w", err)
	} else {
		if resource != permission.Resource || action != permission.Action || (status != "reserved" && status != "active") {
			return errors.New("project role management permission conflicts with the canonical dictionary")
		}
		if status == "reserved" {
			if _, err := tx.ExecContext(ctx, `UPDATE permissions SET status = 'active' WHERE id = ? AND status = 'reserved'`, permission.ID); err != nil {
				return fmt.Errorf("activate project role management permission: %w", err)
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = ? ORDER BY scope_type`, permission.ID)
	if err != nil {
		return fmt.Errorf("read project role management permission scopes: %w", err)
	}
	var scopes []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan project role management permission scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close project role management permission scopes: %w", err)
	}
	if len(scopes) == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES (?, 'project')`, permission.ID); err != nil {
			return fmt.Errorf("insert project role management permission scope: %w", err)
		}
	} else if !sameStrings(scopes, []string{"project"}) {
		return errors.New("project role management permission scopes conflict with the canonical dictionary")
	}
	return nil
}

func ensureCanonicalRoleGrants(ctx context.Context, tx *sql.Tx, canonicalRoles []string) error {
	rows, err := tx.QueryContext(ctx, `SELECT role_id FROM role_permission_grants WHERE permission_id = ? ORDER BY role_id`, PermissionManageRoleBindings)
	if err != nil {
		return fmt.Errorf("read project role management grants: %w", err)
	}
	var current []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan project role management grant: %w", err)
		}
		current = append(current, role)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close project role management grants: %w", err)
	}
	if len(current) == 0 {
		for _, role := range canonicalRoles {
			if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grants (role_id, permission_id) VALUES (?, ?)`, role, PermissionManageRoleBindings); err != nil {
				return fmt.Errorf("insert project role management grant for %s: %w", role, err)
			}
		}
	} else if !sameStrings(current, canonicalRoles) {
		return errors.New("project role management role grants conflict with the canonical dictionary")
	}
	for _, role := range canonicalRoles {
		var bindingScope string
		if err := tx.QueryRowContext(ctx, `SELECT binding_scope FROM roles WHERE id = ?`, role).Scan(&bindingScope); err != nil {
			return fmt.Errorf("read project role management role %s: %w", role, err)
		}
		if role == "system-administrator" && bindingScope != "organization" || role == "project-administrator" && bindingScope != "project" {
			return errors.New("project role management canonical role binding scope is invalid")
		}
		var scopes []string
		scopeRows, err := tx.QueryContext(ctx, `SELECT scope_type FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = ? ORDER BY scope_type`, role, PermissionManageRoleBindings)
		if err != nil {
			return fmt.Errorf("read project role management grant scopes for %s: %w", role, err)
		}
		for scopeRows.Next() {
			var scope string
			if err := scopeRows.Scan(&scope); err != nil {
				_ = scopeRows.Close()
				return fmt.Errorf("scan project role management grant scope for %s: %w", role, err)
			}
			scopes = append(scopes, scope)
		}
		if err := scopeRows.Close(); err != nil {
			return fmt.Errorf("close project role management grant scopes for %s: %w", role, err)
		}
		if len(scopes) == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES (?, ?, 'project')`, role, PermissionManageRoleBindings); err != nil {
				return fmt.Errorf("insert project role management grant scope for %s: %w", role, err)
			}
		} else if !sameStrings(scopes, []string{"project"}) {
			return errors.New("project role management grant scopes conflict with the canonical dictionary")
		}
	}
	return nil
}

func verifyBindingTriggers(ctx context.Context, tx *sql.Tx) error {
	required := map[string][]string{
		"role_bindings_require_revision_on_disable": {
			"old.status = 'active'", "new.status = 'disabled'", "new.revision <> old.revision + 1",
			"raise(abort, 'project role binding revocation requires revision cas')",
		},
		"role_bindings_prevent_reactivation": {
			"old.status = 'disabled'", "new.status <> 'disabled'",
			"raise(abort, 'revoked project role binding cannot be reactivated')",
		},
		"role_bindings_revision_only_on_revocation": {
			"old.status = new.status", "new.revision <> old.revision",
			"raise(abort, 'project role binding revision may change only on revocation')",
		},
		"role_bindings_identity_immutable": {
			"before update of id, organization_id, role_id, scope_type, scope_id, user_id, team_id, created_at on role_bindings",
			"raise(abort, 'project role binding identity is immutable')",
		},
	}
	for name, fragments := range required {
		var definition string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, name).Scan(&definition); err != nil {
			return fmt.Errorf("project role trigger %s is missing", name)
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(definition), " "))
		for _, fragment := range fragments {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("project role trigger %s is invalid", name)
			}
		}
	}
	return nil
}
