package application

import (
	"context"
	"errors"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
)

func (adapter *Adapter) ListSprints(ctx context.Context, request *deliveryv1.ListSprintsRequest) (*deliveryv1.ListSprintsResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	values, err := service.ListSprints(ctx, request.GetProjectId())
	if err != nil {
		return nil, err
	}
	result := make([]*deliveryv1.Sprint, 0, len(values))
	for _, value := range values {
		result = append(result, sprintToProto(value))
	}
	return &deliveryv1.ListSprintsResponse{Sprints: result}, nil
}

func (service *auditedDeliveryService) ListSprints(ctx context.Context, request *deliveryv1.ListSprintsRequest) (*deliveryv1.ListSprintsResponse, error) {
	return service.delegate.ListSprints(ctx, request)
}
