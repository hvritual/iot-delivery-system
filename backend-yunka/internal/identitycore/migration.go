package identitycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
)

const MigrationID = "S0-02-01_identity_core_v1"

const ServiceCredentialMigrationID = "S0-02-07_service_credentials_v1"

const AuthorizationMigrationID = "S0-03-02_authorization_dictionary_v1"

const identitySchema = `
CREATE TABLE organizations (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    slug TEXT NOT NULL UNIQUE CHECK (length(trim(slug)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE users (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) > 0),
    email TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    UNIQUE (organization_id, id)
);
CREATE TABLE external_identities (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    user_id TEXT NOT NULL CHECK (length(trim(user_id)) > 0),
    issuer TEXT NOT NULL CHECK (length(trim(issuer)) > 0),
    subject TEXT NOT NULL CHECK (length(trim(subject)) > 0),
    email_snapshot TEXT,
    display_name_snapshot TEXT,
    last_seen_at TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT,
    UNIQUE (issuer, subject)
);
CREATE TABLE service_accounts (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    UNIQUE (organization_id, name)
);`

const serviceCredentialSchema = `
CREATE TABLE service_account_credentials (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    service_account_id TEXT NOT NULL CHECK (length(trim(service_account_id)) > 0),
    credential_hash BLOB NOT NULL UNIQUE CHECK (length(credential_hash) = 32),
    expires_at TEXT NOT NULL CHECK (length(trim(expires_at)) > 0),
    revoked_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (service_account_id) REFERENCES service_accounts(id) ON DELETE RESTRICT
);
CREATE INDEX service_account_credentials_active_lookup
    ON service_account_credentials (service_account_id, expires_at, revoked_at);`

