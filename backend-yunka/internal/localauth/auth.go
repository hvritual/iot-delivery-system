// Package localauth supplies the explicitly local trust boundary used by the
// MVP. It is intentionally not an identity-provider integration.
package localauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	APIKeyEnvironment               = "IOT_DELIVERY_LOCAL_API_KEY"
	APIKeyHeader                    = "X-API-Key"
	ViewerAPIKeyEnvironment         = "IOT_DELIVERY_LOCAL_VIEWER_API_KEY"
	ContributorAPIKeyEnvironment    = "IOT_DELIVERY_LOCAL_CONTRIBUTOR_API_KEY"
	ReleaseManagerAPIKeyEnvironment = "IOT_DELIVERY_LOCAL_RELEASE_MANAGER_API_KEY"

	RoleViewer         = "viewer"
	RoleContributor    = "contributor"
	RoleReleaseManager = "release-manager"
	RoleLocalAdmin     = "local-admin"

	// DevelopmentTenantID is the stable server-assigned tenant for the
	// development-only API-key boundary. It is never sourced from a client.
	DevelopmentTenantID = "local-development"

	serviceCredentialPrefix = "svc."
)

var errInvalidAPIKey = errors.New("local API key is required")

type Authenticator struct {
	credentials []credential
}

type credential struct {
	apiKey []byte
	role   string
}

// NewAuthenticator constructs the local API-key boundary from an explicit
// server-supplied credential. It is used by composition roots and tests that
// must not mutate process environment.
func NewAuthenticator(apiKey string) (*Authenticator, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("local API key is required")
	}
	if isServiceCredential(apiKey) {
		return nil, errors.New("local API key must use the legacy credential namespace")
	}
	return &Authenticator{credentials: []credential{{apiKey: []byte(apiKey), role: RoleLocalAdmin}}}, nil
}

func FromEnvironment() (*Authenticator, error) {
	apiKey := strings.TrimSpace(os.Getenv(APIKeyEnvironment))
	if apiKey == "" {
		return nil, errors.New("local API key environment is required")
	}
	if isServiceCredential(apiKey) {
		return nil, errors.New("local API key must use the legacy credential namespace")
	}
	credentials := []credential{{apiKey: []byte(apiKey), role: RoleLocalAdmin}}
	for _, value := range []struct {
		environment string
		role        string
	}{
		{environment: ViewerAPIKeyEnvironment, role: RoleViewer},
		{environment: ContributorAPIKeyEnvironment, role: RoleContributor},
		{environment: ReleaseManagerAPIKeyEnvironment, role: RoleReleaseManager},
	} {
		apiKey = strings.TrimSpace(os.Getenv(value.environment))
		if apiKey == "" {
			continue
		}
		if isServiceCredential(apiKey) {
			return nil, errors.New("local API key must use the legacy credential namespace")
		}
		credentials = append(credentials, credential{apiKey: []byte(apiKey), role: value.role})
	}
	for left := range credentials {
		for right := left + 1; right < len(credentials); right++ {
			if subtle.ConstantTimeCompare(credentials[left].apiKey, credentials[right].apiKey) == 1 {
				return nil, errors.New("local API key environments must use distinct values")
			}
		}
	}
	return &Authenticator{credentials: credentials}, nil
}

func (authenticator *Authenticator) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, err := authenticator.authenticate(request.Header.Get(APIKeyHeader))
		if err != nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(writer, request.WithContext(identity.WithPrincipal(request.Context(), principal)))
	})
}

func (authenticator *Authenticator) GRPCUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		values, _ := metadata.FromIncomingContext(ctx)
		apiKeys := values.Get(strings.ToLower(APIKeyHeader))
		var apiKey string
		if len(apiKeys) > 0 {
			apiKey = apiKeys[0]
		}
		principal, err := authenticator.authenticate(apiKey)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "local API key is required")
		}
		return handler(identity.WithPrincipal(ctx, principal), request)
	}
}

// AuthenticateAPIKey resolves a local process credential to the same
// principal used by HTTP and gRPC. It is used by the stdio MCP entrypoint so
// MCP calls cannot bypass the configured local role policy.
func (authenticator *Authenticator) AuthenticateAPIKey(candidate string) (identity.Principal, error) {
	return authenticator.authenticate(candidate)
}

