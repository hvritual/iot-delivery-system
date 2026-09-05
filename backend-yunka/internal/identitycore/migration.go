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

const ServiceGrantMigrationID = "S0-03-06_service_operation_grants_v1"

// ProjectReadAuthorizationMigrationID is deliberately additive. Existing
// databases may already have recorded the authorization and service-grant
// schema migrations, so changing the permission dictionary alone must not
// leave them without the project-list read contract.
const ProjectReadAuthorizationMigrationID = "S0-04-07_project_read_authorization_v1"

// PlanningListAuthorizationMigrationID adds the project-scoped read
// permissions and service-operation rows for the three planning lists without
// rewriting an already-applied authorization dictionary migration.
const PlanningListAuthorizationMigrationID = "S0-04-08_planning_list_authorization_v1"

// ItemReadAuthorizationMigrationID installs the three canonical item-read
// service operations into databases whose original service dictionary
// migration has already been recorded.
const ItemReadAuthorizationMigrationID = "S0-05-09_item_read_authorization_v1"

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

const serviceGrantSchema = `
CREATE TABLE service_operations (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    permission_id TEXT NOT NULL CHECK (length(trim(permission_id)) > 0),
    required_scope TEXT NOT NULL CHECK (required_scope IN ('organization', 'project', 'object')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT
);
CREATE TABLE service_operation_grants (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    organization_id TEXT NOT NULL CHECK (length(trim(organization_id)) > 0),
    service_account_id TEXT NOT NULL CHECK (length(trim(service_account_id)) > 0),
    operation_id TEXT NOT NULL CHECK (length(trim(operation_id)) > 0),
    permission_id TEXT NOT NULL CHECK (length(trim(permission_id)) > 0),
    project_id TEXT NOT NULL CHECK (length(trim(project_id)) > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    revoked_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE RESTRICT,
    FOREIGN KEY (service_account_id) REFERENCES service_accounts(id) ON DELETE RESTRICT,
    FOREIGN KEY (operation_id) REFERENCES service_operations(id) ON DELETE RESTRICT,
    FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE RESTRICT,
    CHECK ((status = 'active' AND revoked_at IS NULL) OR (status = 'revoked' AND revoked_at IS NOT NULL))
);
CREATE UNIQUE INDEX service_operation_grants_active_unique
    ON service_operation_grants (service_account_id, operation_id, permission_id, project_id)
    WHERE status = 'active';
CREATE TRIGGER service_operation_grants_service_account_organization_on_insert
BEFORE INSERT ON service_operation_grants
WHEN NOT EXISTS (
    SELECT 1 FROM service_accounts
    WHERE id = NEW.service_account_id AND organization_id = NEW.organization_id
)
BEGIN
    SELECT RAISE(ABORT, 'service grant service account must belong to organization');
END;
CREATE TRIGGER service_operation_grants_operation_permission_on_insert
BEFORE INSERT ON service_operation_grants
WHEN NOT EXISTS (
    SELECT 1 FROM service_operations
    WHERE id = NEW.operation_id
      AND permission_id = NEW.permission_id
      AND required_scope IN ('project', 'object')
      AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'service grant must target an active project or object operation');
END;
CREATE TRIGGER service_operation_grants_active_permission_on_insert
BEFORE INSERT ON service_operation_grants
WHEN NOT EXISTS (
    SELECT 1 FROM permissions WHERE id = NEW.permission_id AND status = 'active'
)
BEGIN
    SELECT RAISE(ABORT, 'service grant permission must be active');
END;
CREATE TRIGGER service_operation_grants_immutable_tuple_on_update
BEFORE UPDATE OF id, organization_id, service_account_id, operation_id, permission_id, project_id ON service_operation_grants
BEGIN
    SELECT RAISE(ABORT, 'service grant tuple is immutable');
END;
CREATE TRIGGER service_operation_grants_one_way_revocation_on_update
BEFORE UPDATE ON service_operation_grants
WHEN NOT (
    OLD.status = 'active'
    AND OLD.revoked_at IS NULL
    AND NEW.status = 'revoked'
    AND NEW.revoked_at IS NOT NULL
)
BEGIN
    SELECT RAISE(ABORT, 'service grant can only transition once from active to revoked');
END;
CREATE TRIGGER service_operations_immutable_on_update
BEFORE UPDATE ON service_operations
BEGIN
    SELECT RAISE(ABORT, 'service operation dictionary is immutable');
END;
CREATE TRIGGER service_operations_immutable_on_delete
BEFORE DELETE ON service_operations
BEGIN
    SELECT RAISE(ABORT, 'service operation dictionary is immutable');
END;`

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	if database == nil {
		return errors.New("identity SQLite database is required")
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return fmt.Errorf("load authorization permission dictionary: %w", err)
	}
	projectRead, err := loadProjectReadAuthorizationDefinition(dictionary)
	if err != nil {
		return fmt.Errorf("load project read authorization migration definition: %w", err)
	}
	planningLists, err := loadPlanningListAuthorizationDefinitions(dictionary)
	if err != nil {
		return fmt.Errorf("load planning list authorization migration definitions: %w", err)
	}
	itemReads, err := loadItemReadAuthorizationDefinitions(dictionary)
	if err != nil {
		return fmt.Errorf("load item read authorization migration definitions: %w", err)
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
	var projectReadApplied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ProjectReadAuthorizationMigrationID).Scan(&projectReadApplied); err != nil {
		return fmt.Errorf("read project read authorization migration ledger: %w", err)
	}
	var planningListsApplied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, PlanningListAuthorizationMigrationID).Scan(&planningListsApplied); err != nil {
		return fmt.Errorf("read planning list authorization migration ledger: %w", err)
	}
	var itemReadsApplied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM iotd_schema_migrations WHERE migration_id = ?`, ItemReadAuthorizationMigrationID).Scan(&itemReadsApplied); err != nil {
		return fmt.Errorf("read item read authorization migration ledger: %w", err)
	}
	for _, migration := range []struct {
		id     string
		schema string
		name   string
	}{
		{id: MigrationID, schema: identitySchema, name: "identity core"},
		{id: ServiceCredentialMigrationID, schema: serviceCredentialSchema, name: "service credential"},
		{id: AuthorizationMigrationID, schema: authorizationSchema, name: "authorization dictionary"},
		{id: ServiceGrantMigrationID, schema: serviceGrantSchema, name: "service operation grant"},
	} {
		// service_operations has a foreign key to permissions. On an upgraded
		// database the authorization ledger can already be present while the
		// service-grant ledger is not; install and verify the permission half
		// before seeding the current operation dictionary in that case.
		if migration.id == ServiceGrantMigrationID {
			if err := ensureProjectReadAuthorization(ctx, tx, projectRead, projectReadApplied == 0); err != nil {
				return fmt.Errorf("ensure project read authorization before service operation migration: %w", err)
			}
			for _, definition := range planningLists {
				if err := ensureProjectReadAuthorization(ctx, tx, definition, planningListsApplied == 0); err != nil {
					return fmt.Errorf("ensure planning list authorization before service operation migration: %w", err)
				}
			}
		}
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
		if migration.id == ServiceGrantMigrationID {
			if err := seedServiceOperations(ctx, tx, dictionary); err != nil {
				return fmt.Errorf("seed service operations: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, migration.id); err != nil {
			return fmt.Errorf("record %s migration: %w", migration.name, err)
		}
	}
	// This is intentionally unconditional. A forged migration-ledger row must
	// not cause the durable permission, grants, or operation to be trusted.
	if err := ensureProjectReadAuthorization(ctx, tx, projectRead, projectReadApplied == 0); err != nil {
		return fmt.Errorf("ensure project read authorization migration: %w", err)
	}
	if err := ensureProjectReadServiceOperation(ctx, tx, projectRead, projectReadApplied == 0); err != nil {
		return fmt.Errorf("ensure project read service operation migration: %w", err)
	}
	if projectReadApplied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, ProjectReadAuthorizationMigrationID); err != nil {
			return fmt.Errorf("record project read authorization migration: %w", err)
		}
	}
	for _, definition := range planningLists {
		if err := ensureProjectReadAuthorization(ctx, tx, definition, planningListsApplied == 0); err != nil {
			return fmt.Errorf("ensure planning list authorization migration: %w", err)
		}
		if err := ensureProjectReadServiceOperation(ctx, tx, definition, planningListsApplied == 0); err != nil {
			return fmt.Errorf("ensure planning list service operation migration: %w", err)
		}
	}
	if planningListsApplied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, PlanningListAuthorizationMigrationID); err != nil {
			return fmt.Errorf("record planning list authorization migration: %w", err)
		}
	}
	for _, definition := range itemReads {
		if err := ensureProjectReadServiceOperation(ctx, tx, definition, itemReadsApplied == 0); err != nil {
			return fmt.Errorf("ensure item read service operation migration: %w", err)
		}
	}
	if itemReadsApplied == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO iotd_schema_migrations (migration_id, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, ItemReadAuthorizationMigrationID); err != nil {
			return fmt.Errorf("record item read authorization migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit identity migration: %w", err)
	}
	return nil
}

type projectReadAuthorizationDefinition struct {
	permission authorization.Permission
	operation  authorization.Operation
	roles      []authorization.Role
}

func loadProjectReadAuthorizationDefinition(dictionary authorization.Dictionary) (projectReadAuthorizationDefinition, error) {
	return loadProjectScopedReadAuthorizationDefinition(dictionary, "delivery.projects.read", "delivery.projects.list", "delivery.projects")
}

func loadPlanningListAuthorizationDefinitions(dictionary authorization.Dictionary) ([]projectReadAuthorizationDefinition, error) {
	specifications := []struct {
		permission string
		operation  string
		resource   string
	}{
		{permission: "delivery.releases.read", operation: "delivery.releases.list", resource: "delivery.releases"},
		{permission: "delivery.sprints.read", operation: "delivery.sprints.list", resource: "delivery.sprints"},
		{permission: "delivery.milestones.read", operation: "delivery.milestones.list", resource: "delivery.milestones"},
	}
	definitions := make([]projectReadAuthorizationDefinition, 0, len(specifications))
	for _, specification := range specifications {
		definition, err := loadProjectScopedReadAuthorizationDefinition(dictionary, specification.permission, specification.operation, specification.resource)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func loadItemReadAuthorizationDefinitions(dictionary authorization.Dictionary) ([]projectReadAuthorizationDefinition, error) {
	var permission authorization.Permission
	for _, candidate := range dictionary.Permissions {
		if candidate.ID == "delivery.work-items.read" {
			permission = candidate
			break
		}
	}
	if permission.ID == "" || permission.Resource != "delivery.work-items" || permission.Action != "read" ||
		permission.Status != "active" || !sameStrings(permission.AllowedScopes, []string{"project", "object"}) {
		return nil, errors.New("delivery.work-items.read must be active work-item read permission with project and object scopes")
	}
	want := map[string]string{
		"delivery.items.get":        "object",
		"delivery.items.search":     "project",
		"delivery.items.similarity": "project",
	}
	definitions := make([]projectReadAuthorizationDefinition, 0, len(want))
	for _, operation := range dictionary.Operations {
		requiredScope, ok := want[operation.ID]
		if !ok {
			continue
		}
		if operation.Permission != permission.ID || operation.RequiredScope != requiredScope {
			return nil, fmt.Errorf("item read operation %q has invalid dictionary semantics", operation.ID)
		}
		definitions = append(definitions, projectReadAuthorizationDefinition{permission: permission, operation: operation})
		delete(want, operation.ID)
	}
	if len(want) != 0 {
		return nil, fmt.Errorf("item read operation dictionary is incomplete: %v", want)
	}
	return definitions, nil
}

func loadProjectScopedReadAuthorizationDefinition(dictionary authorization.Dictionary, permissionID, operationID, resourceID string) (projectReadAuthorizationDefinition, error) {
	definition := projectReadAuthorizationDefinition{}
	for _, permission := range dictionary.Permissions {
		if permission.ID == permissionID {
			definition.permission = permission
			break
		}
	}
	if definition.permission.ID != permissionID || definition.permission.Resource != resourceID || definition.permission.Action != "read" || definition.permission.Status != "active" || !sameStrings(definition.permission.AllowedScopes, []string{"project"}) {
		return projectReadAuthorizationDefinition{}, fmt.Errorf("permission %q must be active %s/read with exactly project scope", permissionID, resourceID)
	}
	for _, operation := range dictionary.Operations {
		if operation.ID == operationID {
			definition.operation = operation
			break
		}
	}
	if definition.operation.ID != operationID || definition.operation.Permission != permissionID || definition.operation.RequiredScope != "project" {
		return projectReadAuthorizationDefinition{}, fmt.Errorf("operation %q must require %q at project scope", operationID, permissionID)
	}
	for _, role := range dictionary.Roles {
		if !isProjectReadBuiltInRole(role.ID) {
			continue
		}
		for _, grant := range role.Grants {
			if grant.Permission == permissionID && sameStrings(grant.AllowedScopes, []string{"project"}) {
				definition.roles = append(definition.roles, role)
				break
			}
		}
	}
	if len(definition.roles) != 6 {
		return projectReadAuthorizationDefinition{}, fmt.Errorf("all six built-in roles must grant %q at exactly project scope", permissionID)
	}
	return definition, nil
}

func isProjectReadBuiltInRole(roleID string) bool {
	switch roleID {
	case "system-administrator", "project-administrator", "release-approver", "contributor", "viewer", "auditor":
		return true
	default:
		return false
	}
}

func ensureProjectReadAuthorization(ctx context.Context, tx *sql.Tx, definition projectReadAuthorizationDefinition, allowInsert bool) error {
	permission := definition.permission
	var resource, action, status string
	err := tx.QueryRowContext(ctx, `SELECT resource, action, status FROM permissions WHERE id = ?`, permission.ID).Scan(&resource, &action, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if !allowInsert {
			return fmt.Errorf("permission %q is absent despite migration ledger", permission.ID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO permissions (id, resource, action, status) VALUES (?, ?, ?, ?)`, permission.ID, permission.Resource, permission.Action, permission.Status); err != nil {
			return fmt.Errorf("insert permission %q: %w", permission.ID, err)
		}
	} else if err != nil {
		return fmt.Errorf("read permission %q: %w", permission.ID, err)
	} else if resource != permission.Resource || action != permission.Action || status != permission.Status {
		return fmt.Errorf("permission %q conflicts with immutable dictionary row (%q, %q, %q)", permission.ID, resource, action, status)
	}
	if err := ensureExactScopes(ctx, tx, `SELECT scope_type FROM permission_allowed_scopes WHERE permission_id = ?`, `INSERT INTO permission_allowed_scopes (permission_id, scope_type) VALUES (?, ?)`, []any{permission.ID}, permission.ID, permission.AllowedScopes, allowInsert); err != nil {
		return fmt.Errorf("ensure permission %q scopes: %w", permission.ID, err)
	}
	for _, role := range definition.roles {
		var bindingScope string
		if err := tx.QueryRowContext(ctx, `SELECT binding_scope FROM roles WHERE id = ?`, role.ID).Scan(&bindingScope); err != nil {
			return fmt.Errorf("read built-in role %q: %w", role.ID, err)
		}
		if bindingScope != role.BindingScope {
			return fmt.Errorf("built-in role %q binding scope = %q, want %q", role.ID, bindingScope, role.BindingScope)
		}
		var grantCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_permission_grants WHERE role_id = ? AND permission_id = ?`, role.ID, permission.ID).Scan(&grantCount); err != nil {
			return fmt.Errorf("read role %q project read grant: %w", role.ID, err)
		}
		if grantCount == 0 {
			if !allowInsert {
				return fmt.Errorf("role %q project read grant is absent despite migration ledger", role.ID)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO role_permission_grants (role_id, permission_id) VALUES (?, ?)`, role.ID, permission.ID); err != nil {
				return fmt.Errorf("insert role %q project read grant: %w", role.ID, err)
			}
		}
		if err := ensureExactScopes(ctx, tx, `SELECT scope_type FROM role_permission_grant_allowed_scopes WHERE role_id = ? AND permission_id = ?`, `INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES (?, ?, ?)`, []any{role.ID, permission.ID}, role.ID+"/"+permission.ID, []string{"project"}, allowInsert); err != nil {
			return fmt.Errorf("ensure role %q project read grant scopes: %w", role.ID, err)
		}
	}
	return nil
}

