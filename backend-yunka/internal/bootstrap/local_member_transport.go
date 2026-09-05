package bootstrap

import (
	"errors"
	"net/http"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtransportauth"
	"google.golang.org/grpc"
)

type localMemberTransport interface {
	HTTPMiddleware(func(http.Handler) http.Handler) func(http.Handler) http.Handler
	GRPCUnaryServerInterceptor(grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor
}

func configuredLocalMemberTransport(login *locallogin.Manager, recorder *audit.SecurityRecorder) (localMemberTransport, error) {
	if login == nil {
		return nil, nil
	}
	if recorder == nil {
		return nil, errors.New("local member transport audit recorder is required")
	}
	return localtransportauth.New(login, recorder)
}
