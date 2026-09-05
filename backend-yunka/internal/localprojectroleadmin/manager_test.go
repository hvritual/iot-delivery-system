package localprojectroleadmin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/identitycore"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type roleFixture struct {
	repository *delivery.SQLiteRepository
	manager    *Manager
	resolver   *humanauthz.Resolver
	outbox     *localoutbox.SQLiteStore
	adminCtx   context.Context
	projectCtx context.Context
}

func newRoleFixture(t *testing.T) *roleFixture {
	t.Helper()
	repository, err := delivery.NewSQLiteRepository(filepath.Join(t.TempDir(), "yu24-role.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	database := repository.Database()
	if err := identitycore.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := audit.ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	auditStore, err := audit.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	outboxStore, err := localoutbox.NewSQLiteStore(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-a', 'org-a', 'Org A', 'active')`,
		`INSERT INTO organizations (id, slug, name, status) VALUES ('org-b', 'org-b', 'Org B', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('admin-a', 'org-a', 'Admin A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('target-a', 'org-a', 'Target A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('project-admin-a', 'org-a', 'Project Admin A', 'active')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('disabled-a', 'org-a', 'Disabled A', 'disabled')`,
		`INSERT INTO users (id, organization_id, display_name, status) VALUES ('user-b', 'org-b', 'User B', 'active')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('system-admin-binding-a', 'org-a', 'system-administrator', 'organization', 'org-a', 'admin-a', 'active')`,
		`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('project-admin-binding-a', 'org-a', 'project-administrator', 'project', 'project-a', 'project-admin-a', 'active')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	for _, project := range []delivery.Project{
		{ID: "project-a", OrganizationID: "org-a", Name: "Project A", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now},
		{ID: "project-b", OrganizationID: "org-b", Name: "Project B", Board: delivery.BoardResearchDelivery, Owner: "owner", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repository.CreateProject(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	resolver, err := humanauthz.NewGrantResolver(database)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(resolver)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := NewOperationGuard(database)
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guard.GuardResolver())
	if err != nil {
		t.Fatal(err)
	}
	executor := operation.NewExecutorWithOptions(security, operation.ExecutorOptions{Transactions: localtx.NewSQLiteFactory(database)})
	sequence := 0
	manager, err := NewManager(database, repository, auditStore, outboxStore, executor,
		WithClock(func() time.Time { return now }),
		WithIDGenerator(func() (string, error) {
			sequence++
			return fmt.Sprintf("yu24-%04d", sequence), nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	admin := identity.Principal{Subject: "local-user/admin-a", TenantID: "org-a", UserID: "admin-a", AuthMethod: identity.AuthMethodJWT, Authenticated: true}
	projectAdmin := identity.Principal{Subject: "local-user/project-admin-a", TenantID: "org-a", UserID: "project-admin-a", Roles: []string{"system-administrator"}, AuthMethod: identity.AuthMethodJWT, Authenticated: true}
	return &roleFixture{
		repository: repository, manager: manager, resolver: resolver, outbox: outboxStore,
		adminCtx: identity.WithPrincipal(t.Context(), admin),
		projectCtx: identity.WithPrincipal(t.Context(), projectAdmin),
	}
}

func TestYU24AssignAndRevokeChangesDurableGrantOnNextRequest(t *testing.T) {
	fixture := newRoleFixture(t)
	assigned, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "contributor"})
	if err != nil {
		t.Fatal(err)
	}
	if assigned.Status != "active" || assigned.Revision != 1 || assigned.OrganizationID != "org-a" || assigned.ProjectID != "project-a" || assigned.UserID != "target-a" || assigned.RoleID != "contributor" {
		t.Fatalf("assigned=%#v", assigned)
	}
	if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "contributor"}); !errors.Is(err, ErrBindingAlreadyActive) {
		t.Fatalf("duplicate assignment error=%v", err)
	}
	target := identity.Principal{Subject: "local-user/target-a", TenantID: "org-a", UserID: "target-a", AuthMethod: identity.AuthMethodJWT, Authenticated: true}
	grants := resolveGrants(t, fixture.resolver, target, "delivery.work-items.create")
	if len(grants) != 1 || grants[0].RoleID != "contributor" || grants[0].Scope != "project:project-a" {
		t.Fatalf("target grants after assignment=%#v", grants)
	}
	if _, err := fixture.manager.Revoke(fixture.adminCtx, RevokeInput{BindingID: assigned.BindingID, ExpectedRevision: 9}); !errors.Is(err, ErrBindingRevisionConflict) {
		t.Fatalf("stale revoke error=%v", err)
	}
	revoked, err := fixture.manager.Revoke(fixture.adminCtx, RevokeInput{BindingID: assigned.BindingID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Status != "disabled" || revoked.Revision != 2 {
		t.Fatalf("revoked=%#v", revoked)
	}
	if grants := resolveGrants(t, fixture.resolver, target, "delivery.work-items.create"); len(grants) != 0 {
		t.Fatalf("target retained grant after revoke=%#v", grants)
	}
	if _, err := fixture.manager.Revoke(fixture.adminCtx, RevokeInput{BindingID: assigned.BindingID, ExpectedRevision: 1}); !errors.Is(err, ErrBindingRevoked) {
		t.Fatalf("second revoke error=%v", err)
	}
	reassigned, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "contributor"})
	if err != nil {
		t.Fatal(err)
	}
	if reassigned.BindingID == assigned.BindingID || reassigned.Revision != 1 {
		t.Fatalf("reassignment reused revoked binding: old=%#v new=%#v", assigned, reassigned)
	}
	var oldStatus string
	var oldRevision int64
	if err := fixture.repository.Database().QueryRow(`SELECT status, revision FROM role_bindings WHERE id = ?`, assigned.BindingID).Scan(&oldStatus, &oldRevision); err != nil || oldStatus != "disabled" || oldRevision != 2 {
		t.Fatalf("old binding status=%s revision=%d error=%v", oldStatus, oldRevision, err)
	}
	var auditCount int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE operation IN (?, ?)`, OperationAssignProjectRole, OperationRevokeProjectRole).Scan(&auditCount); err != nil || auditCount != 3 {
		t.Fatalf("project role audit count=%d error=%v", auditCount, err)
	}
	snapshot, err := fixture.outbox.Snapshot(t.Context())
	if err != nil || snapshot.Pending != 3 {
		t.Fatalf("project role outbox=%#v error=%v", snapshot, err)
	}
}

func TestYU24RejectsCrossTenantDisabledAndOrganizationScopedRoleTargets(t *testing.T) {
	fixture := newRoleFixture(t)
	for _, scenario := range []struct {
		name  string
		input AssignInput
		want  error
	}{
		{name: "cross-tenant project", input: AssignInput{ProjectID: "project-b", UserID: "target-a", RoleID: "viewer"}, want: ErrProjectNotFound},
		{name: "cross-tenant user", input: AssignInput{ProjectID: "project-a", UserID: "user-b", RoleID: "viewer"}, want: ErrMemberNotFound},
		{name: "disabled user", input: AssignInput{ProjectID: "project-a", UserID: "disabled-a", RoleID: "viewer"}, want: ErrMemberDisabled},
		{name: "organization role", input: AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "system-administrator"}, want: ErrRoleNotAssignable},
		{name: "unknown role", input: AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "made-up-admin"}, want: ErrRoleNotAssignable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if _, err := fixture.manager.Assign(fixture.adminCtx, scenario.input); !errors.Is(err, scenario.want) {
				t.Fatalf("assignment error=%v want=%v", err, scenario.want)
			}
		})
	}
	var active int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM role_bindings WHERE user_id = 'target-a' AND scope_type = 'project' AND status = 'active'`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("rejected assignments created bindings=%d error=%v", active, err)
	}
}

func TestYU24ProjectAdministratorCannotElevateDespitePermissionGrantOrForgedRoles(t *testing.T) {
	fixture := newRoleFixture(t)
	_, err := fixture.manager.Assign(fixture.projectCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"})
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("project administrator elevation error=%v", err)
	}
	var active int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM role_bindings WHERE user_id = 'target-a' AND scope_type = 'project' AND status = 'active'`).Scan(&active); err != nil || active != 0 {
		t.Fatalf("project administrator elevation created bindings=%d error=%v", active, err)
	}
}

