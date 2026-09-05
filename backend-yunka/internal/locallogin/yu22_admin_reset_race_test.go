package locallogin

import (
	"context"
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localoutbox"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtx"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/operation"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestYU22AdminResetAndSelfPasswordChangeCannotBothWinStaleCAS(t *testing.T) {
	t.Run("admin reset wins first", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		userLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		adminManager, adminContext := newYU22AdminManager(t, fixture)
		reset, err := adminManager.ResetCredential(adminContext, localmemberadmin.ResetCredentialInput{
			UserID: "user-a", ExpectedUserRevision: 1, ExpectedCredentialRevision: 1, Password: []byte("YU22-admin-reset-password"),
		})
		if err != nil || reset.UserRevision != 2 || reset.CredentialRevision != 2 {
			t.Fatalf("admin reset result=%#v error=%v", reset, err)
		}
		_, err = fixture.manager.ChangePassword(fixture.context(t), ChangePasswordInput{
			SessionToken: userLogin.SessionToken,
			ExpectedSessionRevision: 1,
			ExpectedUserRevision: 1,
			ExpectedCredentialRevision: 1,
			CurrentPassword: []byte("YU21-password-secret"),
			NewPassword: []byte("YU22-self-must-not-win"),
		})
		if !errors.Is(err, ErrUserRevisionConflict) {
			t.Fatalf("self change after admin reset error=%v, want User CAS conflict", err)
		}
		verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU22-admin-reset-password"))
		if err != nil || !verification.Match || verification.Revision != 2 {
			t.Fatalf("admin-reset credential=%#v error=%v", verification, err)
		}
	})

	t.Run("self password change wins first", func(t *testing.T) {
		fixture := newLoginFixture(t, false)
		userLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "user-a", Password: []byte("YU21-password-secret")})
		if err != nil {
			t.Fatal(err)
		}
		adminManager, adminContext := newYU22AdminManager(t, fixture)
		changed, err := fixture.manager.ChangePassword(fixture.context(t), ChangePasswordInput{
			SessionToken: userLogin.SessionToken,
			ExpectedSessionRevision: 1,
			ExpectedUserRevision: 1,
			ExpectedCredentialRevision: 1,
			CurrentPassword: []byte("YU21-password-secret"),
			NewPassword: []byte("YU22-self-password-wins"),
		})
		if err != nil || changed.UserRevision != 2 || changed.CredentialRevision != 2 {
			t.Fatalf("self password change=%#v error=%v", changed, err)
		}
		_, err = adminManager.ResetCredential(adminContext, localmemberadmin.ResetCredentialInput{
			UserID: "user-a", ExpectedUserRevision: 1, ExpectedCredentialRevision: 1, Password: []byte("YU22-admin-must-not-win"),
		})
		if !errors.Is(err, localmemberadmin.ErrMemberRevisionConflict) {
			t.Fatalf("admin reset after self change error=%v, want member CAS conflict", err)
		}
		verification, err := fixture.credentials.VerifyPassword(t.Context(), "org-a", "user-a", []byte("YU22-self-password-wins"))
		if err != nil || !verification.Match || verification.Revision != 2 {
			t.Fatalf("self-change credential=%#v error=%v", verification, err)
		}
	})
}

func newYU22AdminManager(t *testing.T, fixture *loginFixture) (*localmemberadmin.Manager, context.Context) {
	t.Helper()
	if _, err := fixture.database.Exec(`INSERT INTO users (id, organization_id, display_name, status, revision) VALUES ('admin-yu22', 'org-a', 'YU22 Admin', 'active', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status) VALUES ('binding-admin-yu22', 'org-a', 'system-administrator', 'organization', 'org-a', 'admin-yu22', 'active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.credentials.SetPassword(t.Context(), "org-a", "admin-yu22", []byte("YU22-admin-login-password"), 0); err != nil {
		t.Fatal(err)
	}
	adminLogin, err := fixture.manager.Login(fixture.context(t), LoginInput{OrganizationID: "org-a", UserID: "admin-yu22", Password: []byte("YU22-admin-login-password")})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := fixture.manager.VerifyAccessToken(t.Context(), adminLogin.AccessToken)
	if err != nil || !principal.Authenticated || principal.UserID != "admin-yu22" {
		t.Fatalf("verified admin principal=%#v error=%v", principal, err)
	}
	humanResolver, err := humanauthz.NewGrantResolver(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(humanResolver)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := localmemberadmin.NewOperationGuard(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	security, err := authz.NewExecutionSecurity(authorizer, guard.GuardResolver())
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := audit.NewSecurityRecorder(fixture.auditStore)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := audit.NewRecordingExecutor(operation.NewExecutorWithOptions(security, operation.ExecutorOptions{
		Transactions: localtx.NewSQLiteFactory(fixture.database),
	}), recorder)
	if err != nil {
		t.Fatal(err)
	}
	outboxStore, err := localoutbox.NewSQLiteStore(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := localmemberadmin.NewManager(fixture.database, fixture.credentials, fixture.auditStore, outboxStore, executor)
	if err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(fixture.context(t), principal)
	return manager, ctx
}
