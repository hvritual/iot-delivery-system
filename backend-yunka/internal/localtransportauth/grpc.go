package localtransportauth

import (
	"context"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const GRPCAuthorizationMetadata = "authorization"

// GRPCUnaryServerInterceptor accepts end-user local access JWTs from standard
// authorization metadata. If absent, it delegates to the existing explicit
// development compatibility interceptor. Service credentials use Yunka's
// distinct x-yunka-service-authorization metadata and are selected outside this
// interceptor by serviceauth.
func (verifier *Verifier) GRPCUnaryServerInterceptor(fallback stdgrpc.UnaryServerInterceptor) stdgrpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *stdgrpc.UnaryServerInfo, handler stdgrpc.UnaryHandler) (any, error) {
		incoming, _ := grpcmetadata.FromIncomingContext(ctx)
		values := incoming.Get(GRPCAuthorizationMetadata)
		legacyValues := incoming.Get(strings.ToLower(localauth.APIKeyHeader))
		if len(values) == 0 {
			if fallback != nil {
				return fallback(ctx, request, info, handler)
			}
			verifier.recordGRPCFailure(ctx, info, "authentication.missing_credential")
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		if len(values) != 1 || len(legacyValues) > 0 {
			verifier.recordGRPCFailure(ctx, info, "authentication.mixed_credentials")
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		token, ok := parseBearer(values[0])
		if !ok {
			verifier.recordGRPCFailure(ctx, info, "authentication.invalid_credential")
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		principal, err := verifier.VerifyAccessToken(ctx, token)
		if err != nil {
			verifier.recordGRPCFailure(ctx, info, "authentication.invalid_credential")
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		secured := identity.WithPrincipal(ctx, principal)
		metadata, _ := runtimecontext.MetadataFrom(secured)
		metadata.Transport = "grpc"
		metadata.Protocol = "grpc"
		if info != nil {
			metadata.Route = info.FullMethod
		}
		secured = runtimecontext.WithMetadata(secured, metadata)
		if verifier != nil && verifier.recorder != nil {
			if err := verifier.recorder.RecordAuthenticationAccepted(secured, "authentication.local_access_token"); err != nil {
				return nil, status.Error(codes.Unavailable, "service unavailable")
			}
		}
		return handler(secured, request)
	}
}

func (verifier *Verifier) recordGRPCFailure(ctx context.Context, info *stdgrpc.UnaryServerInfo, reason string) {
	if verifier == nil || verifier.recorder == nil {
		return
	}
	metadata, _ := runtimecontext.MetadataFrom(ctx)
	metadata.Transport = "grpc"
	metadata.Protocol = "grpc"
	if info != nil {
		metadata.Route = info.FullMethod
	}
	ctx = runtimecontext.WithMetadata(ctx, metadata)
	_ = verifier.recorder.RecordAuthenticationFailure(ctx, "authentication.local_access_token", "grpc", reason)
}
