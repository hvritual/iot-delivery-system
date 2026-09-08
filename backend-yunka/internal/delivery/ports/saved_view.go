package ports

import (
	"context"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/domain"
)

// SavedViewRepository is the entire persistence authority of this pilot.
// Empty owner retains the existing all-owner ID-collision lookup. It is NOT a
// public list scope: the use case always derives user-facing scope from identity.
// It exposes no work-item/project/planning writes, SQL handles or transaction API.
type SavedViewRepository interface {
	CreateSavedView(context.Context, domain.SavedView) error
	ListSavedViews(context.Context, string) ([]domain.SavedView, error)
}
