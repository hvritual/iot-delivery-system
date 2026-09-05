package locallogin

import (
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU25AdministratorDisableInvalidatesOldSessionAndJWTOnNextRequest(t *testing.T) {
	fixture := newLoginFixture(t, false)
	userLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	adminManager, adminContext := newYU22AdminManager(t, fixture)
	if _, err := adminManager.Disable(adminContext, localmemberadmin.DisableInput{UserID: "user-a", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), userLogin.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("disabled user old JWT principal=%#v error=%v", principal, err)
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), userLogin.SessionToken); !errors.Is(err, ErrSessionInvalid) || session.SessionID != "" {
		t.Fatalf("disabled user old session=%#v error=%v", session, err)
	}
	if _, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), userLogin.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("disabled user session minted access token error=%v", err)
	}
	if _, err := fixture.manager.CurrentMemberFromSessionToken(t.Context(), userLogin.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("disabled user session resolved current member error=%v", err)
	}
	// The administrator is a second real durable account. Disabling user-a must
	// not convert the tenant into a global authentication outage.
	adminPrincipal, ok := identity.FromContext(adminContext)
	if !ok || !adminPrincipal.Authenticated || adminPrincipal.UserID != "admin-yu22" || adminPrincipal.TenantID != "org-a" {
		t.Fatalf("independent administrator principal=%#v present=%v", adminPrincipal, ok)
	}
}

func TestYU25AdministratorResetInvalidatesOldSessionAndJWTByCredentialRevision(t *testing.T) {
	fixture := newLoginFixture(t, false)
	oldLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	adminManager, adminContext := newYU22AdminManager(t, fixture)
	reset, err := adminManager.ResetCredential(adminContext, localmemberadmin.ResetCredentialInput{
		UserID: "user-a", ExpectedUserRevision: 1, ExpectedCredentialRevision: 1,
		Password: []byte("YU25-admin-reset-password"),
	})
	if err != nil || reset.CredentialRevision != 2 || reset.UserRevision != 2 {
		t.Fatalf("admin reset=%#v error=%v", reset, err)
	}
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), oldLogin.AccessToken); !errors.Is(err, ErrAccessTokenInvalid) || principal.Authenticated {
		t.Fatalf("reset user old JWT principal=%#v error=%v", principal, err)
	}
	if session, err := fixture.manager.VerifySessionToken(t.Context(), oldLogin.SessionToken); !errors.Is(err, ErrSessionInvalid) || session.SessionID != "" {
		t.Fatalf("reset user old session=%#v error=%v", session, err)
	}
	if _, err := fixture.manager.IssueAccessTokenFromSession(t.Context(), oldLogin.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("reset user old session minted access token error=%v", err)
	}
	if _, err := fixture.manager.CurrentMember(t.Context(), CurrentMemberInput{AccessToken: oldLogin.AccessToken}); !errors.Is(err, ErrAccessTokenInvalid) {
		t.Fatalf("reset user old JWT resolved current member error=%v", err)
	}
	// YU-20 reset does not need a second session mutation to achieve fail-closed
	// behavior. The stale row remains active, but its captured credential revision
	// no longer matches the durable credential and therefore cannot authenticate.
	var status string
	var sessionRevision, capturedCredentialRevision int64
	if err := fixture.database.QueryRow(`SELECT status, revision, credential_revision FROM iotd_local_sessions WHERE id = ?`, oldLogin.SessionID).Scan(&status, &sessionRevision, &capturedCredentialRevision); err != nil {
		t.Fatal(err)
	}
	if status != "active" || sessionRevision != 1 || capturedCredentialRevision != 1 {
		t.Fatalf("stale session row status=%s sessionRevision=%d credentialRevision=%d", status, sessionRevision, capturedCredentialRevision)
	}
	newLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU25-admin-reset-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if principal, err := fixture.manager.VerifyAccessToken(t.Context(), newLogin.AccessToken); err != nil || !principal.Authenticated || principal.UserID != "user-a" {
		t.Fatalf("new credential principal=%#v error=%v", principal, err)
	}
}

func TestYU25ProjectRoleRevocationRemovesGrantWithoutGloballyInvalidatingSession(t *testing.T) {
	fixture := newLoginFixture(t, false)
	if err := localprojectroleadmin.ApplyMigrations(t.Context(), fixture.database); err != nil {
		t.Fatal(err)
	}
	login, err := fixture.manager.Login(fixture.context(t), LoginInput{
		OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`INSERT INTO role_bindings
(id, organization_id, role_id, scope_type, scope_id, user_id, status, revision)
VALUES ('yu25-contributor-binding', 'org-a', 'contributor', 'project', 'project-a', 'user-a', 'active', 1)`); err != nil {
		t.Fatal(err)
	}
	resolver, err := humanauthz.NewGrantResolver(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := fixture.manager.VerifyAccessToken(t.Context(), login.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	before, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{
		Principal: principal, Permissions: []authz.PermissionKey{"delivery.work-items.create"},
	})
	if err != nil || len(before) != 1 || before[0].RoleID != "contributor" || before[0].Scope != "project:project-a" {
		t.Fatalf("grant before revoke=%#v error=%v", before, err)
	}
	if _, err := fixture.database.Exec(`UPDATE role_bindings SET status = 'disabled', revision = 2, updated_at = updated_at WHERE id = 'yu25-contributor-binding' AND revision = 1`); err != nil {
		t.Fatal(err)
	}
	// Project-role revoke is authorization invalidation, not global
	// authentication invalidation. The same session stays authenticated while
	// the next durable grant read immediately loses the revoked permission.
	principal, err = fixture.manager.VerifyAccessToken(t.Context(), login.AccessToken)
	if err != nil || !principal.Authenticated {
		t.Fatalf("role-revoked session should remain authenticated principal=%#v error=%v", principal, err)
	}
	if _, err := fixture.manager.VerifySessionToken(t.Context(), login.SessionToken); err != nil {
		t.Fatalf("role-revoked opaque session should remain valid: %v", err)
	}
	after, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{
		Principal: principal, Permissions: []authz.PermissionKey{"delivery.work-items.create"},
	})
	if err != nil || len(after) != 0 {
		t.Fatalf("revoked role retained grant=%#v error=%v", after, err)
	}
}