const authorizationSchema = `
CREATE TABLE teams (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('organization', 'project')),
    scope_id TEXT NOT NULL CHECK (length(trim(scope_id)) > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    UNIQUE (organization_id, id),
    CHECK ((scope_type = 'organization' AND scope_id = organization_id) OR (scope_type = 'project' AND length(trim(scope_id)) > 0))
);
CREATE TABLE team_memberships (
    team_id TEXT NOT NULL CHECK (length(trim(team_id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    user_id TEXT NOT NULL CHECK (length(trim(user_id)) > 0),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (organization_id, team_id) REFERENCES teams(organization_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT
);
CREATE TABLE roles (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    binding_scope TEXT NOT NULL CHECK (binding_scope IN ('organization', 'project'))
);
CREATE TABLE permissions (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    resource TEXT NOT NULL CHECK (length(trim(resource)) > 0),
    action TEXT NOT NULL CHECK (length(trim(action)) > 0),
    status TEXT NOT NULL CHECK (status IN ('active', 'reserved'))
);
CREATE TABLE permission_allowed_scopes (
    permission_id TEXT NOT NULL CHECK (length(trim(permission_id)) > 0),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('organization', 'project', 'object')),
    PRIMARY KEY (permission_id, scope_type),
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT
);
CREATE TABLE role_permission_grants (
    role_id TEXT NOT NULL CHECK (length(trim(role_id)) > 0),
    permission_id TEXT NOT NULL CHECK (length(trim(permission_id)) > 0),
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT
);
CREATE TABLE role_permission_grant_allowed_scopes (
    role_id TEXT NOT NULL CHECK (length(trim(role_id)) > 0),
    permission_id TEXT NOT NULL CHECK (length(trim(permission_id)) > 0),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('organization', 'project', 'object')),
    PRIMARY KEY (role_id, permission_id, scope_type),
    FOREIGN KEY (role_id, permission_id) REFERENCES role_permission_grants(role_id, permission_id) ON DELETE RESTRICT,
    FOREIGN KEY (permission_id, scope_type) REFERENCES permission_allowed_scopes(permission_id, scope_type) ON DELETE RESTRICT
);
CREATE TABLE role_bindings (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    role_id TEXT NOT NULL CHECK (length(trim(role_id)) > 0),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('organization', 'project')),
    scope_id TEXT NOT NULL CHECK (length(trim(scope_id)) > 0),
    user_id TEXT,
    team_id TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, user_id) REFERENCES users(organization_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (organization_id, team_id) REFERENCES teams(organization_id, id) ON DELETE RESTRICT,
    CHECK ((user_id IS NOT NULL AND length(trim(user_id)) > 0 AND team_id IS NULL) OR (user_id IS NULL AND team_id IS NOT NULL AND length(trim(team_id)) > 0)),
    CHECK ((scope_type = 'organization' AND scope_id = organization_id) OR (scope_type = 'project' AND length(trim(scope_id)) > 0))
);
CREATE UNIQUE INDEX role_bindings_active_user_unique
    ON role_bindings (organization_id, role_id, scope_type, scope_id, user_id)
    WHERE status = 'active' AND user_id IS NOT NULL;
CREATE UNIQUE INDEX role_bindings_active_team_unique
    ON role_bindings (organization_id, role_id, scope_type, scope_id, team_id)
    WHERE status = 'active' AND team_id IS NOT NULL;
CREATE TRIGGER role_bindings_match_role_scope_on_insert
BEFORE INSERT ON role_bindings
WHEN (SELECT binding_scope FROM roles WHERE id = NEW.role_id) <> NEW.scope_type
BEGIN
    SELECT RAISE(ABORT, 'role binding scope must match role binding scope');
END;
CREATE TRIGGER role_bindings_match_role_scope_on_update
BEFORE UPDATE OF role_id, scope_type ON role_bindings
WHEN (SELECT binding_scope FROM roles WHERE id = NEW.role_id) <> NEW.scope_type
BEGIN
    SELECT RAISE(ABORT, 'role binding scope must match role binding scope');
END;
CREATE TRIGGER role_bindings_project_team_scope_on_insert
BEFORE INSERT ON role_bindings
WHEN NEW.team_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM teams
    WHERE id = NEW.team_id
      AND organization_id = NEW.organization_id
      AND scope_type = 'project'
      AND (NEW.scope_type <> 'project' OR NEW.scope_id <> scope_id)
)
BEGIN
    SELECT RAISE(ABORT, 'project-scoped team role binding must use the same project scope');
END;
CREATE TRIGGER role_bindings_project_team_scope_on_update
BEFORE UPDATE OF organization_id, scope_type, scope_id, team_id, status ON role_bindings
WHEN NEW.team_id IS NOT NULL AND EXISTS (
    SELECT 1 FROM teams
    WHERE id = NEW.team_id
      AND organization_id = NEW.organization_id
      AND scope_type = 'project'
      AND (NEW.scope_type <> 'project' OR NEW.scope_id <> scope_id)
)
BEGIN
    SELECT RAISE(ABORT, 'project-scoped team role binding must use the same project scope');
END;
CREATE TRIGGER teams_project_scope_update_preserves_active_role_bindings
BEFORE UPDATE OF scope_type, scope_id ON teams
WHEN NEW.scope_type = 'project' AND EXISTS (
    SELECT 1 FROM role_bindings
    WHERE team_id = OLD.id
      AND organization_id = OLD.organization_id
      AND status = 'active'
      AND (scope_type <> 'project' OR scope_id <> NEW.scope_id)
)
BEGIN
    SELECT RAISE(ABORT, 'team scope update would exceed an active role binding scope');
END;`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("identity SQLite database is required")
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return fmt.Errorf("load authorization permission dictionary: %w", err)
	}
	if _, err := database.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable identity foreign keys: %w", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin identity migration transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS iotd_schema_migrations (migration_id TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("create identity migration ledger: %w", err)
	}
	for _, migration := range []struct {
		id     string
		schema string
		name   string
	}{
		{id: MigrationID, schema: identitySchema, name: "identity core"},
		{id: ServiceCredentialMigrationID, schema: serviceCredentialSchema, name: "service credential"},
		{id: AuthorizationMigrationID, schema: authorizationSchema, name: "authorization dictionary"},
	} {
		var applied int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, migration.id).Scan(&applied); err != nil {
			return fmt.Errorf("read %s migration ledger: %w", migration.name, err)
		}
		if applied != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, migration.schema); err != nil {
			return fmt.Errorf("apply %s schema: %w", migration.name, err)
		}
		if migration.id == AuthorizationMigrationID {
			if err := seedAuthorizationDictionary(ctx, tx, dictionary); err != nil {
				return fmt.Errorf("seed authorization dictionary: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, migration.id); err != nil {
			return fmt.Errorf("record %s migration: %w", migration.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit identity migration: %w", err)
	}
	return nil
}

func seedAuthorizationDictionary(ctx context.Context, tx *sql.Tx, dictionary authorization.Dictionary) error {
	for _, role := range dictionary.Roles {
		if _, err := tx.ExecContext(ctx, `INSERT INTO roles (id, binding_scope) VALUES (?, ?)`, role.ID, role.BindingScope); err != nil {
			return fmt.Errorf("insert role %q: %w", role.ID, err)
		}
	}
	for _, permission := range dictionary.Permissions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO permissions (id, resource, action, status) VALUES (?, ?, ?, ?)`, permission.ID, permission.Resource, permission.Action, permission.Status); err != nil {
			return fmt.Errorf("insert permission %q: %w", permission.ID, err)
		}
		for _, scope := range permission.AllowedScopes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES (?, ?)`, permission.ID, scope); err != nil {
				return fmt.Errorf("insert permission %q allowed scope %q: %w", permission.ID, scope, err)
			}
		}
	}
	for _, role := range dictionary.Roles {
		for _, grant := range role.Grants {
			if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grants (role_id, permission_id) VALUES (?, ?)`, role.ID, grant.Permission); err != nil {
				return fmt.Errorf("insert role %q permission %q grant: %w", role.ID, grant.Permission, err)
			}
			for _, scope := range grant.AllowedScopes {
				if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES (?, ?, ?)`, role.ID, grant.Permission, scope); err != nil {
					return fmt.Errorf("insert role %q permission %q allowed scope %q: %w", role.ID, grant.Permission, scope, err)
				}
			}
		}
	}
	return nil
}
