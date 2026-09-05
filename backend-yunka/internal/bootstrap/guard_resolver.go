package bootstrap

import (
	"context"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

type guardResolverMux []authz.GuardResolver

func (mux guardResolverMux) ResolveGuard(operation authz.OperationID) (authz.OperationGuard, bool) {
	for _, resolver := range mux {
		if resolver == nil {
			continue
		}
		if guard, ok := resolver.ResolveGuard(operation); ok {
			return guard, true
		}
	}
	return nil, false
}

// developmentCompatibleGuardResolver preserves the historical API-key
// behavior only for explicit development principals. JWT humans still execute
// the durable OperationGuard, so environment no longer chooses their authority
// model.
type developmentCompatibleGuardResolver struct {
	durable authz.GuardResolver
}

func (resolver developmentCompatibleGuardResolver) ResolveGuard(operation authz.OperationID) (authz.OperationGuard, bool) {
	if resolver.durable == nil {
		return nil, false
	}
	guard, ok := resolver.durable.ResolveGuard(operation)
	if !ok || guard == nil {
		return nil, false
	}
	return developmentCompatibleGuard{durable: guard}, true
}

type developmentCompatibleGuard struct {
	durable authz.OperationGuard
}

func (guard developmentCompatibleGuard) Prepare(ctx context.Context, authorized authz.AuthorizedOperation, input any) (context.Context, error) {
	if isDevelopmentCompatibilityPrincipal(authorized.Principal) {
		return ctx, nil
	}
	return guard.durable.Prepare(ctx, authorized, input)
}

func isDevelopmentCompatibilityPrincipal(principal identity.Principal) bool {
	const prefix = "local-api-key/"
	return principal.Authenticated &&
		principal.AuthMethod == identity.AuthMethodAPIKey &&
		principal.TenantID == localauth.DevelopmentTenantID &&
		principal.Subject == principal.UserID &&
		strings.HasPrefix(principal.UserID, prefix) &&
		len(strings.TrimPrefix(principal.UserID, prefix)) > 0
}
