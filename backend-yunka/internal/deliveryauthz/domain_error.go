package deliveryauthz

import (
	"errors"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/yunka.io/gateway/authz"
)

// NormalizeDomainDenial keeps delivery domain sentinels observable while
// classifying separation-of-duties rejections at the authorization boundary.
// Business application packages must not import or construct gateway authz
// errors directly.
func NormalizeDomainDenial(err error) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, delivery.ErrProductionPrincipalRequired) &&
		!errors.Is(err, delivery.ErrImplementationSourceRequired) &&
		!errors.Is(err, delivery.ErrImplementerCannotVerifyOwnChange) {
		return err
	}
	return errors.Join(authz.Denied(authz.Decision{Reason: authz.ReasonPermissionDenied}), err)
}
