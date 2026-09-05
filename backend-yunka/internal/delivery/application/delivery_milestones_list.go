package application

import (
	"context"
	"errors"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
)

func (adapter *Adapter) ListMilestones(ctx context.Context, request *deliveryv1.ListMilestonesRequest) (*deliveryv1.ListMilestonesResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	values, err := service.ListMilestones(ctx, request.GetProjectId())
	if err != nil {
		return nil, err
	}
	result := make([]*deliveryv1.Milestone, 0, len(values))
	for _, value := range values {
		result = append(result, milestoneToProto(value))
	}
	return &deliveryv1.ListMilestonesResponse{Milestones: result}, nil
}

func (service *auditedDeliveryService) ListMilestones(ctx context.Context, request *deliveryv1.ListMilestonesRequest) (*deliveryv1.ListMilestonesResponse, error) {
	return service.delegate.ListMilestones(ctx, request)
}
