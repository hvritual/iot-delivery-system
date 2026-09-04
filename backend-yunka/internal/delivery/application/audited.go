package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/audit"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/policy"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/framework/execution"
)

// CorrelationIDAttribute is the only trusted runtime attribute copied into an
// audit entry. All other attributes remain outside the audit record.
const CorrelationIDAttribute = "correlation_id"

// WorkItemResolver retrieves the server-persisted parent of a nested comment.
// It intentionally accepts only an item identifier already handled by the
// application, never a client-provided project or scope value.
type WorkItemResolver func(context.Context, string) (delivery.WorkItem, error)

// AuditedOption configures the handwritten audit boundary. The ID and clock
// hooks exist for deterministic tests; production defaults use crypto/rand and
// the service clock.
type AuditedOption func(*auditedDeliveryService) error

func WithAuditIDGenerator(generator func() (string, error)) AuditedOption {
	return func(service *auditedDeliveryService) error {
		if generator == nil {
			return errors.New("audit ID generator is required")
		}
		service.newID = generator
		return nil
	}
}

func WithAuditClock(clock func() time.Time) AuditedOption {
	return func(service *auditedDeliveryService) error {
		if clock == nil {
			return errors.New("audit clock is required")
		}
		service.clock = clock
		return nil
	}
}

func WithWorkItemResolver(resolver WorkItemResolver) AuditedOption {
	return func(service *auditedDeliveryService) error {
		if resolver == nil {
			return errors.New("audit work item resolver is required")
		}
		service.workItem = resolver
		return nil
	}
}

// NewAuditedDeliveryService decorates the generated application port. Each
// append happens after the business method returns successfully and while the
// generated executor's local transaction remains active.
func NewAuditedDeliveryService(delegate DeliveryService, store audit.Store, options ...AuditedOption) (DeliveryService, error) {
	if delegate == nil {
		return nil, errors.New("audit delivery delegate is required")
	}
	if store == nil {
		return nil, errors.New("audit store is required")
	}
	service := &auditedDeliveryService{delegate: delegate, store: store, newID: randomAuditID, clock: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("audit delivery option is required")
		}
		if err := option(service); err != nil {
			return nil, err
		}
	}
	if service.workItem == nil {
		return nil, errors.New("audit work item resolver is required")
	}
	return service, nil
}

type auditedDeliveryService struct {
	delegate DeliveryService
	store    audit.Store
	newID    func() (string, error)
	clock    func() time.Time
	workItem WorkItemResolver
}

var _ DeliveryService = (*auditedDeliveryService)(nil)

func (service *auditedDeliveryService) GetDashboard(ctx context.Context, request *deliveryv1.GetDashboardRequest) (*deliveryv1.GetDashboardResponse, error) {
	return service.delegate.GetDashboard(ctx, request)
}

func (service *auditedDeliveryService) ListItems(ctx context.Context, request *deliveryv1.ListItemsRequest) (*deliveryv1.ListItemsResponse, error) {
	return service.delegate.ListItems(ctx, request)
}

