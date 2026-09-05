// Scaffolded by yunka add operation. Developer-owned; safe to edit.
//
// Operation: delivery.items.search
// Application: delivery/management
// Generated interface method: SearchItems
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
)

func (adapter *Adapter) SearchItems(ctx context.Context, request *deliveryv1.SearchItemsRequest) (*deliveryv1.SearchItemsResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	items, err := service.Search(ctx, delivery.WorkItemFilter{
		ProjectID: request.GetProjectId(), Board: delivery.Board(request.GetBoard()), Owner: request.GetOwner(),
		Status: delivery.Status(request.GetStatus()), Kind: delivery.WorkItemKind(request.GetKind()),
		ReleaseID: request.GetReleaseId(), SprintID: request.GetSprintId(), MilestoneID: request.GetMilestoneId(), Query: request.GetQuery(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.SearchItemsResponse{Items: workItems(filterAuthorizedWorkItems(ctx, items))}, nil
}
