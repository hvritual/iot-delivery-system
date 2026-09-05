package delivery

import (
	"context"
	"errors"
)

const savedViewSavedEventType = "delivery.saved-view.saved"

// NewRootTransactionalService constructs the delivery service used by the
// runtime root Operation executor. Mutations already staged by Service keep
// their existing behavior; repository-owned mutations that historically had no
// service staging seam are wrapped so they cannot commit without the root
// transaction's outbox event.
func NewRootTransactionalService(repository Repository, exporter Exporter, stager MutationStager) *Service {
	if repository != nil && stager != nil {
		repository = &transactionalRepository{Repository: repository, stager: stager}
	}
	return NewService(repository, exporter, stager)
}

// transactionalRepository delegates the full repository contract and closes
// only the remaining SaveView write bypass. Saved-view staging runs before the
// repository write; both operations therefore use the same transaction handle
// supplied by the root Executor.
type transactionalRepository struct {
	Repository
	stager MutationStager
}

func (repository *transactionalRepository) CreateSavedView(ctx context.Context, view SavedView) error {
	if repository == nil || repository.Repository == nil || repository.stager == nil {
		return errors.New("delivery transactional repository is not configured")
	}
	if err := repository.stager.Stage(ctx, savedViewSavedEventType, WorkItem{ID: view.ID, UpdatedAt: view.UpdatedAt}); err != nil {
		return err
	}
	return repository.Repository.CreateSavedView(ctx, view)
}
