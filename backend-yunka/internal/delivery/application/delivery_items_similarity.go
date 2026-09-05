// Scaffolded by yunka add operation. Developer-owned; safe to edit.
//
// Operation: delivery.items.similarity
// Application: delivery/management
// Generated interface method: FindSimilarItems
//
// TODO(agent): after running yunka generate, implement the generated Application
// interface in developer-owned code. Do not edit zz_yunka_* generated files.
// Business rules, persistence, authorization decisions, transaction boundaries,
// idempotency behavior, Saga/Outbox behavior, event publication, and external
// effects must follow the explicit contract and application requirements.
package application

import (
	"context"
	"errors"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
)

func (adapter *Adapter) FindSimilarItems(ctx context.Context, request *deliveryv1.FindSimilarItemsRequest) (*deliveryv1.FindSimilarItemsResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	candidates, err := service.FindSimilar(ctx, delivery.SimilarityQuery{
		Title: request.GetTitle(), Board: delivery.Board(request.GetBoard()), ProjectID: request.GetProjectId(),
		Kind: delivery.WorkItemKind(request.GetKind()), Limit: int(request.GetLimit()),
	})
	if err != nil {
		return nil, err
	}
	projects, restricted := deliveryauthz.AuthorizedProjectsFromContext(ctx)
	result := make([]*deliveryv1.SimilarityCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if restricted && !projects[candidate.ProjectID] {
			continue
		}
		result = append(result, &deliveryv1.SimilarityCandidate{Item: workItem(candidate.WorkItem), Score: candidate.Score, Exact: candidate.Exact})
	}
	return &deliveryv1.FindSimilarItemsResponse{Candidates: result}, nil
}
