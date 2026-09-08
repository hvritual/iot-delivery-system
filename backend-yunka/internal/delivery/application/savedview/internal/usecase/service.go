package usecase

import (
	"context"
	"errors"
	"fmt"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/domain"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/ports"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"strings"
	"time"
)

type service struct {
	repository ports.SavedViewRepository
	now        func() time.Time
	next       func() uint64
}

func New(repository ports.SavedViewRepository, now func() time.Time, next func() uint64) (*service, error) {
	if repository == nil || now == nil || next == nil {
		return nil, errors.New("delivery service is not configured")
	}
	return &service{repository: repository, now: now, next: next}, nil
}

func (s *service) SaveView(ctx context.Context, input domain.SavedViewInput) (domain.SavedView, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.SavedView{}, errors.New("delivery saved view name is required")
	}
	owner, err := canonicalUser(ctx)
	if err != nil {
		return domain.SavedView{}, err
	}
	now := s.now().UTC()
	id, err := s.nextID(ctx, now)
	if err != nil {
		return domain.SavedView{}, err
	}
	view := domain.SavedView{ID: id, Name: name, Owner: owner, Filter: domain.NormalizeWorkItemFilter(input.Filter), CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateSavedView(ctx, view); err != nil {
		return domain.SavedView{}, err
	}
	return view, nil
}

func (s *service) ListSavedViews(ctx context.Context) ([]domain.SavedView, error) {
	owner, err := canonicalUser(ctx)
	if err != nil {
		return nil, err
	}
	return s.repository.ListSavedViews(ctx, owner)
}

func canonicalUser(ctx context.Context) (string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || strings.TrimSpace(principal.UserID) == "" {
		return "", domain.ErrCanonicalUserRequired
	}
	return strings.TrimSpace(principal.UserID), nil
}

func (s *service) nextID(ctx context.Context, now time.Time) (string, error) {
	// Preserve the shared legacy sequence, global collision check and retry bound.
	// This is not a new atomic cross-process ID allocation guarantee.
	for attempt := 0; attempt < 10000; attempt++ {
		id := fmt.Sprintf("VIEW-%s-%04d", now.Format("20060102"), s.next())
		views, err := s.repository.ListSavedViews(ctx, "")
		if err != nil {
			return "", fmt.Errorf("check generated delivery view ID: %w", err)
		}
		exists := false
		for _, v := range views {
			if v.ID == id {
				exists = true
				break
			}
		}
		if !exists {
			return id, nil
		}
	}
	return "", errors.New("could not allocate a delivery view ID")
}
