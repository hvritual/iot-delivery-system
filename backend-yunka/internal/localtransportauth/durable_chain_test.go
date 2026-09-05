package localtransportauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/humanauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
	stdgrpc "google.golang.org/grpc"
	grpcmetadata "google.golang.org/grpc/metadata"
)

func TestYU23HTTPGRPCAndMCPLocalMembersResolveTheSameDurableGrantAndGuard(t *testing.T) {
	fixture := newTransportFixture(t)
	if _, err := fixture.database.Exec(`INSERT INTO role_bindings (id, organization_id, role_id, scope_type, scope_id, user_id, status)
VALUES ('yu23-admin-binding', 'org-a', 'system-administrator', 'organization', 'org-a', 'user-a', 'active')`); err != nil {
		t.Fatal(err)
	}
	var httpPrincipal identity.Principal
	httpHandler := fixture.verifier.HTTPMiddleware(nil)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		httpPrincipal, _ = identity.FromContext(request.Context())
		writer.WriteHeader(http.StatusNoContent)
	}))
	httpRequest := httptest.NewRequest(http.MethodGet, "http://example.test/api/items", nil)
	httpRequest.Header.Set("Authorization", "Bearer "+fixture.result.AccessToken)
	httpResponse := httptest.NewRecorder()
	httpHandler.ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusNoContent {
		t.Fatalf("HTTP authentication status=%d", httpResponse.Code)
	}
	var grpcPrincipal identity.Principal
	grpcInterceptor := fixture.verifier.GRPCUnaryServerInterceptor(nil)
	grpcContext := grpcmetadata.NewIncomingContext(t.Context(), grpcmetadata.Pairs(GRPCAuthorizationMetadata, "Bearer "+fixture.result.AccessToken))
	if _, err := grpcInterceptor(grpcContext, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, func(ctx context.Context, _ any) (any, error) {
		grpcPrincipal, _ = identity.FromContext(ctx)
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	mcpPrincipal, err := fixture.verifier.VerifySessionToken(t.Context(), fixture.result.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	principals := map[string]identity.Principal{"http": httpPrincipal, "grpc": grpcPrincipal, "mcp": mcpPrincipal}
	for name, principal := range principals {
		if !principal.Authenticated || principal.AuthMethod != identity.AuthMethodJWT || principal.TenantID != "org-a" || principal.UserID != "user-a" || len(principal.Roles) != 0 {
			t.Fatalf("%s principal=%#v", name, principal)
		}
	}
	resolver, err := humanauthz.NewGrantResolver(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(principal identity.Principal) []authz.Grant {
		grants, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{Principal: principal, Permissions: []authz.PermissionKey{authz.PermissionKey(localmemberadmin.PermissionManageUsers)}})
		if err != nil {
			t.Fatal(err)
		}
		return grants
	}
	httpGrants, grpcGrants, mcpGrants := resolve(httpPrincipal), resolve(grpcPrincipal), resolve(mcpPrincipal)
	if len(httpGrants) != 1 || !reflect.DeepEqual(httpGrants, grpcGrants) || !reflect.DeepEqual(httpGrants, mcpGrants) || httpGrants[0].RoleID != "system-administrator" || httpGrants[0].Scope != "organization:org-a" {
		t.Fatalf("durable grants HTTP=%#v gRPC=%#v MCP=%#v", httpGrants, grpcGrants, mcpGrants)
	}
	authorizer, err := authz.NewGrantAuthorizerWithResolver(resolver)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := localmemberadmin.NewOperationGuard(fixture.database)
	if err != nil {
		t.Fatal(err)
	}
	plan := localmemberadmin.OperationPlans()[0]
	policy := authz.PolicyFromOperationPlan(plan)
	for name, principal := range principals {
		decision, err := authorizer.Authorize(t.Context(), principal, policy)
		if err != nil || !decision.Allowed {
			t.Fatalf("%s durable decision=%#v error=%v", name, decision, err)
		}
		if _, err := guard.Prepare(t.Context(), authz.AuthorizedOperation{Principal: principal, Policy: policy, Decision: decision}, &localmemberadmin.CreateInput{DisplayName: "guard-only", Password: []byte("not-persisted")}); err != nil {
			t.Fatalf("%s durable guard error=%v", name, err)
		}
	}
	if _, err := fixture.database.Exec(`UPDATE role_bindings SET status = 'disabled' WHERE id = 'yu23-admin-binding'`); err != nil {
		t.Fatal(err)
	}
	for name, principal := range principals {
		if grants := resolve(principal); len(grants) != 0 {
			t.Fatalf("%s retained durable grant after RoleBinding revocation: %#v", name, grants)
		}
		decision, err := authorizer.Authorize(t.Context(), principal, policy)
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed {
			t.Fatalf("%s remained authorized after RoleBinding revocation: %#v", name, decision)
		}
	}
}
