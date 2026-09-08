package savedview

import (
	"context"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview/internal/usecase"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/domain"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
	"time"
)

// Application is a use-case seam, not a new canonical Yunka Application or RPC.
type Application interface {
	SaveView(context.Context, domain.SavedViewInput) (domain.SavedView, error)
	ListSavedViews(context.Context) ([]domain.SavedView, error)
}

// Build constructs a resource-free handler. The composition owner supplies a
// real attenuated repository and the legacy shared clock/sequence; no root UoW
// or DB connection is created here. The actual returned type has two methods.
func Build(repository ports.SavedViewRepository, now func() time.Time, next func() uint64) (Application, error) {
	return usecase.New(repository, now, next)
}
