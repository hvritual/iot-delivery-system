package rpc

import (
	"context"
	"errors"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/hvritual/yunka.io/gateway/authz"
	gatewaygrpc "github.com/hvritual/yunka.io/gateway/rpc/transport/grpc"
)

// RevisionErrorUnaryServerInterceptor translates consumer-owned optimistic
// concurrency errors after generated RPC adapters preserve their application
// causes. It deliberately leaves unrelated transport errors unchanged.
func RevisionErrorUnaryServerInterceptor(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	response, err := handler(ctx, request)
	switch {
	case authz.IsDenied(err):
		return nil, gatewaygrpc.OperationError(err)
	case errors.Is(err, delivery.ErrRevisionConflict):
		return nil, status.Error(codes.Aborted, "revision_conflict")
	case errors.Is(err, delivery.ErrInvalidExpectedRevision):
		return nil, status.Error(codes.InvalidArgument, "invalid_expected_revision")
	default:
		return response, err
	}
}
