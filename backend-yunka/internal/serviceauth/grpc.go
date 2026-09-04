package serviceauth

import (
	"context"
	"strings"

	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpccredentials "google.golang.org/grpc/credentials"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"github.com/hvritual/yunka.io/framework/core/identity"
	coremiddleware "github.com/hvritual/yunka.io/framework/core/middleware"
	yunkagrpc "github.com/hvritual/yunka.io/gateway/rpc/transport/grpc"
)

// Verify implements Yunka's CredentialVerifier contract with the service
// credential stored in SQLite. It uses Yunka's standard Bearer metadata,
// generic errors, token limits, and privacy-and-integrity requirement.
func (manager *Manager) Verify(ctx context.Context) (identity.Principal, error) {
	if manager == nil || manager.database == nil {
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInvalid
	}
	if !manager.allowInsecureTransportForDevelopment && !transportProvidesPrivacyAndIntegrity(ctx) {
		manager.recordAuthenticationFailure(ctx, "authentication.insecure_transport")
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInsecureTransport
	}
	token, err := serviceAuthorizationToken(ctx)
	if err != nil {
		manager.recordAuthenticationFailure(ctx, "authentication.invalid_credential")
		return identity.Principal{}, err
	}
	principal, err := manager.authenticate(ctx, token)
	if err != nil {
		manager.recordAuthenticationFailure(ctx, "authentication.invalid_credential")
		return identity.Principal{}, yunkagrpc.ErrServiceCredentialInvalid
	}
	return principal, nil
}

func (manager *Manager) recordAuthenticationFailure(ctx context.Context, reasonCode string) {
	manager.recordAuthenticationFailureFor(ctx, "authentication.service_token", reasonCode)
}

func (manager *Manager) recordAuthenticationFailureFor(ctx context.Context, operation, reasonCode string) {
	if manager == nil || manager.auditRecorder == nil {
		return
	}
	_ = manager.auditRecorder.RecordAuthenticationFailure(ctx, operation, "grpc", reasonCode)
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
				manager.recordAuthenticationFailure(ctx, "authentication.missing_credential")
				return nil, status.Error(codes.Unauthenticated, "unauthenticated")
			}
			handlerInvoked := false
			response, fallbackErr := fallback(ctx, request, info, func(handlerContext context.Context, handlerRequest any) (any, error) {
				handlerInvoked = true
				return handler(handlerContext, handlerRequest)
			})
			if !handlerInvoked && status.Code(fallbackErr) == codes.Unauthenticated {
				manager.recordAuthenticationFailureFor(ctx, "authentication.development_api_key", "authentication.invalid_credential")
			}
			return response, fallbackErr
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
