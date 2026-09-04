// Package principalauthz selects the durable authority model for each
// authenticated principal type. It intentionally has no fallback path between
// human and service identity grants.
package principalauthz

import (
	"context"
	"errors"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

var ErrResolverRequired = errors.New("principal grant resolver is required")

type Resolver struct {
	humans   authz.GrantResolver
	services authz.GrantResolver
}

var _ authz.GrantResolver = (*Resolver)(nil)

func New(humans, services authz.GrantResolver) (*Resolver, error) {
	if humans == nil || services == nil {
		return nil, ErrResolverRequired
	}
	return &Resolver{humans: humans, services: services}, nil
}

func (resolver *Resolver) ResolveGrants(ctx context.Context, request authz.GrantRequest) ([]authz.Grant, error) {
	if resolver == nil || resolver.humans == nil || resolver.services == nil {
		return nil, ErrResolverRequired
	}
	switch request.Principal.AuthMethod {
	case identity.AuthMethodJWT:
		return resolver.humans.ResolveGrants(ctx, request)
	case identity.AuthMethodServiceToken:
		return resolver.services.ResolveGrants(ctx, request)
	default:
		return nil, nil
	}
}
