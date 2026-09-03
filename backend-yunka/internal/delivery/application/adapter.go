package application

import (
	"context"
	"errors"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		Title:    request.GetTitle(),
		Board:    delivery.Board(request.GetBoard()),
		Type:     request.GetType(),
		Owner:    request.GetOwner(),
		Priority: delivery.Priority(request.GetPriority()),
		DueDate:  request.GetDueDate(),
		Plan:     request.GetPlan(),
		Solution: request.GetSolution(),
		IsSample: request.GetIsSample(),
	})
	if err != nil {
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
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
		return nil, err
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
		return nil, err
	}
	return &deliveryv1.WorkItemResponse{Item: workItem(item)}, nil
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
	return service.List(ctx)
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
	}
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
