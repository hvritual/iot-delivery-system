package bootstrap

import "github.com/hvritual/yunka.io/gateway/authz"

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
