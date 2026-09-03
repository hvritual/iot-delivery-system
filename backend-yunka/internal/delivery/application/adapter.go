package application

import (
	"context"
	"errors"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"google.golang.org/protobuf/types/known/timestamppb"
	"yunka.io/gateway/authz"
)

// Adapter is the explicit, handwritten application implementation injected
// into Yunka's generated DeliveryService port. Business rules remain in the
// delivery service; the generated RPC executor owns policy and transactions.
type Adapter struct {
	service *delivery.Service
}

func NewAdapter(service *delivery.Service) *Adapter {
	return &Adapter{service: service}
}

func (adapter *Adapter) GetDashboard(ctx context.Context, _ *deliveryv1.GetDashboardRequest) (*deliveryv1.GetDashboardResponse, error) {
	items, err := adapter.list(ctx)
	if err != nil {
		return nil, err
	}
	return &deliveryv1.GetDashboardResponse{Dashboard: &deliveryv1.Dashboard{
		Boards:      boardSummaries(items),
		Items:       workItems(items),
		GeneratedAt: timestamppb.New(time.Now().UTC()),
	}}, nil
}

func (adapter *Adapter) ListItems(ctx context.Context, _ *deliveryv1.ListItemsRequest) (*deliveryv1.ListItemsResponse, error) {
	items, err := adapter.list(ctx)
	if err != nil {
		return nil, err
	}
	return &deliveryv1.ListItemsResponse{Items: workItems(items)}, nil
}

