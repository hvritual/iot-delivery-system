package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var (
	ErrEvidenceRequired      = errors.New("gate advancement requires at least one evidence record")
	ErrInvalidGateTransition = errors.New("invalid delivery gate transition")
	ErrRetrospectiveRequired = errors.New("closing a delivery item requires a retrospective")
	ErrReleaseNotValidated   = errors.New("delivery item must pass production validation before it can close")
)

type Exporter interface {
	Export(context.Context, []WorkItem) error
}

type Service struct {
	repository Repository
	exporter   Exporter
	now        func() time.Time
	sequence   atomic.Uint64
}

func NewService(repository Repository, exporter Exporter) *Service {
	return &Service{repository: repository, exporter: exporter, now: time.Now}
}

func (service *Service) Create(ctx context.Context, input CreateInput) (WorkItem, error) {
	if service == nil || service.repository == nil {
		return WorkItem{}, errors.New("delivery service is not configured")
	}
	if strings.TrimSpace(input.Title) == "" {
		return WorkItem{}, errors.New("delivery item title is required")
	}
	if strings.TrimSpace(string(input.Board)) == "" {
		return WorkItem{}, errors.New("delivery item board is required")
	}
	if strings.TrimSpace(input.Owner) == "" {
		return WorkItem{}, errors.New("delivery item owner is required")
	}
	if input.Priority == "" {
		input.Priority = PriorityP1
	}
	now := service.now().UTC()
	id, err := service.nextID(ctx, now)
	if err != nil {
		return WorkItem{}, err
	}
	item := WorkItem{
		ID:        id,
		Title:     strings.TrimSpace(input.Title),
		Board:     input.Board,
		Type:      strings.TrimSpace(input.Type),
		Owner:     strings.TrimSpace(input.Owner),
		Priority:  input.Priority,
		Status:    StatusPlanned,
		Gate:      GatePlanning,
		DueDate:   strings.TrimSpace(input.DueDate),
		Plan:      strings.TrimSpace(input.Plan),
		Solution:  strings.TrimSpace(input.Solution),
		IsSample:  input.IsSample,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := service.repository.Create(ctx, item); err != nil {
		return WorkItem{}, err
	}
	if err := service.export(ctx); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) AdvanceGate(ctx context.Context, id string, next Gate, evidence []Evidence) (WorkItem, error) {
	if len(evidence) == 0 {
		return WorkItem{}, ErrEvidenceRequired
	}
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if !isNextGate(item.Gate, next) {
		return WorkItem{}, fmt.Errorf("%w: %s -> %s", ErrInvalidGateTransition, item.Gate, next)
	}
	now := service.now().UTC()
	for index := range evidence {
		evidence[index].Kind = strings.TrimSpace(evidence[index].Kind)
		evidence[index].Title = strings.TrimSpace(evidence[index].Title)
		evidence[index].Reference = strings.TrimSpace(evidence[index].Reference)
		if evidence[index].Kind == "" || evidence[index].Title == "" {
			return WorkItem{}, ErrEvidenceRequired
		}
		if evidence[index].RecordedAt.IsZero() {
			evidence[index].RecordedAt = now
		}
	}
	item.Gate = next
	item.Status = StatusInProgress
	if next == GateTestPassed {
		item.Status = StatusVerifying
	}
	if next == GateProductionValidated {
		item.Status = StatusReleased
	}
	item.Evidence = append(item.Evidence, evidence...)
	item.UpdatedAt = now
	if err := service.repository.Save(ctx, item); err != nil {
		return WorkItem{}, err
	}
	if err := service.export(ctx); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) Close(ctx context.Context, id, retrospective string) (WorkItem, error) {
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	if item.Gate != GateProductionValidated {
		return WorkItem{}, ErrReleaseNotValidated
	}
	retrospective = strings.TrimSpace(retrospective)
	if retrospective == "" {
		return WorkItem{}, ErrRetrospectiveRequired
	}
	item.Status = StatusClosed
	item.Retrospective = retrospective
	item.UpdatedAt = service.now().UTC()
	if err := service.repository.Save(ctx, item); err != nil {
		return WorkItem{}, err
	}
	if err := service.export(ctx); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) UpdateContext(ctx context.Context, id string, input ContextUpdate) (WorkItem, error) {
	item, err := service.repository.Get(ctx, id)
	if err != nil {
		return WorkItem{}, err
	}
	now := service.now().UTC()
	if input.Plan != nil {
		item.Plan = strings.TrimSpace(*input.Plan)
	}
	if input.Solution != nil {
		item.Solution = strings.TrimSpace(*input.Solution)
	}
	if input.Blocker != nil {
		item.Blocker = strings.TrimSpace(*input.Blocker)
		if item.Blocker != "" {
			if item.Status == StatusClosed || item.Status == StatusReleased {
				return WorkItem{}, errors.New("released or closed delivery item cannot be blocked")
			}
			item.Status = StatusBlocked
		} else if item.Status == StatusBlocked {
			item.Status = statusForGate(item.Gate)
		}
	}
	if input.Decision != nil {
		decision := *input.Decision
		decision.Title = strings.TrimSpace(decision.Title)
		decision.Context = strings.TrimSpace(decision.Context)
		decision.Outcome = strings.TrimSpace(decision.Outcome)
		decision.Consequences = strings.TrimSpace(decision.Consequences)
		if decision.Title == "" || decision.Outcome == "" {
			return WorkItem{}, errors.New("decision title and outcome are required")
		}
		if strings.TrimSpace(decision.ID) == "" {
			decision.ID = fmt.Sprintf("ADR-%s-%03d", item.ID, len(item.Decisions)+1)
		}
		if decision.CreatedAt.IsZero() {
			decision.CreatedAt = now
		}
		item.Decisions = append(item.Decisions, decision)
	}
	item.UpdatedAt = now
	if err := service.repository.Save(ctx, item); err != nil {
		return WorkItem{}, err
	}
	if err := service.export(ctx); err != nil {
		return WorkItem{}, err
	}
	return item, nil
}

func (service *Service) List(ctx context.Context) ([]WorkItem, error) {
	return service.repository.List(ctx)
}

// Sync refreshes the one-way Obsidian projection from the delivery system's
// database. It never reads or treats generated notes as task state.
func (service *Service) Sync(ctx context.Context) error {
	if service == nil || service.repository == nil {
		return errors.New("delivery service is not configured")
	}
	return service.export(ctx)
}

func (service *Service) export(ctx context.Context) error {
	if service.exporter == nil {
		return nil
	}
	items, err := service.repository.List(ctx)
	if err != nil {
		return err
	}
	return service.exporter.Export(ctx, items)
}

func (service *Service) nextID(ctx context.Context, now time.Time) (string, error) {
	for attempt := 0; attempt < 10_000; attempt++ {
		candidate := fmt.Sprintf("IOT-%s-%04d", now.Format("20060102"), service.sequence.Add(1))
		_, err := service.repository.Get(ctx, candidate)
		switch {
		case errors.Is(err, ErrNotFound):
			return candidate, nil
		case err != nil:
			return "", fmt.Errorf("check generated delivery item ID: %w", err)
		}
	}
	return "", errors.New("could not allocate a delivery item ID")
}

func isNextGate(current, next Gate) bool {
	switch current {
	case GatePlanning:
		return next == GateSolutionReviewed
	case GateSolutionReviewed:
		return next == GateDevelopmentCompleted
	case GateDevelopmentCompleted:
		return next == GateTestPassed
	case GateTestPassed:
		return next == GateProductionValidated
	default:
		return false
	}
}

func statusForGate(gate Gate) Status {
	switch gate {
	case GatePlanning:
		return StatusPlanned
	case GateTestPassed:
		return StatusVerifying
	case GateProductionValidated:
		return StatusReleased
	default:
		return StatusInProgress
	}
}
