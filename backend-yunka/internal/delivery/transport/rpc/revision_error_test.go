package rpc

import (
	"context"
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"yunka.io/gateway/authz"
)

func TestRevisionErrorUnaryServerInterceptorUsesStableCategories(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		err         error
		wantCode    codes.Code
		wantMessage string
	}{
		{name: "revision conflict", err: delivery.ErrRevisionConflict, wantCode: codes.Aborted, wantMessage: "revision_conflict"},
		{name: "invalid expected revision", err: delivery.ErrInvalidExpectedRevision, wantCode: codes.InvalidArgument, wantMessage: "invalid_expected_revision"},
		{name: "unauthenticated remains prior mapping", err: authz.Denied(authz.Decision{Reason: authz.ReasonUnauthenticated}), wantCode: codes.Unauthenticated, wantMessage: "authentication required"},
		{name: "unauthenticated wins over joined revision conflict", err: errors.Join(authz.Denied(authz.Decision{Reason: authz.ReasonUnauthenticated}), delivery.ErrRevisionConflict), wantCode: codes.Unauthenticated, wantMessage: "authentication required"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := RevisionErrorUnaryServerInterceptor(t.Context(), nil, nil, func(context.Context, any) (any, error) {
				return nil, scenario.err
			})
			result := status.Convert(err)
			if result.Code() != scenario.wantCode || result.Message() != scenario.wantMessage {
				t.Fatalf("gRPC error = code %s message %q, want code %s message %q", result.Code(), result.Message(), scenario.wantCode, scenario.wantMessage)
			}
		})
	}
}