func ensureExactScopes(ctx context.Context, tx *sql.Tx, selectSQL, insertSQL string, selectArgs []any, label string, want []string, allowInsert bool) error {
	rows, err := tx.QueryContext(ctx, selectSQL, selectArgs...)
	if err != nil {
		return fmt.Errorf("read %s scopes: %w", label, err)
	}
	got := make([]string, 0, len(want))
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan %s scope: %w", label, err)
		}
		got = append(got, scope)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s scopes: %w", label, err)
	}
	if !sameStrings(got, want) && len(got) != 0 {
		return fmt.Errorf("%s scopes = %v, want exactly %v", label, got, want)
	}
	if len(got) == 0 {
		if !allowInsert {
			return fmt.Errorf("%s scopes are absent despite migration ledger", label)
		}
		for _, scope := range want {
			args := append(append([]any{}, selectArgs...), scope)
			if _, err := tx.ExecContext(ctx, insertSQL, args...); err != nil {
				return fmt.Errorf("insert %s scope %q: %w", label, scope, err)
			}
		}
	}
	return nil
}

func ensureProjectReadServiceOperation(ctx context.Context, tx *sql.Tx, definition projectReadAuthorizationDefinition, allowInsert bool) error {
	operation := definition.operation
	var permissionID, requiredScope, status string
	err := tx.QueryRowContext(ctx, `SELECT permission_id, required_scope, status FROM service_operations WHERE id = ?`, operation.ID).Scan(&permissionID, &requiredScope, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if !allowInsert {
			return fmt.Errorf("service operation %q is absent despite migration ledger", operation.ID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_operations (id, permission_id, required_scope, status) VALUES (?, ?, ?, 'active')`, operation.ID, operation.Permission, operation.RequiredScope); err != nil {
			return fmt.Errorf("insert service operation %q: %w", operation.ID, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read service operation %q: %w", operation.ID, err)
	}
	if permissionID != operation.Permission || requiredScope != operation.RequiredScope || status != "active" {
		return fmt.Errorf("service operation %q conflicts with immutable dictionary row (%q, %q, %q)", operation.ID, permissionID, requiredScope, status)
	}
	return nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	if len(seen) != len(got) {
		return false
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}

func seedServiceOperations(ctx context.Context, tx *sql.Tx, dictionary authorization.Dictionary) error {
	for _, operation := range dictionary.Operations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_operations (id, permission_id, required_scope, status) VALUES (?, ?, ?, 'active')`, operation.ID, operation.Permission, operation.RequiredScope); err != nil {
			return fmt.Errorf("insert service operation %q: %w", operation.ID, err)
		}
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