func (service *auditedDeliveryService) CreateItem(ctx context.Context, request *deliveryv1.CreateItemRequest) (*deliveryv1.WorkItemResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateItem().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateItem(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := service.appendItem(ctx, operationID, metadata, "created", createItemFields(request), response.GetItem()); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) UpdateItem(ctx context.Context, request *deliveryv1.UpdateItemRequest) (*deliveryv1.WorkItemResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanUpdateItem().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.UpdateItem(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := service.appendItem(ctx, operationID, metadata, "updated", request.GetUpdateMask(), response.GetItem()); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CreateItemComment(ctx context.Context, request *deliveryv1.CreateItemCommentRequest) (*deliveryv1.CommentResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateItemComment().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateItemComment(ctx, request)
	if err != nil {
		return nil, err
	}
	item, err := service.workItem(ctx, request.GetId())
	if err != nil {
		return nil, fmt.Errorf("resolve comment audit scope: %w", err)
	}
	if response == nil || response.GetComment() == nil {
		return nil, errors.New("create item comment returned no comment")
	}
	if err := service.append(ctx, operationID, metadata, auditInput{
		change:     "created",
		fields:     []string{"body"},
		projectID:  item.ProjectID,
		targetType: "delivery.comment",
		targetID:   response.GetComment().GetId(),
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) UpdateItemContext(ctx context.Context, request *deliveryv1.UpdateItemContextRequest) (*deliveryv1.WorkItemResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanUpdateItemContext().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.UpdateItemContext(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := service.appendItem(ctx, operationID, metadata, "updated_context", contextFields(request), response.GetItem()); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) AdvanceGate(ctx context.Context, request *deliveryv1.AdvanceGateRequest) (*deliveryv1.WorkItemResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanAdvanceGate().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.AdvanceGate(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := service.appendItem(ctx, operationID, metadata, "advanced_gate", []string{"gate", "evidence"}, response.GetItem()); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CloseItem(ctx context.Context, request *deliveryv1.CloseItemRequest) (*deliveryv1.WorkItemResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCloseItem().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CloseItem(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := service.appendItem(ctx, operationID, metadata, "closed", []string{"status", "retrospective"}, response.GetItem()); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CreateProject(ctx context.Context, request *deliveryv1.CreateProjectRequest) (*deliveryv1.ProjectResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateProject().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateProject(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetProject() == nil {
		return nil, errors.New("create project returned no project")
	}
	if err := service.append(ctx, operationID, metadata, auditInput{
		change:     "created",
		fields:     []string{"name", "board", "owner", "description"},
		targetType: "delivery.project",
		targetID:   response.GetProject().GetId(),
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CreateRelease(ctx context.Context, request *deliveryv1.CreateReleaseRequest) (*deliveryv1.ReleaseResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateRelease().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateRelease(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetRelease() == nil {
		return nil, errors.New("create release returned no release")
	}
	if err := service.append(ctx, operationID, metadata, auditInput{
		change:     "created",
		fields:     []string{"project_id", "name", "version", "target_date", "status", "description"},
		projectID:  response.GetRelease().GetProjectId(),
		targetType: "delivery.release",
		targetID:   response.GetRelease().GetId(),
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CreateSprint(ctx context.Context, request *deliveryv1.CreateSprintRequest) (*deliveryv1.SprintResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateSprint().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateSprint(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetSprint() == nil {
		return nil, errors.New("create sprint returned no sprint")
	}
	if err := service.append(ctx, operationID, metadata, auditInput{
		change:     "created",
		fields:     []string{"project_id", "name", "goal", "start_date", "end_date", "status"},
		projectID:  response.GetSprint().GetProjectId(),
		targetType: "delivery.sprint",
		targetID:   response.GetSprint().GetId(),
	}); err != nil {
		return nil, err
	}
	return response, nil
}

func (service *auditedDeliveryService) CreateMilestone(ctx context.Context, request *deliveryv1.CreateMilestoneRequest) (*deliveryv1.MilestoneResponse, error) {
	operationID, metadata, err := verifiedWriteOperation(ctx, policy.OperationPlanCreateMilestone().OperationID)
	if err != nil {
		return nil, err
	}
	response, err := service.delegate.CreateMilestone(ctx, request)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GetMilestone() == nil {
		return nil, errors.New("create milestone returned no milestone")
	}
	if err := service.append(ctx, operationID, metadata, auditInput{
		change:     "created",
		fields:     []string{"project_id", "name", "target_date", "status", "description"},
		projectID:  response.GetMilestone().GetProjectId(),
		targetType: "delivery.milestone",
		targetID:   response.GetMilestone().GetId(),
	}); err != nil {
		return nil, err
	}
	return response, nil
}

type auditInput struct {
	change     string
	fields     []string
	projectID  string
	targetType string
	targetID   string
}

func verifiedWriteOperation(ctx context.Context, expectedOperationID string) (string, runtimecontext.Metadata, error) {
	frame, active := execution.Current(ctx)
	if !active {
		return "", runtimecontext.Metadata{}, errors.New("audit execution frame is required")
	}
	if frame.Transaction != execution.TransactionLocal {
		return "", runtimecontext.Metadata{}, fmt.Errorf("audit execution transaction = %q, want local", frame.Transaction)
	}
	if expectedOperationID == "" || frame.OperationID != expectedOperationID {
		return "", runtimecontext.Metadata{}, fmt.Errorf("audit execution operation = %q, want %q", frame.OperationID, expectedOperationID)
	}
	metadata, present := runtimecontext.MetadataFrom(ctx)
	if !present || metadata.Operation != frame.OperationID {
		return "", runtimecontext.Metadata{}, errors.New("audit execution metadata operation does not match frame")
	}
	return frame.OperationID, metadata, nil
}

func (service *auditedDeliveryService) appendItem(ctx context.Context, operationID string, metadata runtimecontext.Metadata, change string, fields []string, item *deliveryv1.WorkItem) error {
	if item == nil {
		return errors.New("delivery write returned no work item")
	}
	return service.append(ctx, operationID, metadata, auditInput{
		change:     change,
		fields:     fields,
		projectID:  item.GetProjectId(),
		targetType: "delivery.work-item",
		targetID:   item.GetId(),
	})
}

func (service *auditedDeliveryService) append(ctx context.Context, operationID string, metadata runtimecontext.Metadata, input auditInput) error {
	if service == nil || service.store == nil || service.newID == nil || service.clock == nil {
		return errors.New("audited delivery service is not configured")
	}
	actor, organizationID, err := trustedAuditActor(ctx)
	if err != nil {
		return err
	}
	id, err := service.newID()
	if err != nil {
		return fmt.Errorf("generate audit ID: %w", err)
	}
	diffSummary, err := audit.BuildDiffSummary(input.change, input.fields)
	if err != nil {
		return fmt.Errorf("encode audit diff summary: %w", err)
	}
	auditMetadata := map[string]string{}
	if metadata.Transport != "" {
		auditMetadata["transport"] = metadata.Transport
	}
	encodedMetadata, err := json.Marshal(auditMetadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	scopeType, scopeID := audit.ScopeOrganization, organizationID
	if input.projectID != "" {
		scopeType, scopeID = audit.ScopeProject, input.projectID
	}
	occurredAt := service.clock().UTC()
	if occurredAt.IsZero() {
		return errors.New("audit clock returned zero time")
	}
	_, err = service.store.Append(ctx, audit.Entry{
		ID:                    id,
		SchemaVersion:         audit.SchemaVersion,
		EventCategory:         audit.EventCategoryDelivery,
		OrganizationID:        organizationID,
		ProjectID:             input.projectID,
		ActorType:             actor.kind,
		ActorID:               actor.id,
		Operation:             operationID,
		AuthorizationDecision: audit.DecisionAllowed,
		ScopeType:             scopeType,
		ScopeID:               scopeID,
		TargetType:            input.targetType,
		TargetID:              input.targetID,
		Result:                audit.ResultSuccess,
		ReasonCode:            "delivery.change.applied",
		TraceID:               runtimecontext.TraceIDFrom(ctx),
		RequestID:             metadata.RequestID,
		CorrelationID:         metadata.Attributes[CorrelationIDAttribute],
		DiffSummary:           diffSummary,
		Metadata:              string(encodedMetadata),
		OccurredAt:            occurredAt,
	})
	if err != nil {
		return fmt.Errorf("record successful delivery audit: %w", err)
	}
	return nil
}

type auditActor struct {
	kind audit.ActorType
	id   string
}

func trustedAuditActor(ctx context.Context) (auditActor, string, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok || !principal.Authenticated || !canonicalAuditIdentifier(principal.TenantID) {
		return auditActor{}, "", errors.New("successful delivery audit requires a trusted principal tenant")
	}
	switch principal.AuthMethod {
	case identity.AuthMethodJWT:
		if !canonicalAuditIdentifier(principal.UserID) {
			return auditActor{}, "", errors.New("successful delivery audit requires a trusted human actor")
		}
		return auditActor{kind: audit.ActorHuman, id: principal.UserID}, principal.TenantID, nil
	case identity.AuthMethodServiceToken:
		serviceAccountID, exists := strings.CutPrefix(principal.Subject, "service-account/")
		if !exists || !canonicalAuditIdentifier(serviceAccountID) || principal.UserID != "" {
			return auditActor{}, "", errors.New("successful delivery audit requires a trusted service actor")
		}
		return auditActor{kind: audit.ActorService, id: serviceAccountID}, principal.TenantID, nil
	case identity.AuthMethodAPIKey:
		return auditActor{kind: audit.ActorSystem, id: "development-api-key"}, principal.TenantID, nil
	default:
		return auditActor{}, "", errors.New("successful delivery audit requires a supported trusted principal")
	}
}

func canonicalAuditIdentifier(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func randomAuditID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func createItemFields(request *deliveryv1.CreateItemRequest) []string {
	if request == nil {
		return nil
	}
	return []string{"title", "board", "type", "owner", "priority", "due_date", "project_id", "parent_id", "kind", "release_id", "sprint_id", "milestone_id", "start_date", "estimate_points", "progress_percent", "plan", "solution", "dependencies", "iot_bindings", "trace_links"}
}

func contextFields(request *deliveryv1.UpdateItemContextRequest) []string {
	if request == nil {
		return nil
	}
	fields := make([]string, 0, 4)
	if request.Plan != nil {
		fields = append(fields, "plan")
	}
	if request.Solution != nil {
		fields = append(fields, "solution")
	}
	if request.Blocker != nil {
		fields = append(fields, "blocker")
	}
	if request.Decision != nil {
		fields = append(fields, "decision")
	}
	return fields
}