func (adapter *Adapter) CreateItem(ctx context.Context, request *deliveryv1.CreateItemRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	item, err := service.Create(ctx, delivery.CreateInput{
		Title:     request.GetTitle(),
		Board:     delivery.Board(request.GetBoard()),
		Type:      request.GetType(),
		Owner:     request.GetOwner(),
		Priority:  delivery.Priority(request.GetPriority()),
		DueDate:   request.GetDueDate(),
		Plan:      request.GetPlan(),
		Solution:  request.GetSolution(),
		IsSample:  request.GetIsSample(),
		ProjectID: request.GetProjectId(), ParentID: request.GetParentId(), Kind: delivery.WorkItemKind(request.GetKind()), ReleaseID: request.GetReleaseId(), SprintID: request.GetSprintId(), MilestoneID: request.GetMilestoneId(), StartDate: request.GetStartDate(), EstimatePoints: request.GetEstimatePoints(), ProgressPercent: int(request.GetProgressPercent()),
		Dependencies: dependenciesFromProto(request.GetDependencies()), IoTBindings: bindingsFromProto(request.GetIotBindings()), TraceLinks: traceLinksFromProto(request.GetTraceLinks()),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}

func (adapter *Adapter) UpdateItem(ctx context.Context, request *deliveryv1.UpdateItemRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	update := delivery.WorkItemUpdate{}
	for _, field := range request.GetUpdateMask() {
		switch field {
		case "title":
			value := request.GetTitle()
			update.Title = &value
		case "owner":
			value := request.GetOwner()
			update.Owner = &value
		case "priority":
			value := delivery.Priority(request.GetPriority())
			update.Priority = &value
		case "release_id":
			value := request.GetReleaseId()
			update.ReleaseID = &value
		case "sprint_id":
			value := request.GetSprintId()
			update.SprintID = &value
		case "milestone_id":
			value := request.GetMilestoneId()
			update.MilestoneID = &value
		case "start_date":
			value := request.GetStartDate()
			update.StartDate = &value
		case "due_date":
			value := request.GetDueDate()
			update.DueDate = &value
		case "estimate_points":
			value := request.GetEstimatePoints()
			update.EstimatePoints = &value
		case "progress_percent":
			value := int(request.GetProgressPercent())
			update.ProgressPercent = &value
		case "dependencies":
			value := dependenciesFromProto(request.GetDependencies())
			update.Dependencies = &value
		case "iot_bindings":
			value := bindingsFromProto(request.GetIotBindings())
			update.IoTBindings = &value
		case "trace_links":
			value := traceLinksFromProto(request.GetTraceLinks())
			update.TraceLinks = &value
		default:
			return nil, errors.New("unsupported delivery work item update field: " + field)
		}
	}
	item, err := service.UpdateWorkItem(ctx, request.GetId(), update)
	if err != nil {
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}

func dependenciesFromProto(values []*deliveryv1.WorkItemDependency) []delivery.WorkItemDependency {
	result := make([]delivery.WorkItemDependency, 0, len(values))
	for _, v := range values {
		if v != nil {
			result = append(result, delivery.WorkItemDependency{ItemID: v.GetItemId(), Relation: delivery.DependencyRelation(v.GetRelation())})
		}
	}
	return result
}
func bindingsFromProto(values []*deliveryv1.IoTBinding) []delivery.IoTBinding {
	result := make([]delivery.IoTBinding, 0, len(values))
	for _, v := range values {
		if v != nil {
			attrs := map[string]string{}
			for k, x := range v.GetAttributes() {
				attrs[k] = x
			}
			result = append(result, delivery.IoTBinding{Kind: delivery.IoTBindingKind(v.GetKind()), Reference: v.GetReference(), Label: v.GetLabel(), Attributes: attrs})
		}
	}
	return result
}
func traceLinksFromProto(values []*deliveryv1.TraceLink) []delivery.TraceLink {
	result := make([]delivery.TraceLink, 0, len(values))
	for _, v := range values {
		if v != nil {
			result = append(result, delivery.TraceLink{Kind: delivery.TraceKind(v.GetKind()), Reference: v.GetReference(), Title: v.GetTitle(), URL: v.GetUrl(), Status: v.GetStatus(), RecordedAt: timeFromProto(v.GetRecordedAt())})
		}
	}
	return result
}

func (adapter *Adapter) CreateItemComment(ctx context.Context, request *deliveryv1.CreateItemCommentRequest) (*deliveryv1.CommentResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	comment, err := service.AddComment(ctx, request.GetId(), delivery.CommentInput{Body: request.GetBody()})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.CommentResponse{Comment: &deliveryv1.Comment{Id: comment.ID, Body: comment.Body, Author: comment.Author, CreatedAt: timestamp(comment.CreatedAt)}}, nil
}

func (adapter *Adapter) CreateProject(ctx context.Context, request *deliveryv1.CreateProjectRequest) (*deliveryv1.ProjectResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	project, err := service.CreateProject(ctx, delivery.ProjectInput{
		OrganizationID: deliveryauthz.OrganizationIDFromContext(ctx),
		Name:           request.GetName(),
		Board:          delivery.Board(request.GetBoard()),
		Owner:          request.GetOwner(),
		Description:    request.GetDescription(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.ProjectResponse{Project: projectToProto(project)}, nil
}

func (adapter *Adapter) CreateRelease(ctx context.Context, request *deliveryv1.CreateReleaseRequest) (*deliveryv1.ReleaseResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	release, err := service.CreateRelease(ctx, delivery.ReleaseInput{
		ProjectID:   request.GetProjectId(),
		Name:        request.GetName(),
		Version:     request.GetVersion(),
		TargetDate:  request.GetTargetDate(),
		Status:      request.GetStatus(),
		Description: request.GetDescription(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.ReleaseResponse{Release: releaseToProto(release)}, nil
}

func (adapter *Adapter) CreateSprint(ctx context.Context, request *deliveryv1.CreateSprintRequest) (*deliveryv1.SprintResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	sprint, err := service.CreateSprint(ctx, delivery.SprintInput{
		ProjectID: request.GetProjectId(),
		Name:      request.GetName(),
		Goal:      request.GetGoal(),
		StartDate: request.GetStartDate(),
		EndDate:   request.GetEndDate(),
		Status:    request.GetStatus(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.SprintResponse{Sprint: sprintToProto(sprint)}, nil
}

func (adapter *Adapter) CreateMilestone(ctx context.Context, request *deliveryv1.CreateMilestoneRequest) (*deliveryv1.MilestoneResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	milestone, err := service.CreateMilestone(ctx, delivery.MilestoneInput{
		ProjectID:   request.GetProjectId(),
		Name:        request.GetName(),
		TargetDate:  request.GetTargetDate(),
		Status:      request.GetStatus(),
		Description: request.GetDescription(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.MilestoneResponse{Milestone: milestoneToProto(milestone)}, nil
}

func (adapter *Adapter) UpdateItemContext(ctx context.Context, request *deliveryv1.UpdateItemContextRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	input := delivery.ContextUpdate{Plan: request.Plan, Solution: request.Solution, Blocker: request.Blocker}
	if request.Decision != nil {
		input.Decision = decisionFromProto(request.Decision)
	}
	item, err := service.UpdateContext(ctx, request.GetId(), input)
	if err != nil {
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}

func (adapter *Adapter) AdvanceGate(ctx context.Context, request *deliveryv1.AdvanceGateRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	evidence := make([]delivery.Evidence, 0, len(request.GetEvidence()))
	for _, record := range request.GetEvidence() {
		if record == nil {
			continue
		}
		evidence = append(evidence, delivery.Evidence{
			Kind:       record.GetKind(),
			Title:      record.GetTitle(),
			Reference:  record.GetReference(),
			RecordedAt: timeFromProto(record.GetRecordedAt()),
		})
	}
	item, err := service.AdvanceGate(ctx, request.GetId(), delivery.Gate(request.GetGate()), evidence)
	if err != nil {
		return nil, normalizeAuthorizationError(err)
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}

func (adapter *Adapter) CloseItem(ctx context.Context, request *deliveryv1.CloseItemRequest) (*deliveryv1.WorkItemResponse, error) {
	if request == nil {
		return nil, errors.New("request is required")
	}
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	item, err := service.Close(ctx, request.GetId(), request.GetRetrospective())
	if err != nil {
		return nil, normalizeAuthorizationError(err)
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
}

// normalizeAuthorizationError retains domain sentinel matching while marking
// high-risk separation-of-duties denials for the shared transport adapters.
func normalizeAuthorizationError(err error) error {
	if !errors.Is(err, delivery.ErrProductionPrincipalRequired) &&
		!errors.Is(err, delivery.ErrImplementationSourceRequired) &&
		!errors.Is(err, delivery.ErrImplementerCannotVerifyOwnChange) {
		return err
	}
	return errors.Join(authz.Denied(authz.Decision{Reason: authz.ReasonPermissionDenied}), err)
}

func (adapter *Adapter) deliveryService() (*delivery.Service, error) {
	if adapter == nil || adapter.service == nil {
		return nil, errors.New("delivery application is not configured")
	}
	return adapter.service, nil
}

func (adapter *Adapter) list(ctx context.Context) ([]delivery.WorkItem, error) {
	service, err := adapter.deliveryService()
	if err != nil {
		return nil, err
	}
	items, err := service.List(ctx)
	if err != nil {
		return nil, err
	}
	projects, restricted := deliveryauthz.AuthorizedProjectsFromContext(ctx)
	if !restricted {
		return items, nil
	}
	result := make([]delivery.WorkItem, 0, len(items))
	for _, item := range items {
		if projects[item.ProjectID] {
			result = append(result, item)
		}
	}
	return result, nil
}

func workItems(items []delivery.WorkItem) []*deliveryv1.WorkItem {
	result := make([]*deliveryv1.WorkItem, 0, len(items))
	for _, item := range items {
		result = append(result, workItem(item))
	}
	return result
}

func workItem(item delivery.WorkItem) *deliveryv1.WorkItem {
	decisions := make([]*deliveryv1.Decision, 0, len(item.Decisions))
	for _, decision := range item.Decisions {
		decisions = append(decisions, &deliveryv1.Decision{
			Id:           decision.ID,
			Title:        decision.Title,
			Context:      decision.Context,
			Outcome:      decision.Outcome,
			Consequences: decision.Consequences,
			CreatedAt:    timestamp(decision.CreatedAt),
		})
	}
	evidence := make([]*deliveryv1.Evidence, 0, len(item.Evidence))
	for _, record := range item.Evidence {
		evidence = append(evidence, &deliveryv1.Evidence{
			Kind:       record.Kind,
			Title:      record.Title,
			Reference:  record.Reference,
			RecordedAt: timestamp(record.RecordedAt),
		})
	}
	dependencies := make([]*deliveryv1.WorkItemDependency, 0, len(item.Dependencies))
	for _, value := range item.Dependencies {
		dependencies = append(dependencies, &deliveryv1.WorkItemDependency{ItemId: value.ItemID, Relation: string(value.Relation)})
	}
	bindings := make([]*deliveryv1.IoTBinding, 0, len(item.IoTBindings))
	for _, value := range item.IoTBindings {
		attributes := map[string]string{}
		for key, attribute := range value.Attributes {
			attributes[key] = attribute
		}
		bindings = append(bindings, &deliveryv1.IoTBinding{Kind: string(value.Kind), Reference: value.Reference, Label: value.Label, Attributes: attributes})
	}
	traces := make([]*deliveryv1.TraceLink, 0, len(item.TraceLinks))
	for _, value := range item.TraceLinks {
		traces = append(traces, &deliveryv1.TraceLink{Kind: string(value.Kind), Reference: value.Reference, Title: value.Title, Url: value.URL, Status: value.Status, RecordedAt: timestamp(value.RecordedAt)})
	}
	comments := make([]*deliveryv1.Comment, 0, len(item.Comments))
	for _, value := range item.Comments {
		comments = append(comments, &deliveryv1.Comment{Id: value.ID, Body: value.Body, Author: value.Author, CreatedAt: timestamp(value.CreatedAt)})
	}
	activities := make([]*deliveryv1.Activity, 0, len(item.Activities))
	for _, value := range item.Activities {
		activities = append(activities, &deliveryv1.Activity{Id: value.ID, Type: value.Type, Summary: value.Summary, Actor: value.Actor, OccurredAt: timestamp(value.OccurredAt)})
	}
	return &deliveryv1.WorkItem{
		Id:            item.ID,
		Title:         item.Title,
		Board:         string(item.Board),
		Type:          item.Type,
		Owner:         item.Owner,
		Priority:      string(item.Priority),
		Status:        string(item.Status),
		Gate:          string(item.Gate),
		DueDate:       item.DueDate,
		Plan:          item.Plan,
		Solution:      item.Solution,
		Decisions:     decisions,
		Evidence:      evidence,
		Retrospective: item.Retrospective,
		Blocker:       item.Blocker,
		IsSample:      item.IsSample,
		CreatedAt:     timestamp(item.CreatedAt),
		UpdatedAt:     timestamp(item.UpdatedAt),
		ProjectId:     item.ProjectID, ParentId: item.ParentID, Kind: string(item.Kind), Dependencies: dependencies, ReleaseId: item.ReleaseID, SprintId: item.SprintID, MilestoneId: item.MilestoneID, StartDate: item.StartDate, EstimatePoints: item.EstimatePoints, ProgressPercent: int32(item.ProgressPercent), IotBindings: bindings, TraceLinks: traces, Comments: comments, Activities: activities,
	}
}

func projectToProto(value delivery.Project) *deliveryv1.Project {
	return &deliveryv1.Project{Id: value.ID, Name: value.Name, Board: string(value.Board), Owner: value.Owner, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Description: value.Description}
}

func releaseToProto(value delivery.Release) *deliveryv1.Release {
	return &deliveryv1.Release{Id: value.ID, ProjectId: value.ProjectID, Name: value.Name, Version: value.Version, TargetDate: value.TargetDate, Status: value.Status, Description: value.Description, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt)}
}

func sprintToProto(value delivery.Sprint) *deliveryv1.Sprint {
	return &deliveryv1.Sprint{Id: value.ID, ProjectId: value.ProjectID, Name: value.Name, Goal: value.Goal, StartDate: value.StartDate, EndDate: value.EndDate, Status: value.Status, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt)}
}

func milestoneToProto(value delivery.Milestone) *deliveryv1.Milestone {
	return &deliveryv1.Milestone{Id: value.ID, ProjectId: value.ProjectID, Name: value.Name, TargetDate: value.TargetDate, Status: value.Status, Description: value.Description, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt)}
}

func decisionFromProto(value *deliveryv1.Decision) *delivery.Decision {
	if value == nil {
		return nil
	}
	return &delivery.Decision{
		ID:           value.GetId(),
		Title:        value.GetTitle(),
		Context:      value.GetContext(),
		Outcome:      value.GetOutcome(),
		Consequences: value.GetConsequences(),
		CreatedAt:    timeFromProto(value.GetCreatedAt()),
	}
}

func boardSummaries(items []delivery.WorkItem) []*deliveryv1.BoardSummary {
	boards := []delivery.Board{
		delivery.BoardDeviceQuality,
		delivery.BoardProductPlatform,
		delivery.BoardResearchDelivery,
		delivery.BoardOperations,
		delivery.BoardCustomerValue,
	}
	summaries := make(map[delivery.Board]*deliveryv1.BoardSummary, len(boards))
	for _, board := range boards {
		summaries[board] = &deliveryv1.BoardSummary{Board: string(board)}
	}
	for _, item := range items {
		summary, exists := summaries[item.Board]
		if !exists {
			summary = &deliveryv1.BoardSummary{Board: string(item.Board)}
			summaries[item.Board] = summary
		}
		summary.Total++
		switch item.Status {
		case delivery.StatusBlocked:
			summary.Blocked++
		case delivery.StatusVerifying:
			summary.Verifying++
		case delivery.StatusReleased:
			summary.Released++
		case delivery.StatusClosed:
			summary.Closed++
		default:
			summary.Active++
		}
	}
	result := make([]*deliveryv1.BoardSummary, 0, len(boards))
	for _, board := range boards {
		result = append(result, summaries[board])
	}
	return result
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timeFromProto(value *timestamppb.Timestamp) time.Time {
	if value == nil || !value.IsValid() {
		return time.Time{}
	}
	return value.AsTime()
}
