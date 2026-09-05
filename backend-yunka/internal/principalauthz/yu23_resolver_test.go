package principalauthz

import (
	"context"
	"testing"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type recordingResolver struct {
	calls int
	grant authz.Grant
}

func (resolver *recordingResolver) ResolveGrants(context.Context, authz.GrantRequest) ([]authz.Grant, error) {
	resolver.calls++
	if resolver.grant.Permission == "" {
		return nil, nil
	}
	return []authz.Grant{resolver.grant}, nil
}

func TestYU23PrincipalResolverNeverFallsBackAcrossIdentityTypes(t *testing.T) {
	human := &recordingResolver{grant: authz.Grant{Permission: "delivery.projects.read", RoleID: "human", Scope: "project:p1"}}
	service := &recordingResolver{grant: authz.Grant{Permission: "delivery.projects.read", RoleID: "service", Scope: "project:p1"}}
	development := &recordingResolver{grant: authz.Grant{Permission: "delivery.projects.read", RoleID: "local-admin", Scope: "local"}}
	resolver, err := NewWithDevelopmentCompatibility(human, service, development)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		principal identity.Principal
		want      *recordingResolver
	}{
		{name: "jwt human", principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-a", Roles: []string{"local-admin", "forged"}}, want: human},
		{name: "service", principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service/account"}, want: service},
		{name: "development api key", principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "local-development", UserID: "local-api-key/local-admin", Roles: []string{"local-admin"}}, want: development},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			beforeHuman, beforeService, beforeDevelopment := human.calls, service.calls, development.calls
			grants, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{Principal: tc.principal, Permissions: []authz.PermissionKey{"delivery.projects.read"}})
			if err != nil || len(grants) != 1 {
				t.Fatalf("grants=%#v error=%v", grants, err)
			}
			if tc.want == human && human.calls != beforeHuman+1 || tc.want != human && human.calls != beforeHuman {
				t.Fatal("human resolver selection drift")
			}
			if tc.want == service && service.calls != beforeService+1 || tc.want != service && service.calls != beforeService {
				t.Fatal("service resolver selection drift")
			}
			if tc.want == development && development.calls != beforeDevelopment+1 || tc.want != development && development.calls != beforeDevelopment {
				t.Fatal("development resolver selection drift")
			}
		})
	}
}

func TestYU23ProductionResolverGivesAPIKeyNoCompatibilityFallback(t *testing.T) {
	human := &recordingResolver{}
	service := &recordingResolver{}
	resolver, err := New(human, service)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := resolver.ResolveGrants(t.Context(), authz.GrantRequest{Principal: identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodAPIKey, TenantID: "local-development", UserID: "local-api-key/local-admin", Roles: []string{"local-admin"}}, Permissions: []authz.PermissionKey{"delivery.projects.read"}})
	if err != nil || len(grants) != 0 || human.calls != 0 || service.calls != 0 {
		t.Fatalf("production API-key grants=%#v human=%d service=%d error=%v", grants, human.calls, service.calls, err)
	}
}
