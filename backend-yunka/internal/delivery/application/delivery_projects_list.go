package application

import (
	"context"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
)

// ListProjects implements the generated application port. Scope is derived by
// the operation guard from the authenticated principal and durable grants; the
// request intentionally contains no caller-controlled tenant or project ID.
func (adapter *Adapter) ListProjects(ctx context.Context, _ *deliveryv1.ListProjectsRequest) (*deliveryv1.ListProjectsResponse, error) {
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	projects, err := service.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	return &deliveryv1.ListProjectsResponse{Projects: projectsToProto(filterAuthorizedProjects(ctx, projects))}, nil
}

func (service *auditedDeliveryService) ListProjects(ctx context.Context, request *deliveryv1.ListProjectsRequest) (*deliveryv1.ListProjectsResponse, error) {
	return service.delegate.ListProjects(ctx, request)
}

func filterAuthorizedProjects(ctx context.Context, projects []delivery.Project) []delivery.Project {
	authorized, restricted := deliveryauthz.AuthorizedProjectsFromContext(ctx)
	if !restricted {
		return projects
	}
	filtered := make([]delivery.Project, 0, len(projects))
	for _, project := range projects {
		if authorized[project.ID] {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

func projectsToProto(projects []delivery.Project) []*deliveryv1.Project {
	result := make([]*deliveryv1.Project, 0, len(projects))
	for _, project := range projects {
		result = append(result, projectToProto(project))
	}
	return result
}
