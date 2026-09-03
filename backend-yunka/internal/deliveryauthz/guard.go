// Package deliveryauthz interprets durable delivery resource ownership at the
// operation boundary. It never trusts client-provided organization identifiers.
package deliveryauthz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/contracts/authorization"
	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"yunka.io/gateway/authz"
)

var (
	ErrLookupRequired   = errors.New("delivery authorization lookup is required")
	ErrDatabaseRequired = errors.New("delivery authorization database is required")
	ErrDenied           = errors.New("delivery authorization denied")
)

type resourceLookup interface {
	Get(context.Context, string) (delivery.WorkItem, error)
	GetProject(context.Context, string) (delivery.Project, error)
	ListProjects(context.Context) ([]delivery.Project, error)
}

type OperationGuard struct {
	lookup     resourceLookup
	database   *sql.DB
	operations map[authz.OperationID]authorization.Operation
}

var authorizedProjectsKey = authz.MustScopeKey[map[string]bool]("delivery.authorized-projects")
var organizationKey = authz.MustScopeKey[string]("delivery.authorized-organization")

func NewOperationGuard(lookup resourceLookup, database *sql.DB) (*OperationGuard, error) {
	if lookup == nil {
		return nil, ErrLookupRequired
	}
	if database == nil {
		return nil, ErrDatabaseRequired
	}
	dictionary, err := authorization.LoadPermissionDictionary()
	if err != nil {
		return nil, fmt.Errorf("load permission dictionary: %w", err)
	}
	operations := make(map[authz.OperationID]authorization.Operation, len(dictionary.Operations))
	for _, operation := range dictionary.Operations {
		if operation.ID == "" || operation.Permission == "" || operation.RequiredScope == "" {
			return nil, errors.New("delivery authorization dictionary operation is incomplete")
		}
		operations[authz.OperationID(operation.ID)] = operation
	}
	return &OperationGuard{lookup: lookup, database: database, operations: operations}, nil
}

func (guard *OperationGuard) GuardResolver() authz.StaticGuardResolver {
	if guard == nil {
		return nil
	}
	guards := make(map[authz.OperationID]authz.OperationGuard, len(guard.operations))
	for operation := range guard.operations {
		guards[operation] = guard
	}
	return authz.NewStaticGuardResolver(guards)
}

func (guard *OperationGuard) Prepare(ctx context.Context, authorized authz.AuthorizedOperation, input any) (context.Context, error) {
	if guard == nil || guard.lookup == nil || guard.database == nil {
		return nil, ErrDenied
	}
	if !validInput(authorized.Policy.Operation, input) {
		return nil, ErrDenied
	}
	operation, ok := guard.operations[authorized.Policy.Operation]
	if !ok || !authorized.Decision.Allowed || operation.Permission != string(singlePermission(authorized.Policy)) {
		return nil, ErrDenied
	}
	tenantID := strings.TrimSpace(authorized.Principal.TenantID)
	if tenantID == "" {
		return nil, ErrDenied
	}
	grants, err := guard.verifyGrants(ctx, authorized.Decision.Grants, operation)
	if err != nil {
		return nil, err
	}
	secured := organizationKey.With(ctx, tenantID)
	if operation.RequiredScope == "organization" {
		if !guard.allowsOrganization(grants, operation, tenantID) {
			return nil, ErrDenied
		}
		if authorized.Policy.Operation == "delivery.dashboard.get" {
			projects, err := guard.organizationProjects(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			return authorizedProjectsKey.With(secured, projects), nil
		}
		return secured, nil
	}
	if authorized.Policy.Operation == "delivery.items.list" {
		projects, err := guard.allowedProjects(ctx, grants, operation, tenantID)
		if err != nil {
			return nil, err
		}
		return authorizedProjectsKey.With(secured, projects), nil
	}
	projectID, err := guard.projectID(ctx, authorized.Policy.Operation, input)
	if err != nil {
		return nil, err
	}
	project, err := guard.ownedProject(ctx, projectID, tenantID)
	if err != nil {
		return nil, err
	}
	if !guard.allowsProject(grants, operation, tenantID, project.ID, "") {
		return nil, ErrDenied
	}
	return secured, nil
}

func singlePermission(policy authz.Policy) authz.PermissionKey {
	if len(policy.Permissions) != 1 {
		return ""
	}
	return policy.Permissions[0]
}

func (guard *OperationGuard) verifyGrants(ctx context.Context, grants []authz.Grant, operation authorization.Operation) ([]authz.Grant, error) {
	verified := make([]authz.Grant, 0, len(grants))
	for _, grant := range grants {
		if string(grant.Permission) != operation.Permission || strings.TrimSpace(grant.RoleID) == "" {
			continue
		}
		var found int
		err := guard.database.QueryRowContext(ctx, `SELECT 1 FROM permissions p JOIN role_permission_grants r ON r.permission_id = p.id JOIN role_permission_grant_allowed_scopes s ON s.role_id = r.role_id AND s.permission_id = r.permission_id WHERE p.id = ? AND p.status = 'active' AND r.role_id = ? AND s.scope_type = ?`, operation.Permission, grant.RoleID, operation.RequiredScope).Scan(&found)
		if err == nil && found == 1 {
			verified = append(verified, grant)
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("verify delivery grant: %w", err)
		}
	}
	if len(verified) == 0 {
		return nil, ErrDenied
	}
	return verified, nil
}

func (guard *OperationGuard) organizationProjects(ctx context.Context, tenantID string) (map[string]bool, error) {
	projects, err := guard.lookup.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list delivery projects: %w", err)
	}
	allowed := make(map[string]bool)
	for _, project := range projects {
		if project.OrganizationID == tenantID {
			allowed[project.ID] = true
		}
	}
	return allowed, nil
}

