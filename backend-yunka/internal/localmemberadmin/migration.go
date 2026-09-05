// Package localmemberadmin owns administrator-driven local member lifecycle operations.
package localmemberadmin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
)

const MigrationID = "YU-20_local_member_admin_v1"

const PermissionManageUsers = "identity.users.manage"

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("local member admin SQLite database is required")
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return fmt.Errorf("load local member admin authorization dictionary: %w", err)
	}
	permission, err := memberAdminAuthorizationContract(dictionary)
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable local member admin foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin local member admin migration: %w", err)
	}
	defer tx.Rollback()
	for _, table := range []string{"users", "roles", "permissions", "permission_allowed_scopes", "role_permission_grants", "role_permission_grant_allowed_scopes", "iotd_local_user_credentials", "iotd_schema_migrations"} {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return fmt.Errorf("inspect local member admin dependency %s: %w", table, err)
		}
		if count != 1 {
			return fmt.Errorf("local member admin migration requires %s", table)
		}
	}
	if err := ensureUserRevision(ctx, tx); err != nil {
		return err
	}
	if err := ensurePermission(ctx, tx, permission); err != nil {
		return err
	}
	if err := ensureSystemAdministratorGrant(ctx, tx); err != nil {
		return err
	}
	var applied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, MigrationID).Scan(&applied); err != nil {
		return fmt.Errorf("read local member admin migration ledger: %w", err)
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, MigrationID); err != nil {
			return fmt.Errorf("record local member admin migration: %w", err)
		}
	} else if applied != 1 {
		return errors.New("local member admin migration ledger is invalid")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local member admin migration: %w", err)
	}
	return nil
}

func memberAdminAuthorizationContract(dictionary authorization.Dictionary) (authorization.Permission, error) {
	var permission authorization.Permission
	for _, candidate := range dictionary.Permissions {
		if candidate.ID == PermissionManageUsers {
			permission = candidate
			break
		}
	}
	if permission.ID != PermissionManageUsers || permission.Resource != "identity.users" || permission.Action != "manage" || permission.Status != "active" || !sameStrings(permission.AllowedScopes, []string{"organization"}) {
		return authorization.Permission{}, errors.New("identity.users.manage must be an active organization-scoped identity.users/manage permission")
	}
	return permission, nil
}

func ensureUserRevision(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info('users')`)
	if err != nil {
		return fmt.Errorf("inspect users revision column: %w", err)
	}
	hasRevision := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typeName string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan users revision column: %w", err)
		}
		if name == "revision" {
			hasRevision = true
			if strings.ToUpper(typeName) != "INTEGER" || notNull != 1 || fmt.Sprint(defaultValue) != "1" {
				_ = rows.Close()
				return errors.New("users revision column has an invalid contract")
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate users revision column: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close users revision inspection: %w", err)
	}
	if !hasRevision {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE users ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)`); err != nil {
			return fmt.Errorf("add users revision column: %w", err)
		}
	}
	var tableSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&tableSQL); err != nil {
		return fmt.Errorf("read users schema after revision migration: %w", err)
	}
	if !strings.Contains(strings.ToLower(tableSQL), "revision integer not null default 1 check (revision >= 1)") {
		return errors.New("users revision constraint is missing")
	}
	return nil
}

func ensurePermission(ctx context.Context, tx *sql.Tx, permission authorization.Permission) error {
	var resource, action, status string
	err := tx.QueryRowContext(ctx, `SELECT resource, action, status FROM permissions WHERE id = ?`, permission.ID).Scan(&resource, &action, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permissions (id, resource, action, status) VALUES (?, ?, ?, ?)`, permission.ID, permission.Resource, permission.Action, permission.Status); err != nil {
			return fmt.Errorf("insert local member admin permission: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read local member admin permission: %w", err)
	} else if resource != permission.Resource || action != permission.Action || status != permission.Status {
		return errors.New("local member admin permission conflicts with the canonical dictionary")
	}
	var scopes []string
	rows, err := tx.QueryContext(ctx, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = ? ORDER BY scope_type`, permission.ID)
	if err != nil {
		return fmt.Errorf("read local member admin permission scopes: %w", err)
	}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan local member admin permission scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close local member admin permission scopes: %w", err)
	}
	if len(scopes) == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES (?, 'organization')`, permission.ID); err != nil {
			return fmt.Errorf("insert local member admin permission scope: %w", err)
		}
	} else if !sameStrings(scopes, []string{"organization"}) {
		return errors.New("local member admin permission scopes conflict with the canonical dictionary")
	}
	return nil
}

func ensureSystemAdministratorGrant(ctx context.Context, tx *sql.Tx) error {
	var bindingScope string
	if err := tx.QueryRowContext(ctx, `SELECT binding_scope FROM roles WHERE id = 'system-administrator'`).Scan(&bindingScope); err != nil || bindingScope != "organization" {
		return errors.New("local member admin requires the organization-scoped system-administrator role")
	}
	rows, err := tx.QueryContext(ctx, `SELECT role_id FROM role_permission_grants WHERE permission_id = ? ORDER BY role_id`, PermissionManageUsers)
	if err != nil {
		return fmt.Errorf("read local member admin role grants: %w", err)
	}
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan local member admin role grant: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close local member admin role grants: %w", err)
	}
	if len(roles) == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grants (role_id, permission_id) VALUES ('system-administrator', ?)`, PermissionManageUsers); err != nil {
			return fmt.Errorf("insert system administrator member-management grant: %w", err)
		}
	} else if !sameStrings(roles, []string{"system-administrator"}) {
		return errors.New("identity.users.manage must be granted only to system-administrator")
	}
	var scopes []string
	scopeRows, err := tx.QueryContext(ctx, `SELECT scope_type FROM role_permission_grant_allowed_scopes WHERE role_id = 'system-administrator' AND permission_id = ? ORDER BY scope_type`, PermissionManageUsers)
	if err != nil {
		return fmt.Errorf("read system administrator member-management grant scopes: %w", err)
	}
	for scopeRows.Next() {
		var scope string
		if err := scopeRows.Scan(&scope); err != nil {
			_ = scopeRows.Close()
			return fmt.Errorf("scan system administrator member-management grant scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := scopeRows.Close(); err != nil {
		return fmt.Errorf("close system administrator member-management grant scopes: %w", err)
	}
	if len(scopes) == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES ('system-administrator', ?, 'organization')`, PermissionManageUsers); err != nil {
			return fmt.Errorf("insert system administrator member-management grant scope: %w", err)
		}
	} else if !sameStrings(scopes, []string{"organization"}) {
		return errors.New("system administrator member-management grant must be organization-scoped")
	}
	return nil
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
