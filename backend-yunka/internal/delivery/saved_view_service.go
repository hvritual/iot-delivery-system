package delivery

import (
	"context"
	"errors"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application/savedview"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
)

// The legacy facade preserves callers and the shared ID sequence. Validation,
// normalization and ID-collision rules now belong only to the narrow use case.
func (service *Service) SaveView(ctx context.Context, input SavedViewInput) (SavedView, error) {
	var views savedview.Application
	var err error
	views, err = service.savedViewUseCase()
	if err != nil {
		return SavedView{}, err
	}
	return views.SaveView(ctx, input)
}
func (service *Service) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	var views savedview.Application
	var err error
	views, err = service.savedViewUseCase()
	if err != nil {
		return nil, err
	}
	return views.ListSavedViews(ctx)
}
func (service *Service) savedViewUseCase() (savedview.Application, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("delivery service is not configured")
	}
	sequence := &service.sequence
	return savedview.Build(newSavedViewRepository(service.repository), service.now, func() uint64 { return sequence.Add(1) })
}

// Do not embed Repository or return it as a small interface: that would retain
// its 22-method dynamic surface. Only two explicit bound functions cross this
// seam, including the existing transactional wrapper's Stage-before-write call.
type savedViewRepository struct {
	create func(context.Context, SavedView) error
	list   func(context.Context, string) ([]SavedView, error)
}

func newSavedViewRepository(repository Repository) ports.SavedViewRepository {
	if repository == nil {
		return nil
	}
	return savedViewRepository{create: repository.CreateSavedView, list: repository.ListSavedViews}
}
func (r savedViewRepository) CreateSavedView(ctx context.Context, v SavedView) error {
	return r.create(ctx, v)
}
func (r savedViewRepository) ListSavedViews(ctx context.Context, owner string) ([]SavedView, error) {
	return r.list(ctx, owner)
}