func (guard *OperationGuard) allowedProjects(ctx context.Context, grants []authz.Grant, operation authorization.Operation, tenantID string) (map[string]bool, error) {
	projects, err := guard.organizationProjects(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for id := range projects {
		if !guard.allowsProject(grants, operation, tenantID, id, "") {
			delete(projects, id)
		}
	}
	return projects, nil
}

func (guard *OperationGuard) ownedProject(ctx context.Context, projectID, tenantID string) (delivery.Project, error) {
	project, err := guard.lookup.GetProject(ctx, projectID)
	if err != nil || project.OrganizationID == "" || project.OrganizationID != tenantID {
		return delivery.Project{}, ErrDenied
	}
	return project, nil
}

func (guard *OperationGuard) allowsOrganization(grants []authz.Grant, operation authorization.Operation, tenantID string) bool {
	for _, grant := range grants {
		if string(grant.Permission) == operation.Permission && grant.Scope == "organization:"+tenantID {
			return true
		}
	}
	return false
}

func (guard *OperationGuard) allowsProject(grants []authz.Grant, operation authorization.Operation, tenantID, projectID, objectID string) bool {
	for _, grant := range grants {
		if string(grant.Permission) != operation.Permission {
			continue
		}
		switch grant.Scope {
		case "organization:" + tenantID:
			return true
		case "project:" + projectID:
			return true
		case "object:work-item:" + objectID:
			return objectID != ""
		}
	}
	return false
}

func (guard *OperationGuard) projectID(ctx context.Context, operation authz.OperationID, input any) (string, error) {
	projectID := ""
	switch request := input.(type) {
	case *deliveryv1.CreateItemRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateReleaseRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateSprintRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.CreateMilestoneRequest:
		projectID = request.GetProjectId()
	case *deliveryv1.UpdateItemRequest:
		return guard.itemProjectID(ctx, request.GetId())
	case *deliveryv1.CreateItemCommentRequest:
		return guard.itemProjectID(ctx, request.GetId())
	case *deliveryv1.UpdateItemContextRequest:
		return guard.itemProjectID(ctx, request.GetId())
	case *deliveryv1.AdvanceGateRequest:
		return guard.itemProjectID(ctx, request.GetId())
	case *deliveryv1.CloseItemRequest:
		return guard.itemProjectID(ctx, request.GetId())
	default:
		return "", ErrDenied
	}
	if strings.TrimSpace(projectID) == "" {
		return "", ErrDenied
	}
	return projectID, nil
}

func validInput(operation authz.OperationID, input any) bool {
	switch operation {
	case "delivery.dashboard.get":
		request, ok := input.(*deliveryv1.GetDashboardRequest)
		return ok && request != nil
	case "delivery.items.list":
		request, ok := input.(*deliveryv1.ListItemsRequest)
		return ok && request != nil
	case "delivery.projects.create":
		request, ok := input.(*deliveryv1.CreateProjectRequest)
		return ok && request != nil
	case "delivery.items.create":
		request, ok := input.(*deliveryv1.CreateItemRequest)
		return ok && request != nil
	case "delivery.items.update":
		request, ok := input.(*deliveryv1.UpdateItemRequest)
		return ok && request != nil
	case "delivery.items.comment.create":
		request, ok := input.(*deliveryv1.CreateItemCommentRequest)
		return ok && request != nil
	case "delivery.items.update-context":
		request, ok := input.(*deliveryv1.UpdateItemContextRequest)
		return ok && request != nil
	case "delivery.items.advance-gate":
		request, ok := input.(*deliveryv1.AdvanceGateRequest)
		return ok && request != nil
	case "delivery.items.close":
		request, ok := input.(*deliveryv1.CloseItemRequest)
		return ok && request != nil
	case "delivery.releases.create":
		request, ok := input.(*deliveryv1.CreateReleaseRequest)
		return ok && request != nil
	case "delivery.sprints.create":
		request, ok := input.(*deliveryv1.CreateSprintRequest)
		return ok && request != nil
	case "delivery.milestones.create":
		request, ok := input.(*deliveryv1.CreateMilestoneRequest)
		return ok && request != nil
	default:
		return false
	}
}

func (guard *OperationGuard) itemProjectID(ctx context.Context, itemID string) (string, error) {
	if strings.TrimSpace(itemID) == "" {
		return "", ErrDenied
	}
	item, err := guard.lookup.Get(ctx, itemID)
	if err != nil || strings.TrimSpace(item.ProjectID) == "" {
		return "", ErrDenied
	}
	return item.ProjectID, nil
}

func AuthorizedProjectsFromContext(ctx context.Context) (map[string]bool, bool) {
	return authorizedProjectsKey.From(ctx)
}
func OrganizationIDFromContext(ctx context.Context) string {
	value, _ := organizationKey.From(ctx)
	return value
}
