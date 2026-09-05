package bootstrap

import (
	"context"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type recordingOperationGuard struct{ calls int }

func (guard *recordingOperationGuard) Prepare(ctx context.Context, _ authz.AuthorizedOperation, _ any) (context.Context, error) {
	guard.calls++
	return ctx, nil
}

func TestYU23DevelopmentGuardBypassAcceptsOnlyCanonicalLocalAPIKeyPrincipal(t *testing.T) {
	durable := &recordingOperationGuard{}
	guard := developmentCompatibleGuard{durable: durable}
	canonical := identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "local-api-key/local-admin", Subject: "local-api-key/local-admin", Roles: []string{localauth.RoleLocalAdmin}}
	if _, err := guard.Prepare(t.Context(), authz.AuthorizedOperation{Principal: canonical}, nil); err != nil {
		t.Fatal(err)
	}
	if durable.calls != 0 {
		t.Fatal("canonical development API-key unexpectedly entered durable guard")
	}
	for _, principal := range []identity.Principal{
		{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Subject: "local-user/user-a", Roles: []string{localauth.RoleLocalAdmin}},
		{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "org-a", UserID: "local-api-key/local-admin", Subject: "local-api-key/local-admin", Roles: []string{localauth.RoleLocalAdmin}},
		{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "local-api-key/local-admin", Subject: "different-subject", Roles: []string{localauth.RoleLocalAdmin}},
		{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "local-api-key/fake", Subject: "local-api-key/fake", Roles: []string{localauth.RoleLocalAdmin}},
		{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: localauth.DevelopmentTenantID, UserID: "local-api-key/viewer", Subject: "local-api-key/viewer", Roles: []string{localauth.RoleViewer, localauth.RoleLocalAdmin}},
	} {
		if _, err := guard.Prepare(t.Context(), authz.AuthorizedOperation{Principal: principal}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if durable.calls != 5 {
		t.Fatalf("noncanonical principals entered durable guard %d times, want 5", durable.calls)
	}
}
