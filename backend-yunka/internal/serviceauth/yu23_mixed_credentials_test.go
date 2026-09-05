package serviceauth

import (
	"context"
	"testing"

	yunkagrpc "github.com/hvritual/yunka.io/gateway/rpc/transport/grpc"
	stdgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestYU23ServiceAndHumanGRPCCredentialsAreMutuallyExclusive(t *testing.T) {
	manager := &Manager{}
	fallbackCalls := 0
	handlerCalls := 0
	fallback := func(ctx context.Context, request any, info *stdgrpc.UnaryServerInfo, handler stdgrpc.UnaryHandler) (any, error) {
		fallbackCalls++
		return handler(ctx, request)
	}
	interceptor := manager.GRPCUnaryServerInterceptor(fallback)
	ctx := grpcmetadata.NewIncomingContext(t.Context(), grpcmetadata.Pairs(
		yunkagrpc.ServiceAuthorizationMetadata, "Bearer svc.service-token-sentinel",
		endUserAuthorizationMetadata, "Bearer local-user-jwt-sentinel",
	))
	_, err := interceptor(ctx, nil, &stdgrpc.UnaryServerInfo{FullMethod: "/iot.delivery.v1.DeliveryService/SearchItems"}, func(context.Context, any) (any, error) {
		handlerCalls++
		return "unexpected", nil
	})
	if status.Code(err) != codes.Unauthenticated || fallbackCalls != 0 || handlerCalls != 0 {
		t.Fatalf("mixed credential error=%v fallback=%d handler=%d", err, fallbackCalls, handlerCalls)
	}
}
