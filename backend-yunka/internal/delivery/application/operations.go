package application

import (
	"context"
	"errors"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/deliveryauthz"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"yunka.io/framework/operation"
	"yunka.io/pkg/operationplan"
)

// Operations is the explicit local application assembly used by the legacy
// HTTP adapter. It reuses the generated operation plans and executor, so HTTP
// and generated gRPC share the same security and transaction boundary.
type Operations struct {
	application   DeliveryService
	service       *delivery.Service
	notifications notification.Reader
	executor      operation.Executor
}

func NewOperations(application DeliveryService, executor operation.Executor, services ...*delivery.Service) *Operations {
	var service *delivery.Service
	for _, candidate := range services {
		if candidate != nil {
			service = candidate
			break
		}
	}
	return &Operations{application: application, service: service, executor: executor}
}

// WithNotificationReader attaches the durable, local notification read model
// without coupling the generated delivery application to a transport adapter.
func (operations *Operations) WithNotificationReader(reader notification.Reader) *Operations {
	if operations != nil {
		operations.notifications = reader
	}
	return operations
}

func (operations *Operations) Dashboard(ctx context.Context) ([]delivery.WorkItem, error) {
	if operations != nil && operations.service != nil {
		return operations.listExtendedWorkItems(ctx, "delivery.dashboard.get", "get_dashboard")
	}
	if err := operations.ready(); err != nil {
		return nil, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanGetDashboard(), &deliveryv1.GetDashboardRequest{}, operations.application.GetDashboard)
	if err != nil {
		return nil, err
	}
	if response == nil || response.Dashboard == nil {
		return nil, nil
	}
	return workItemsFromProto(response.Dashboard.GetItems()), nil
}

func (operations *Operations) List(ctx context.Context) ([]delivery.WorkItem, error) {
	if operations != nil && operations.service != nil {
		return operations.listExtendedWorkItems(ctx, "delivery.items.list", "list_items")
	}
	if err := operations.ready(); err != nil {
		return nil, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanListItems(), &deliveryv1.ListItemsRequest{}, operations.application.ListItems)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, nil
	}
	return workItemsFromProto(response.GetItems()), nil
}

func (operations *Operations) listExtendedWorkItems(ctx context.Context, operationID, useCase string) ([]delivery.WorkItem, error) {
	if err := operations.extensionReady(); err != nil {
		return nil, err
	}
	value, err := operations.executor.Execute(ctx, extensionPlan(operationID, useCase, "delivery.items.read", "read_only"), nil, func(callContext context.Context) (any, error) {
		return operations.service.List(callContext)
	})
	if err != nil {
		return nil, err
	}
	items, ok := value.([]delivery.WorkItem)
	if !ok {
		return nil, errors.New("delivery work item list operation returned an unexpected result")
	}
	return filterAuthorizedWorkItems(ctx, items), nil
}

