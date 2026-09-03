package localauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"yunka.io/framework/core/identity"
	"yunka.io/gateway/authz"
)

func TestHTTPMiddlewareRequiresEnvironmentAPIKeyAndAttachesLocalAdmin(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "local-test-key")
	authenticator, err := localauth.FromEnvironment()
	if err != nil {
		t.Fatalf("load local API key: %v", err)
	}
	handler := authenticator.HTTPMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := identity.FromContext(request.Context())
		if !ok || !principal.Authenticated || principal.AuthMethod != identity.AuthMethodAPIKey || !principal.HasRole(localauth.RoleLocalAdmin) {
			t.Fatalf("request principal = %#v, want authenticated local administrator", principal)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))

	for name, apiKey := range map[string]string{"missing": "", "invalid": "wrong-key"} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			if apiKey != "" {
				request.Header.Set(localauth.APIKeyHeader, apiKey)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set(localauth.APIKeyHeader, "local-test-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestGrantResolverMapsRolesToExplicitPermissions(t *testing.T) {
	resolver := localauth.NewGrantResolver()
	grants, err := resolver.ResolveGrants(context.Background(), authz.GrantRequest{
		Principal: identity.Principal{
			Authenticated: true,
			AuthMethod:    identity.AuthMethodAPIKey,
			Roles:         []string{localauth.RoleContributor},
		},
		Operation:   "delivery.items.create",
		Permissions: []authz.PermissionKey{"delivery.items.write", "delivery.items.close"},
	})
	if err != nil {
		t.Fatalf("resolve contributor grants: %v", err)
	}
	if len(grants) != 1 || grants[0].Permission != "delivery.items.write" || grants[0].RoleID != localauth.RoleContributor {
		t.Fatalf("contributor grants = %#v, want only delivery.items.write", grants)
	}
}

func TestHTTPMiddlewareUsesOptionalRoleSpecificEnvironmentKey(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "local-admin-key")
	t.Setenv(localauth.ViewerAPIKeyEnvironment, "viewer-key")
	authenticator, err := localauth.FromEnvironment()
	if err != nil {
		t.Fatalf("load role-specific local API keys: %v", err)
	}
	handler := authenticator.HTTPMiddleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := identity.FromContext(request.Context())
		if !ok || !principal.HasRole(localauth.RoleViewer) || principal.HasRole(localauth.RoleLocalAdmin) {
			t.Fatalf("viewer key principal = %#v, want viewer role only", principal)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	request.Header.Set(localauth.APIKeyHeader, "viewer-key")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("viewer API key status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestLocalAPIKeyAuthenticatorRejectsServiceCredentialNamespace(t *testing.T) {
	serviceCredential := "svc.credential-test.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := localauth.NewAuthenticator(serviceCredential); err == nil {
		t.Fatal("local API-key authenticator accepted service credential namespace")
	}

	for name, environment := range map[string]string{
		"administrator":   localauth.APIKeyEnvironment,
		"viewer":          localauth.ViewerAPIKeyEnvironment,
		"contributor":     localauth.ContributorAPIKeyEnvironment,
		"release manager": localauth.ReleaseManagerAPIKeyEnvironment,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(localauth.APIKeyEnvironment, "ordinary-local-key")
			t.Setenv(environment, serviceCredential)
			if _, err := localauth.FromEnvironment(); err == nil {
				t.Fatalf("local API-key environment %s accepted service credential namespace", environment)
			}
		})
	}
}
