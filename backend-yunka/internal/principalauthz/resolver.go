// Package principalauthz selects the authority model for each authenticated
// principal type. JWT humans and service identities always use durable stores;
// development API keys may opt into an explicit compatibility resolver.
package principalauthz

import (
	"context"
	"errors"

	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/gateway/authz"
)

var ErrResolverRequired = errors.New("principal grant resolver is required")

type Resolver struct {
	humans      authz.GrantResolver
	services    authz.GrantResolver
	development authz.GrantResolver
}

var _ authz.GrantResolver = (*Resolver)(nil)

func New(humans, services authz.GrantResolver) (*Resolver, error) {
	if humans == nil || services == nil {
		return nil, ErrResolverRequired
	}
	return &Resolver{humans: humans, services: services}, nil
}

// NewWithDevelopmentCompatibility preserves the explicit legacy API-key role
// table without allowing it to influence JWT humans or service principals.
func NewWithDevelopmentCompatibility(humans, services, development authz.GrantResolver) (*Resolver, error) {
	resolver, err := New(humans, services)
	if err != nil {
		return nil, err
	}
	if development == nil {
		return nil, ErrResolverRequired
	}
	resolver.development = development
	return resolver, nil
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
	case identity.AuthMethodAPIKey:
		if resolver.development == nil {
			return nil, nil
		}
		return resolver.development.ResolveGrants(ctx, request)
	default:
		return nil, nil
	}
}