func filterAuthorizedWorkItems(ctx context.Context, items []delivery.WorkItem) []delivery.WorkItem {
	projects, restricted := deliveryauthz.AuthorizedProjectsFromContext(ctx)
	if !restricted {
		return items
	}
	filtered := make([]delivery.WorkItem, 0, len(items))
	for _, item := range items {
		if projects[item.ProjectID] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (operations *Operations) Create(ctx context.Context, input delivery.CreateInput) (delivery.WorkItem, error) {
	if err := operations.ready(); err != nil {
		return delivery.WorkItem{}, err
	}
	if input.ProgressPercent < 0 || input.ProgressPercent > 100 {
		return delivery.WorkItem{}, errors.New("delivery progress percent must be between 0 and 100")
	}
	request := &deliveryv1.CreateItemRequest{
		Title:    input.Title,
		Board:    string(input.Board),
		Type:     input.Type,
		Owner:    input.Owner,
		Priority: string(input.Priority),
		DueDate:  input.DueDate,
		Plan:     input.Plan,
		Solution: input.Solution,
		IsSample: input.IsSample, ProjectId: input.ProjectID, ParentId: input.ParentID, Kind: string(input.Kind), ReleaseId: input.ReleaseID, SprintId: input.SprintID, MilestoneId: input.MilestoneID, StartDate: input.StartDate, EstimatePoints: input.EstimatePoints, ProgressPercent: int32(input.ProgressPercent),
	}
	for _, v := range input.Dependencies {
		request.Dependencies = append(request.Dependencies, &deliveryv1.WorkItemDependency{ItemId: v.ItemID, Relation: string(v.Relation)})
	}
	for _, v := range input.IoTBindings {
		request.IotBindings = append(request.IotBindings, &deliveryv1.IoTBinding{Kind: string(v.Kind), Reference: v.Reference, Label: v.Label, Attributes: v.Attributes})
	}
	for _, v := range input.TraceLinks {
		request.TraceLinks = append(request.TraceLinks, &deliveryv1.TraceLink{Kind: string(v.Kind), Reference: v.Reference, Title: v.Title, Url: v.URL, Status: v.Status, RecordedAt: timestamp(v.RecordedAt)})
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateItem(), request, operations.application.CreateItem)
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return workItemFromProto(response.GetItem()), nil
}

func (operations *Operations) UpdateContext(ctx context.Context, id string, expectedRevision int64, input delivery.ContextUpdate) (delivery.WorkItem, error) {
	if err := operations.ready(); err != nil {
		return delivery.WorkItem{}, err
	}
	request := &deliveryv1.UpdateItemContextRequest{Id: id, ExpectedRevision: expectedRevision, Plan: input.Plan, Solution: input.Solution, Blocker: input.Blocker}
	if input.Decision != nil {
		request.Decision = decisionToProto(*input.Decision)
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanUpdateItemContext(), request, operations.application.UpdateItemContext)
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return workItemFromProto(response.GetItem()), nil
}

func (operations *Operations) AdvanceGate(ctx context.Context, id string, expectedRevision int64, next delivery.Gate, evidence []delivery.Evidence) (delivery.WorkItem, error) {
	if err := operations.ready(); err != nil {
		return delivery.WorkItem{}, err
	}
	records := make([]*deliveryv1.Evidence, 0, len(evidence))
	for _, record := range evidence {
		records = append(records, &deliveryv1.Evidence{
			Kind:       record.Kind,
			Title:      record.Title,
			Reference:  record.Reference,
			RecordedAt: timestamp(record.RecordedAt),
		})
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanAdvanceGate(), &deliveryv1.AdvanceGateRequest{
		Id: id, ExpectedRevision: expectedRevision, Gate: string(next), Evidence: records,
	}, operations.application.AdvanceGate)
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return workItemFromProto(response.GetItem()), nil
}

func (operations *Operations) Close(ctx context.Context, id string, expectedRevision int64, retrospective string) (delivery.WorkItem, error) {
	if err := operations.ready(); err != nil {
		return delivery.WorkItem{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCloseItem(), &deliveryv1.CloseItemRequest{
		Id: id, ExpectedRevision: expectedRevision, Retrospective: retrospective,
	}, operations.application.CloseItem)
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return workItemFromProto(response.GetItem()), nil
}

func (operations *Operations) CreateProject(ctx context.Context, input delivery.ProjectInput) (delivery.Project, error) {
	if err := operations.ready(); err != nil {
		return delivery.Project{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateProject(), &deliveryv1.CreateProjectRequest{Name: input.Name, Board: string(input.Board), Owner: input.Owner, Description: input.Description}, operations.application.CreateProject)
	if err != nil {
		return delivery.Project{}, err
	}
	return projectFromProto(response.GetProject()), nil
}

func (operations *Operations) ListProjects(ctx context.Context) ([]delivery.Project, error) {
	if err := operations.extensionReady(); err != nil {
		return nil, err
	}
	value, err := operations.executor.Execute(ctx, extensionPlan("delivery.projects.list", "list_projects", "delivery.items.read", "read_only"), nil, func(callContext context.Context) (any, error) {
		return operations.service.ListProjects(callContext)
	})
	if err != nil {
		return nil, err
	}
	projects, ok := value.([]delivery.Project)
	if !ok {
		return nil, errors.New("delivery project list operation returned an unexpected result")
	}
	return projects, nil
}

func (operations *Operations) FindSimilar(ctx context.Context, query delivery.SimilarityQuery) ([]delivery.SimilarityCandidate, error) {
	if err := operations.extensionReady(); err != nil {
		return nil, err
	}
	value, err := operations.executor.Execute(ctx, extensionPlan("delivery.items.similarity", "find_similar_items", "delivery.items.read", "read_only"), query, func(callContext context.Context) (any, error) {
		return operations.service.FindSimilar(callContext, query)
	})
	if err != nil {
		return nil, err
	}
	candidates, ok := value.([]delivery.SimilarityCandidate)
	if !ok {
		return nil, errors.New("delivery similarity operation returned an unexpected result")
	}
	return candidates, nil
}

func (operations *Operations) Get(ctx context.Context, id string) (delivery.WorkItem, error) {
	return executeServiceExtension(operations, ctx, "delivery.items.get", "get_item", "delivery.items.read", "read_only", id, func(callContext context.Context) (delivery.WorkItem, error) {
		return operations.service.Get(callContext, id)
	})
}

func (operations *Operations) UpdateWorkItem(ctx context.Context, id string, expectedRevision int64, input delivery.WorkItemUpdate) (delivery.WorkItem, error) {
	if err := operations.ready(); err != nil {
		return delivery.WorkItem{}, err
	}
	request := &deliveryv1.UpdateItemRequest{Id: id, ExpectedRevision: expectedRevision}
	if input.Title != nil {
		request.UpdateMask = append(request.UpdateMask, "title")
		request.Title = *input.Title
	}
	if input.Owner != nil {
		request.UpdateMask = append(request.UpdateMask, "owner")
		request.Owner = *input.Owner
	}
	if input.ProgressPercent != nil {
		if *input.ProgressPercent < 0 || *input.ProgressPercent > 100 {
			return delivery.WorkItem{}, errors.New("delivery progress percent must be between 0 and 100")
		}
		request.UpdateMask = append(request.UpdateMask, "progress_percent")
		request.ProgressPercent = int32(*input.ProgressPercent)
	}
	if input.Priority != nil {
		request.UpdateMask = append(request.UpdateMask, "priority")
		request.Priority = string(*input.Priority)
	}
	if input.ReleaseID != nil {
		request.UpdateMask = append(request.UpdateMask, "release_id")
		request.ReleaseId = *input.ReleaseID
	}
	if input.SprintID != nil {
		request.UpdateMask = append(request.UpdateMask, "sprint_id")
		request.SprintId = *input.SprintID
	}
	if input.MilestoneID != nil {
		request.UpdateMask = append(request.UpdateMask, "milestone_id")
		request.MilestoneId = *input.MilestoneID
	}
	if input.StartDate != nil {
		request.UpdateMask = append(request.UpdateMask, "start_date")
		request.StartDate = *input.StartDate
	}
	if input.DueDate != nil {
		request.UpdateMask = append(request.UpdateMask, "due_date")
		request.DueDate = *input.DueDate
	}
	if input.EstimatePoints != nil {
		request.UpdateMask = append(request.UpdateMask, "estimate_points")
		request.EstimatePoints = *input.EstimatePoints
	}
	if input.Dependencies != nil {
		request.UpdateMask = append(request.UpdateMask, "dependencies")
		for _, v := range *input.Dependencies {
			request.Dependencies = append(request.Dependencies, &deliveryv1.WorkItemDependency{ItemId: v.ItemID, Relation: string(v.Relation)})
		}
	}
	if input.IoTBindings != nil {
		request.UpdateMask = append(request.UpdateMask, "iot_bindings")
		for _, v := range *input.IoTBindings {
			request.IotBindings = append(request.IotBindings, &deliveryv1.IoTBinding{Kind: string(v.Kind), Reference: v.Reference, Label: v.Label, Attributes: v.Attributes})
		}
	}
	if input.TraceLinks != nil {
		request.UpdateMask = append(request.UpdateMask, "trace_links")
		for _, v := range *input.TraceLinks {
			request.TraceLinks = append(request.TraceLinks, &deliveryv1.TraceLink{Kind: string(v.Kind), Reference: v.Reference, Title: v.Title, Url: v.URL, Status: v.Status, RecordedAt: timestamp(v.RecordedAt)})
		}
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanUpdateItem(), request, operations.application.UpdateItem)
	if err != nil {
		return delivery.WorkItem{}, err
	}
	return workItemFromProto(response.GetItem()), nil
}

func (operations *Operations) AddComment(ctx context.Context, id string, expectedRevision int64, input delivery.CommentInput) (delivery.Comment, error) {
	if err := operations.ready(); err != nil {
		return delivery.Comment{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateItemComment(), &deliveryv1.CreateItemCommentRequest{Id: id, Body: input.Body, ExpectedRevision: expectedRevision}, operations.application.CreateItemComment)
	if err != nil {
		return delivery.Comment{}, err
	}
	value := response.GetComment()
	return delivery.Comment{ID: value.GetId(), Body: value.GetBody(), Author: value.GetAuthor(), CreatedAt: timeFromProto(value.GetCreatedAt()), WorkItemRevision: response.GetRevision()}, nil
}

func (operations *Operations) Search(ctx context.Context, filter delivery.WorkItemFilter) ([]delivery.WorkItem, error) {
	return executeServiceExtension(operations, ctx, "delivery.items.search", "search_items", "delivery.items.read", "read_only", filter, func(callContext context.Context) ([]delivery.WorkItem, error) {
		return operations.service.Search(callContext, filter)
	})
}

func (operations *Operations) CreateRelease(ctx context.Context, input delivery.ReleaseInput) (delivery.Release, error) {
	if err := operations.ready(); err != nil {
		return delivery.Release{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateRelease(), &deliveryv1.CreateReleaseRequest{ProjectId: input.ProjectID, Name: input.Name, Version: input.Version, TargetDate: input.TargetDate, Status: input.Status, Description: input.Description}, operations.application.CreateRelease)
	if err != nil {
		return delivery.Release{}, err
	}
	return releaseFromProto(response.GetRelease()), nil
}

func (operations *Operations) ListReleases(ctx context.Context, projectID string) ([]delivery.Release, error) {
	return executeServiceExtension(operations, ctx, "delivery.releases.list", "list_releases", "delivery.items.read", "read_only", projectID, func(callContext context.Context) ([]delivery.Release, error) {
		return operations.service.ListReleases(callContext, projectID)
	})
}

func (operations *Operations) CreateSprint(ctx context.Context, input delivery.SprintInput) (delivery.Sprint, error) {
	if err := operations.ready(); err != nil {
		return delivery.Sprint{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateSprint(), &deliveryv1.CreateSprintRequest{ProjectId: input.ProjectID, Name: input.Name, Goal: input.Goal, StartDate: input.StartDate, EndDate: input.EndDate, Status: input.Status}, operations.application.CreateSprint)
	if err != nil {
		return delivery.Sprint{}, err
	}
	return sprintFromProto(response.GetSprint()), nil
}

func (operations *Operations) ListSprints(ctx context.Context, projectID string) ([]delivery.Sprint, error) {
	return executeServiceExtension(operations, ctx, "delivery.sprints.list", "list_sprints", "delivery.items.read", "read_only", projectID, func(callContext context.Context) ([]delivery.Sprint, error) {
		return operations.service.ListSprints(callContext, projectID)
	})
}

func (operations *Operations) CreateMilestone(ctx context.Context, input delivery.MilestoneInput) (delivery.Milestone, error) {
	if err := operations.ready(); err != nil {
		return delivery.Milestone{}, err
	}
	response, err := operation.ExecuteTyped(ctx, operations.executor, policy.OperationPlanCreateMilestone(), &deliveryv1.CreateMilestoneRequest{ProjectId: input.ProjectID, Name: input.Name, TargetDate: input.TargetDate, Status: input.Status, Description: input.Description}, operations.application.CreateMilestone)
	if err != nil {
		return delivery.Milestone{}, err
	}
	return milestoneFromProto(response.GetMilestone()), nil
}

func (operations *Operations) ListMilestones(ctx context.Context, projectID string) ([]delivery.Milestone, error) {
	return executeServiceExtension(operations, ctx, "delivery.milestones.list", "list_milestones", "delivery.items.read", "read_only", projectID, func(callContext context.Context) ([]delivery.Milestone, error) {
		return operations.service.ListMilestones(callContext, projectID)
	})
}

func (operations *Operations) SaveView(ctx context.Context, input delivery.SavedViewInput) (delivery.SavedView, error) {
	return executeServiceExtension(operations, ctx, "delivery.views.save", "save_view", "delivery.items.write", "local", input, func(callContext context.Context) (delivery.SavedView, error) {
		return operations.service.SaveView(callContext, input)
	})
}

func (operations *Operations) ListSavedViews(ctx context.Context) ([]delivery.SavedView, error) {
	return executeServiceExtension(operations, ctx, "delivery.views.list", "list_views", "delivery.items.read", "read_only", nil, func(callContext context.Context) ([]delivery.SavedView, error) {
		return operations.service.ListSavedViews(callContext)
	})
}

func (operations *Operations) MemberWeek(ctx context.Context, member, weekStart string) (delivery.MemberWeek, error) {
	return executeServiceExtension(operations, ctx, "delivery.members.week", "get_member_week", "delivery.items.read", "read_only", map[string]string{"member": member, "weekStart": weekStart}, func(callContext context.Context) (delivery.MemberWeek, error) {
		return operations.service.MemberWeek(callContext, member, weekStart)
	})
}

func (operations *Operations) ProjectProgress(ctx context.Context, projectID string) (delivery.ProjectProgress, error) {
	return executeServiceExtension(operations, ctx, "delivery.projects.progress", "get_project_progress", "delivery.items.read", "read_only", projectID, func(callContext context.Context) (delivery.ProjectProgress, error) {
		return operations.service.ProjectProgress(callContext, projectID)
	})
}

// ProjectSchedule exposes the same read-only, authenticated project-health
// calculation to HTTP and MCP callers without letting those transports access
// the repository directly.
func (operations *Operations) ProjectSchedule(ctx context.Context, projectID string) (delivery.ProjectSchedule, error) {
	return executeServiceExtension(operations, ctx, "delivery.projects.schedule", "get_project_schedule", "delivery.items.read", "read_only", projectID, func(callContext context.Context) (delivery.ProjectSchedule, error) {
		return operations.service.ProjectSchedule(callContext, projectID)
	})
}

func (operations *Operations) ListNotifications(ctx context.Context, limit int) ([]notification.Notification, error) {
	if err := operations.notificationsReady(); err != nil {
		return nil, err
	}
	value, err := operations.executor.Execute(ctx, extensionPlan("delivery.notifications.list", "list_notifications", "delivery.items.read", "read_only"), limit, func(callContext context.Context) (any, error) {
		return operations.notifications.List(callContext, limit)
	})
	if err != nil {
		return nil, err
	}
	notifications, ok := value.([]notification.Notification)
	if !ok {
		return nil, errors.New("delivery notification list operation returned an unexpected result")
	}
	return notifications, nil
}

func executeServiceExtension[T any](operations *Operations, ctx context.Context, operationID, useCase, permission, transaction string, input any, action func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := operations.extensionReady(); err != nil {
		return zero, err
	}
	value, err := operations.executor.Execute(ctx, extensionPlan(operationID, useCase, permission, transaction), input, func(callContext context.Context) (any, error) {
		return action(callContext)
	})
	if err != nil {
		return zero, err
	}
	result, ok := value.(T)
	if !ok {
		return zero, errors.New("delivery extension operation returned an unexpected result")
	}
	return result, nil
}

func (operations *Operations) ready() error {
	if operations == nil || operations.application == nil {
		return errors.New("delivery operations application is not configured")
	}
	if operations.executor == nil {
		return errors.New("delivery operations executor is not configured")
	}
	return nil
}

func (operations *Operations) extensionReady() error {
	if err := operations.ready(); err != nil {
		return err
	}
	if operations.service == nil {
		return errors.New("delivery extension service is not configured")
	}
	return nil
}

func (operations *Operations) notificationsReady() error {
	if err := operations.extensionReady(); err != nil {
		return err
	}
	if operations.notifications == nil {
		return errors.New("delivery notification reader is not configured")
	}
	return nil
}

func extensionPlan(operationID, useCase, permission, transaction string) operationplan.Plan {
	return operationplan.Plan{
		OperationID: operationID,
		Domain:      "delivery",
		Application: "management",
		UseCase:     useCase,
		Execution:   operationplan.Execution{Transaction: transaction, Idempotency: "none"},
		Security: operationplan.Security{
			Public:         false,
			Authentication: []string{"api-key", "jwt"},
			Permissions:    []string{permission},
			PermissionMode: "all",
		},
		Composition: operationplan.Composition{Boundary: "local"},
	}
}

func workItemsFromProto(values []*deliveryv1.WorkItem) []delivery.WorkItem {
	items := make([]delivery.WorkItem, 0, len(values))
	for _, value := range values {
		items = append(items, workItemFromProto(value))
	}
	return items
}

func workItemFromProto(value *deliveryv1.WorkItem) delivery.WorkItem {
	if value == nil {
		return delivery.WorkItem{}
	}
	item := delivery.WorkItem{
		Revision:  value.GetRevision(),
		ID:        value.GetId(),
		Title:     value.GetTitle(),
		Board:     delivery.Board(value.GetBoard()),
		ProjectID: value.GetProjectId(), ParentID: value.GetParentId(), Kind: delivery.WorkItemKind(value.GetKind()),
		Type:      value.GetType(),
		Owner:     value.GetOwner(),
		Priority:  delivery.Priority(value.GetPriority()),
		Status:    delivery.Status(value.GetStatus()),
		Gate:      delivery.Gate(value.GetGate()),
		DueDate:   value.GetDueDate(),
		ReleaseID: value.GetReleaseId(), SprintID: value.GetSprintId(), MilestoneID: value.GetMilestoneId(), StartDate: value.GetStartDate(), EstimatePoints: value.GetEstimatePoints(), ProgressPercent: int(value.GetProgressPercent()),
		Plan:          value.GetPlan(),
		Solution:      value.GetSolution(),
		Retrospective: value.GetRetrospective(),
		Blocker:       value.GetBlocker(),
		IsSample:      value.GetIsSample(),
		CreatedAt:     timeFromProto(value.GetCreatedAt()),
		UpdatedAt:     timeFromProto(value.GetUpdatedAt()),
	}
	for _, v := range value.GetDependencies() {
		if v != nil {
			item.Dependencies = append(item.Dependencies, delivery.WorkItemDependency{ItemID: v.GetItemId(), Relation: delivery.DependencyRelation(v.GetRelation())})
		}
	}
	for _, v := range value.GetIotBindings() {
		if v != nil {
			item.IoTBindings = append(item.IoTBindings, delivery.IoTBinding{Kind: delivery.IoTBindingKind(v.GetKind()), Reference: v.GetReference(), Label: v.GetLabel(), Attributes: v.GetAttributes()})
		}
	}
	for _, v := range value.GetTraceLinks() {
		if v != nil {
			item.TraceLinks = append(item.TraceLinks, delivery.TraceLink{Kind: delivery.TraceKind(v.GetKind()), Reference: v.GetReference(), Title: v.GetTitle(), URL: v.GetUrl(), Status: v.GetStatus(), RecordedAt: timeFromProto(v.GetRecordedAt())})
		}
	}
	for _, v := range value.GetComments() {
		if v != nil {
			item.Comments = append(item.Comments, delivery.Comment{ID: v.GetId(), Body: v.GetBody(), Author: v.GetAuthor(), CreatedAt: timeFromProto(v.GetCreatedAt())})
		}
	}
	for _, v := range value.GetActivities() {
		if v != nil {
			item.Activities = append(item.Activities, delivery.Activity{ID: v.GetId(), Type: v.GetType(), Summary: v.GetSummary(), Actor: v.GetActor(), OccurredAt: timeFromProto(v.GetOccurredAt())})
		}
	}
	for _, value := range value.GetDecisions() {
		if value == nil {
			continue
		}
		item.Decisions = append(item.Decisions, delivery.Decision{
			ID:           value.GetId(),
			Title:        value.GetTitle(),
			Context:      value.GetContext(),
			Outcome:      value.GetOutcome(),
			Consequences: value.GetConsequences(),
			CreatedAt:    timeFromProto(value.GetCreatedAt()),
		})
	}
	for _, value := range value.GetEvidence() {
		if value == nil {
			continue
		}
		item.Evidence = append(item.Evidence, delivery.Evidence{
			Kind:       value.GetKind(),
			Title:      value.GetTitle(),
			Reference:  value.GetReference(),
			RecordedAt: timeFromProto(value.GetRecordedAt()),
		})
	}
	return item
}

func projectFromProto(value *deliveryv1.Project) delivery.Project {
	if value == nil {
		return delivery.Project{}
	}
	return delivery.Project{ID: value.GetId(), Name: value.GetName(), Board: delivery.Board(value.GetBoard()), Owner: value.GetOwner(), Description: value.GetDescription(), CreatedAt: timeFromProto(value.GetCreatedAt()), UpdatedAt: timeFromProto(value.GetUpdatedAt())}
}

func releaseFromProto(value *deliveryv1.Release) delivery.Release {
	if value == nil {
		return delivery.Release{}
	}
	return delivery.Release{ID: value.GetId(), ProjectID: value.GetProjectId(), Name: value.GetName(), Version: value.GetVersion(), TargetDate: value.GetTargetDate(), Status: value.GetStatus(), Description: value.GetDescription(), CreatedAt: timeFromProto(value.GetCreatedAt()), UpdatedAt: timeFromProto(value.GetUpdatedAt())}
}

func sprintFromProto(value *deliveryv1.Sprint) delivery.Sprint {
	if value == nil {
		return delivery.Sprint{}
	}
	return delivery.Sprint{ID: value.GetId(), ProjectID: value.GetProjectId(), Name: value.GetName(), Goal: value.GetGoal(), StartDate: value.GetStartDate(), EndDate: value.GetEndDate(), Status: value.GetStatus(), CreatedAt: timeFromProto(value.GetCreatedAt()), UpdatedAt: timeFromProto(value.GetUpdatedAt())}
}

func milestoneFromProto(value *deliveryv1.Milestone) delivery.Milestone {
	if value == nil {
		return delivery.Milestone{}
	}
	return delivery.Milestone{ID: value.GetId(), ProjectID: value.GetProjectId(), Name: value.GetName(), TargetDate: value.GetTargetDate(), Status: value.GetStatus(), Description: value.GetDescription(), CreatedAt: timeFromProto(value.GetCreatedAt()), UpdatedAt: timeFromProto(value.GetUpdatedAt())}
}

func decisionToProto(value delivery.Decision) *deliveryv1.Decision {
	return &deliveryv1.Decision{
		Id:           value.ID,
		Title:        value.Title,
		Context:      value.Context,
		Outcome:      value.Outcome,
		Consequences: value.Consequences,
		CreatedAt:    timestamp(value.CreatedAt),
	}
}