func (authenticator *Authenticator) authenticate(candidate string) (identity.Principal, error) {
	if authenticator == nil || len(authenticator.credentials) == 0 {
		return identity.Principal{}, errInvalidAPIKey
	}
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || isServiceCredential(candidate) {
		return identity.Principal{}, errInvalidAPIKey
	}
	var role string
	matched := 0
	for _, credential := range authenticator.credentials {
		if subtle.ConstantTimeCompare([]byte(candidate), credential.apiKey) == 1 {
			matched = 1
			role = credential.role
		}
	}
	if matched == 0 {
		return identity.Principal{}, errInvalidAPIKey
	}
	return identity.Principal{
		Subject:       "local-api-key/" + role,
		TenantID:      DevelopmentTenantID,
		UserID:        "local-api-key/" + role,
		Roles:         []string{role},
		AuthMethod:    identity.AuthMethodAPIKey,
		Authenticated: true,
	}, nil
}

func isServiceCredential(value string) bool { return strings.HasPrefix(value, serviceCredentialPrefix) }

type grantResolver struct{}

func NewGrantResolver() authz.GrantResolver { return grantResolver{} }

func NewAuthorizer() (*authz.GrantAuthorizer, error) {
	return authz.NewGrantAuthorizerWithResolver(NewGrantResolver())
}

func (grantResolver) ResolveGrants(_ context.Context, request authz.GrantRequest) ([]authz.Grant, error) {
	requested := make(map[authz.PermissionKey]struct{}, len(request.Permissions))
	for _, permission := range request.Permissions {
		requested[authz.PermissionKey(strings.TrimSpace(string(permission)))] = struct{}{}
	}
	grants := make([]authz.Grant, 0, len(request.Permissions))
	seen := make(map[authz.PermissionKey]struct{}, len(request.Permissions))
	for _, role := range request.Principal.Roles {
		role = strings.TrimSpace(role)
		for _, permission := range permissionsByRole[role] {
			if _, wanted := requested[permission]; !wanted {
				continue
			}
			if _, exists := seen[permission]; exists {
				continue
			}
			seen[permission] = struct{}{}
			grants = append(grants, authz.Grant{Permission: permission, RoleID: role, Scope: "local"})
		}
	}
	sort.Slice(grants, func(left, right int) bool {
		return grants[left].Permission < grants[right].Permission
	})
	return grants, nil
}

var permissionsByRole = map[string][]authz.PermissionKey{
	RoleViewer: {
		"delivery.dashboard.read",
		"delivery.projects.read",
		"delivery.work-items.read",
		"delivery.items.read", // Development-only alias for legacy extension operations.
	},
	RoleContributor: {
		"delivery.dashboard.read",
		"delivery.projects.read",
		"delivery.work-items.read",
		"delivery.work-items.create",
		"delivery.work-items.update",
		"delivery.work-items.comment.create",
		"delivery.work-items.context.update",
		"delivery.items.read",  // Development-only alias for legacy extension operations.
		"delivery.items.write", // Development-only alias for legacy saved views.
	},
	RoleReleaseManager: {
		"delivery.dashboard.read",
		"delivery.projects.read",
		"delivery.work-items.read",
		"delivery.work-items.create",
		"delivery.work-items.update",
		"delivery.work-items.comment.create",
		"delivery.work-items.context.update",
		"delivery.work-items.gate.advance",
		"delivery.work-items.close",
		"delivery.items.read",  // Development-only alias for legacy extension operations.
		"delivery.items.write", // Development-only alias for legacy saved views.
	},
	RoleLocalAdmin: {
		"delivery.dashboard.read",
		"delivery.projects.read",
		"delivery.work-items.read",
		"delivery.work-items.create",
		"delivery.work-items.update",
		"delivery.work-items.comment.create",
		"delivery.work-items.context.update",
		"delivery.work-items.gate.advance",
		"delivery.work-items.close",
		"delivery.projects.create",
		"delivery.releases.create",
		"delivery.sprints.create",
		"delivery.milestones.create",
		"delivery.items.read",  // Development-only alias for legacy extension operations.
		"delivery.items.write", // Development-only alias for legacy saved views.
	},
}

// DevelopmentPermissionsForRole returns a copy of the explicit local
// compatibility profile. It is not a production role-binding API.
func DevelopmentPermissionsForRole(role string) []authz.PermissionKey {
	return slices.Clone(permissionsByRole[role])
}
