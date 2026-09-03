package serviceauth

import (
	"context"
	"strings"

	stdgrpc "google.golang.org/grpc"
	grpccredentials "google.golang.org/grpc/credentials"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"yunka.io/framework/core/identity"
	coremiddleware "yunka.io/framework/core/middleware"
	yunkagrpc "yunka.io/gateway/rpc/transport/grpc"
)

// Verify implements Yunka's CredentialVerifier contract with the service
// credential stored in SQLite. It uses Yunka's standard Bearer metadata,
// generic errors, token limits, and privacy-and-integrity requirement.
func (manager *Manager) Verify(ctx context.Context) (identity.Principal, error) {
	if manager == nil || manager.database == nil {
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInvalid
	}
	if !manager.allowInsecureTransportForDevelopment && !transportProvidesPrivacyAndIntegrity(ctx) {
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInsecureTransport
	}
	token, err := serviceAuthorizationToken(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	principal, err := manager.authenticate(ctx, token)
	if err != nil {
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInvalid
	}
	return principal, nil
}

// GRPCUnaryServerInterceptor selects the established legacy API-key path only
// when Yunka's service authorization metadata is absent. Once that metadata is
// present, the framework interceptor owns duplicate handling, authentication,
// standard trace extraction, and generic failures; no invalid credential can
// fall back to a local API key.
func (manager *Manager) GRPCUnaryServerInterceptor(fallback stdgrpc.UnaryServerInterceptor) stdgrpc.UnaryServerInterceptor {
	service := yunkagrpc.AuthenticatedUnaryServerInterceptor(coremiddleware.New(), manager)
	return func(ctx context.Context, request any, info *stdgrpc.UnaryServerInfo, handler stdgrpc.UnaryHandler) (any, error) {
		incoming, _ := grpcmetadata.FromIncomingContext(ctx)
		if len(incoming.Get(yunkagrpc.ServiceAuthorizationMetadata)) == 0 {
			if fallback == nil {
				return nil, yunkagrpc.ErrServiceCredentialMissing
			}
			return fallback(ctx, request, info, handler)
		}
		return service(ctx, request, info, handler)
	}
}

func serviceAuthorizationToken(ctx context.Context) (string, error) {
	metadata, ok := grpcmetadata.FromIncomingContext(ctx)
	if !ok {
		return "", yunkagrpc.ErrServiceCredentialMissing
	}
	values := metadata.Get(yunkagrpc.ServiceAuthorizationMetadata)
	if len(values) == 0 {
		return "", yunkagrpc.ErrServiceCredentialMissing
	}
	if len(values) != 1 {
		return "", yunkagrpc.ErrServiceCredentialInvalid
	}
	parts := strings.Fields(strings.TrimSpace(values[0]))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validServiceToken(parts[1]) {
		return "", yunkagrpc.ErrServiceCredentialInvalid
	}
	return parts[1], nil
}

func transportProvidesPrivacyAndIntegrity(ctx context.Context) bool {
	current, ok := peer.FromContext(ctx)
	if !ok || current.AuthInfo == nil {
		return false
	}
	type commonAuthInfoProvider interface {
		GetCommonAuthInfo() grpccredentials.CommonAuthInfo
	}
	provider, ok := current.AuthInfo.(commonAuthInfoProvider)
	return ok && provider.GetCommonAuthInfo().SecurityLevel >= grpccredentials.PrivacyAndIntegrity
}

var _ yunkagrpc.CredentialVerifier = (*Manager)(nil)
