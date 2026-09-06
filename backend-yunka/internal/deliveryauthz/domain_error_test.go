package deliveryauthz

import (
	"errors"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/yunka.io/gateway/authz"
)

func TestNormalizeDomainDenial(t *testing.T) {
	for _, sentinel := range []error{
		delivery.ErrProductionPrincipalRequired,
		delivery.ErrImplementationSourceRequired,
		delivery.ErrImplementerCannotVerifyOwnChange,
	} {
		err := NormalizeDomainDenial(sentinel)
		if !authz.IsDenied(err) || !errors.Is(err, sentinel) {
			t.Fatalf("NormalizeDomainDenial(%v) = %v, want permission denial preserving sentinel", sentinel, err)
		}
	}

	plain := errors.New("plain domain failure")
	if got := NormalizeDomainDenial(plain); got != plain {
		t.Fatalf("NormalizeDomainDenial(non-denial) = %v, want original error", got)
	}
	if got := NormalizeDomainDenial(nil); got != nil {
		t.Fatalf("NormalizeDomainDenial(nil) = %v, want nil", got)
	}
}
