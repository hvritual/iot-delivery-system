// Scaffolded by yunka add operation. Developer-owned; safe to edit.
//
// Operation: delivery.items.get
// Application: delivery/management
// Generated interface method: GetItem
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
)

func (adapter *Adapter) GetItem(ctx context.Context, request *deliveryv1.GetItemRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	item, err := service.Get(ctx, request.GetId())
	if err != nil {
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}