func TestYU24RoleContractDriftFailsClosed(t *testing.T) {
	fixture := newRoleFixture(t)
	database := fixture.repository.Database()
	if _, err := database.Exec(`INSERT INTO role_permission_grants (role_id, permission_id) VALUES ('contributor', 'identity.users.manage')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO role_permission_grant_allowed_scopes (role_id, permission_id, scope_type) VALUES ('contributor', 'identity.users.manage', 'organization')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "contributor"}); !errors.Is(err, ErrRoleContractDrift) {
		t.Fatalf("drifted role assignment error=%v", err)
	}
}

func TestYU24AuditOrOutboxFailureRollsBackRoleBindingState(t *testing.T) {
	t.Run("assign audit failure", func(t *testing.T) {
		fixture := newRoleFixture(t)
		if _, err := fixture.repository.Database().Exec(`CREATE TRIGGER yu24_fail_assign_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.project-role-bindings.assign'
BEGIN SELECT RAISE(ABORT, 'forced YU24 audit failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"}); err == nil {
			t.Fatal("forced audit failure committed assignment")
		}
		assertNoActiveTuple(t, fixture, "project-a", "target-a", "viewer")
		if snapshot, err := fixture.outbox.Snapshot(t.Context()); err != nil || snapshot.Pending != 0 {
			t.Fatalf("audit failure outbox=%#v error=%v", snapshot, err)
		}
	})

	t.Run("assign outbox failure", func(t *testing.T) {
		fixture := newRoleFixture(t)
		if _, err := fixture.repository.Database().Exec(`CREATE TRIGGER yu24_fail_outbox BEFORE INSERT ON iotd_outbox BEGIN SELECT RAISE(ABORT, 'forced YU24 outbox failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"}); err == nil {
			t.Fatal("forced outbox failure committed assignment")
		}
		assertNoActiveTuple(t, fixture, "project-a", "target-a", "viewer")
		var auditCount int
		if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM iotd_audit_entries WHERE operation = ?`, OperationAssignProjectRole).Scan(&auditCount); err != nil || auditCount != 0 {
			t.Fatalf("outbox failure retained audit count=%d error=%v", auditCount, err)
		}
	})

	t.Run("revoke audit failure", func(t *testing.T) {
		fixture := newRoleFixture(t)
		assigned, err := fixture.manager.Assign(fixture.adminCtx, AssignInput{ProjectID: "project-a", UserID: "target-a", RoleID: "viewer"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.repository.Database().Exec(`CREATE TRIGGER yu24_fail_revoke_audit BEFORE INSERT ON iotd_audit_entries
WHEN NEW.operation = 'identity.project-role-bindings.revoke'
BEGIN SELECT RAISE(ABORT, 'forced YU24 revoke audit failure'); END`); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.manager.Revoke(fixture.adminCtx, RevokeInput{BindingID: assigned.BindingID, ExpectedRevision: 1}); err == nil {
			t.Fatal("forced revoke audit failure committed revocation")
		}
		var status string
		var revision int64
		if err := fixture.repository.Database().QueryRow(`SELECT status, revision FROM role_bindings WHERE id = ?`, assigned.BindingID).Scan(&status, &revision); err != nil || status != "active" || revision != 1 {
			t.Fatalf("revoke audit failure state=%s rev=%d error=%v", status, revision, err)
		}
	})
}

func resolveGrants(t *testing.T, resolver *humanauthz.Resolver, principal identity.Principal, permission authz.PermissionKey) []authz.Grant {
	t.Helper()
	grants, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{Principal: principal, Permissions: []authz.PermissionKey{permission}})
	if err != nil {
		t.Fatal(err)
	}
	return grants
}

func assertNoActiveTuple(t *testing.T, fixture *roleFixture, projectID, userID, roleID string) {
	t.Helper()
	var active int
	if err := fixture.repository.Database().QueryRow(`SELECT COUNT(*) FROM role_bindings WHERE organization_id = 'org-a' AND scope_type = 'project' AND scope_id = ? AND user_id = ? AND role_id = ? AND status = 'active'`, projectID, userID, roleID).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active role binding count=%d error=%v", active, err)
	}
}
