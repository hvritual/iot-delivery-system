package bootstrap

import (
	"errors"
	"net/http"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localbffhttp"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/locallogin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localmemberadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localprojectroleadmin"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localtransportauth"
)

func registerLocalAuthBFF(
	mux *http.ServeMux,
	login *locallogin.Manager,
	members *localmemberadmin.Manager,
	projectRoles *localprojectroleadmin.Manager,
	recorder *audit.SecurityRecorder,
) error {
	if login == nil {
		return nil
	}
	if mux == nil || members == nil || projectRoles == nil || recorder == nil {
		return errors.New("local auth BFF runtime dependencies are required")
	}
	verifier, err := localtransportauth.New(login, recorder)
	if err != nil {
		return err
	}
	handler, err := localbffhttp.New(localbffhttp.Config{
		Login: login,
		Verifier: verifier,
		Members: members,
		ProjectRoles: projectRoles,
	})
	if err != nil {
		return err
	}
	return handler.Register(mux)
}
