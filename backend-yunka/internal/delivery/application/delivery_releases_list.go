package application

import (
	"context"
	"errors"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
)

func (adapter *Adapter) ListReleases(ctx context.Context, request *deliveryv1.ListReleasesRequest) (*deliveryv1.ListReleasesResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	values, err := service.ListReleases(ctx, request.GetProjectId())
	if err != nil {
		return nil, err
	}
	result := make([]*deliveryv1.Release, 0, len(values))
	for _, value := range values {
		result = append(result, releaseToProto(value))
	}
	return &deliveryv1.ListReleasesResponse{Releases: result}, nil
}

func (service *auditedDeliveryService) ListReleases(ctx context.Context, request *deliveryv1.ListReleasesRequest) (*deliveryv1.ListReleasesResponse, error) {
	return service.delegate.ListReleases(ctx, request)
}
