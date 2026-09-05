// Package mcpserver exposes the local delivery lifecycle as a Model Context
// Protocol server. It uses the same application operations as HTTP, so MCP
// calls inherit Yunka's execution, authorization, and transaction boundaries.
package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery/application"
	"github.com/hvritual/yunka.io/framework/core/identity"
	"github.com/hvritual/yunka.io/framework/core/runtimecontext"
	"github.com/hvritual/yunka.io/gateway/authz"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var errMCPUnauthenticated = errors.New("MCP principal is not authenticated")

type server struct {
	operations *application.Operations
	principal  identity.Principal
}

// New creates an in-process MCP server. Run it over stdio from the local
// command; it never exposes a remote MCP endpoint or sends notifications to a
// third party by itself.
func New(operations *application.Operations, principal identity.Principal) *mcp.Server {
	implementation := mcp.NewServer(&mcp.Implementation{
		Name:    "iot-delivery-system",
		Version: "0.2.0",
	}, &mcp.ServerOptions{
		Instructions: "用于本地 IoT 研发交付生命周期管理。写操作仅改变本地交付系统；创建相似任务前必须先确认。",
	})
	current := &server{operations: operations, principal: principal}
	current.addTools(implementation)
	return implementation
}

func (server *server) addTools(target *mcp.Server) {
	addTool(target, readTool("delivery.list_projects", "列出交付项目", "列出当前本地交付项目。"), server.listProjects)
	addTool(target, writeTool("delivery.create_project", "创建交付项目", "在本地交付系统创建项目。"), server.createProject)
	addTool(target, readTool("delivery.list_work_items", "查询交付事项", "按项目、负责人、状态、类型和关键词查询事项。"), server.listWorkItems)
	addTool(target, readTool("delivery.get_work_item", "查看交付事项", "按事项 ID 查看一个交付事项。"), server.getWorkItem)
	addTool(target, readTool("delivery.find_similar", "检查相似事项", "在创建前检查同项目或同板块的相似事项。"), server.findSimilar)
	addTool(target, writeTool("delivery.create_work_item", "创建交付事项", "创建事项；若存在相似候选，先返回候选，需显式确认后才创建。"), server.createWorkItem)
	addTool(target, writeTool("delivery.update_work_item", "更新交付事项", "编辑任务的排期、进度、IoT 绑定、依赖和研发证据。"), server.updateWorkItem)
	addTool(target, writeTool("delivery.add_comment", "新增事项评论", "为交付事项添加可审计评论。"), server.addComment)
	addTool(target, writeTool("delivery.advance_gate", "推进交付关卡", "提交证据并推进交付关卡。"), server.advanceGate)
	addTool(target, writeTool("delivery.close_work_item", "关闭交付事项", "在生产验证后记录复盘并关闭事项。"), server.closeWorkItem)
	addTool(target, writeTool("delivery.create_release", "创建发布版本", "为项目创建发布版本。"), server.createRelease)
	addTool(target, readTool("delivery.list_releases", "列出发布版本", "列出项目的发布版本。"), server.listReleases)
	addTool(target, writeTool("delivery.create_sprint", "创建 Sprint", "为项目创建 Sprint。"), server.createSprint)
	addTool(target, readTool("delivery.list_sprints", "列出 Sprint", "列出项目的 Sprint。"), server.listSprints)
	addTool(target, writeTool("delivery.create_milestone", "创建里程碑", "为项目创建里程碑。"), server.createMilestone)
	addTool(target, readTool("delivery.list_milestones", "列出里程碑", "列出项目的里程碑。"), server.listMilestones)
	addTool(target, readTool("delivery.get_member_week", "查看成员周任务", "查看成员在指定自然周的任务事项。"), server.memberWeek)
	addTool(target, readTool("delivery.get_project_progress", "查看项目进度", "按估算权重汇总项目任务进度。"), server.projectProgress)
	addTool(target, readTool("delivery.get_project_schedule", "查看项目交付健康", "查看依赖阻塞、逾期、未排期和成员剩余估算。"), server.projectSchedule)
	addTool(target, writeTool("delivery.save_view", "保存任务视图", "保存当前本地身份的任务筛选视图。"), server.saveView)
	addTool(target, readTool("delivery.list_saved_views", "列出保存视图", "列出当前本地身份保存的任务筛选视图。"), server.listSavedViews)
}

func addTool[In, Out any](target *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(target, tool, func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		result, output, err := handler(ctx, request, input)
		return result, output, normalizeToolError(err)
	})
}

type normalizedToolError struct {
	category string
	cause    error
}

func (err *normalizedToolError) Error() string { return err.category }
func (err *normalizedToolError) Unwrap() error { return err.cause }

func normalizeToolError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errMCPUnauthenticated) {
		return &normalizedToolError{category: "unauthenticated", cause: err}
	}
	if authz.IsDenied(err) {
		var denied *authz.DeniedError
		if errors.As(err, &denied) && (denied.Decision.Reason == authz.ReasonUnauthenticated || denied.Decision.Reason == authz.ReasonAuthenticationMethod) {
			return &normalizedToolError{category: "unauthenticated", cause: err}
		}
		return &normalizedToolError{category: "permission_denied", cause: err}
	}
	if errors.Is(err, delivery.ErrRevisionConflict) {
		return &normalizedToolError{category: "revision_conflict", cause: err}
	}
	if errors.Is(err, delivery.ErrInvalidExpectedRevision) {
		return &normalizedToolError{category: "invalid_expected_revision", cause: err}
	}
	return err
}

func (server *server) toolContext(ctx context.Context) (context.Context, error) {
	if server == nil || server.operations == nil {
		return nil, errors.New("delivery MCP operations are not configured")
	}
	if !server.principal.Authenticated {
		return nil, errMCPUnauthenticated
	}
	ctx = identity.WithPrincipal(ctx, server.principal)
	if _, exists := runtimecontext.MetadataFrom(ctx); !exists {
		ctx = runtimecontext.WithMetadata(ctx, runtimecontext.Metadata{Transport: "mcp", Protocol: "mcp"})
	}
	return ctx, nil
}

type createProjectArgs struct {
	Name        string         `json:"name" jsonschema:"the project name"`
	Board       delivery.Board `json:"board" jsonschema:"one of the five delivery boards"`
	Owner       string         `json:"owner" jsonschema:"the project owner"`
	Description string         `json:"description,omitempty"`
}

type projectOutput struct {
	Project delivery.Project `json:"project"`
}

func (server *server) createProject(ctx context.Context, _ *mcp.CallToolRequest, args createProjectArgs) (*mcp.CallToolResult, projectOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, projectOutput{}, err
	}
	project, err := server.operations.CreateProject(ctx, delivery.ProjectInput{
		Name:        args.Name,
		Board:       args.Board,
		Owner:       args.Owner,
		Description: args.Description,
	})
	return nil, projectOutput{Project: project}, err
}

type projectsOutput struct {
	Projects []delivery.Project `json:"projects"`
}

func (server *server) listProjects(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, projectsOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, projectsOutput{}, err
	}
	projects, err := server.operations.ListProjects(ctx)
	return nil, projectsOutput{Projects: projects}, err
}

type listWorkItemsArgs struct {
	ProjectID   string                `json:"projectId,omitempty"`
	Board       delivery.Board        `json:"board,omitempty"`
	Owner       string                `json:"owner,omitempty"`
	Status      delivery.Status       `json:"status,omitempty"`
	Kind        delivery.WorkItemKind `json:"kind,omitempty"`
	ReleaseID   string                `json:"releaseId,omitempty"`
	SprintID    string                `json:"sprintId,omitempty"`
	MilestoneID string                `json:"milestoneId,omitempty"`
	Query       string                `json:"query,omitempty"`
}

type workItemsOutput struct {
	Items []delivery.WorkItem `json:"items"`
}

type getWorkItemArgs struct {
	ID string `json:"id" jsonschema:"the work item ID"`
}

type getWorkItemOutput struct {
	Item delivery.WorkItem `json:"item"`
}

func (server *server) getWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args getWorkItemArgs) (*mcp.CallToolResult, getWorkItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, getWorkItemOutput{}, err
	}
	item, err := server.operations.Get(ctx, args.ID)
	return nil, getWorkItemOutput{Item: item}, err
}

func (server *server) listWorkItems(ctx context.Context, _ *mcp.CallToolRequest, args listWorkItemsArgs) (*mcp.CallToolResult, workItemsOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemsOutput{}, err
	}
	filter := delivery.WorkItemFilter{
		ProjectID: args.ProjectID, Board: args.Board, Owner: args.Owner, Status: args.Status, Kind: args.Kind,
		ReleaseID: args.ReleaseID, SprintID: args.SprintID, MilestoneID: args.MilestoneID, Query: args.Query,
	}
	items, err := server.operations.Search(ctx, filter)
	if err != nil {
		return nil, workItemsOutput{}, err
	}
	return nil, workItemsOutput{Items: items}, nil
}

type similarArgs struct {
	Title     string                `json:"title"`
	Board     delivery.Board        `json:"board"`
	ProjectID string                `json:"projectId,omitempty"`
	Kind      delivery.WorkItemKind `json:"kind,omitempty"`
	Limit     int                   `json:"limit,omitempty"`
}

type similarOutput struct {
	Candidates []delivery.SimilarityCandidate `json:"candidates"`
}

func (server *server) findSimilar(ctx context.Context, _ *mcp.CallToolRequest, args similarArgs) (*mcp.CallToolResult, similarOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, similarOutput{}, err
	}
	candidates, err := server.operations.FindSimilar(ctx, delivery.SimilarityQuery(args))
	return nil, similarOutput{Candidates: candidates}, err
}

type createWorkItemArgs struct {
	Title           string                        `json:"title"`
	Board           delivery.Board                `json:"board"`
	ProjectID       string                        `json:"projectId,omitempty"`
	ParentID        string                        `json:"parentId,omitempty"`
	Kind            delivery.WorkItemKind         `json:"kind,omitempty"`
	Dependencies    []delivery.WorkItemDependency `json:"dependencies,omitempty"`
	Type            string                        `json:"type,omitempty"`
	Owner           string                        `json:"owner"`
	Priority        delivery.Priority             `json:"priority,omitempty"`
	ReleaseID       string                        `json:"releaseId,omitempty"`
	SprintID        string                        `json:"sprintId,omitempty"`
	MilestoneID     string                        `json:"milestoneId,omitempty"`
	StartDate       string                        `json:"startDate,omitempty"`
	DueDate         string                        `json:"dueDate,omitempty"`
	EstimatePoints  float64                       `json:"estimatePoints,omitempty"`
	ProgressPercent int                           `json:"progressPercent,omitempty"`
	Plan            string                        `json:"plan,omitempty"`
	Solution        string                        `json:"solution,omitempty"`
	IoTBindings     []delivery.IoTBinding         `json:"iotBindings,omitempty"`
	TraceLinks      []delivery.TraceLink          `json:"traceLinks,omitempty"`
	ConfirmSimilar  bool                          `json:"confirmSimilar,omitempty" jsonschema:"set true only after reviewing returned similar candidates"`
}

type createWorkItemOutput struct {
	Created              delivery.WorkItem              `json:"created,omitempty"`
	SimilarCandidates    []delivery.SimilarityCandidate `json:"similarCandidates,omitempty"`
	RequiresConfirmation bool                           `json:"requiresConfirmation"`
}

func (server *server) createWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args createWorkItemArgs) (*mcp.CallToolResult, createWorkItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, createWorkItemOutput{}, err
	}
	kind := args.Kind
	if kind == "" {
		kind = delivery.WorkItemKindTask
	}
	candidates, err := server.operations.FindSimilar(ctx, delivery.SimilarityQuery{
		Title: args.Title, Board: args.Board, ProjectID: args.ProjectID, Kind: kind, Limit: 5,
	})
	if err != nil {
		return nil, createWorkItemOutput{}, err
	}
	if len(candidates) > 0 && !args.ConfirmSimilar {
		return nil, createWorkItemOutput{SimilarCandidates: candidates, RequiresConfirmation: true}, nil
	}
	created, err := server.operations.Create(ctx, delivery.CreateInput{
		Title: args.Title, Board: args.Board, ProjectID: args.ProjectID, ParentID: args.ParentID, Kind: kind,
		Dependencies: args.Dependencies, Type: args.Type, Owner: args.Owner, Priority: args.Priority,
		ReleaseID: args.ReleaseID, SprintID: args.SprintID, MilestoneID: args.MilestoneID,
		StartDate: args.StartDate, DueDate: args.DueDate, EstimatePoints: args.EstimatePoints,
		ProgressPercent: args.ProgressPercent, Plan: args.Plan, Solution: args.Solution,
		IoTBindings: args.IoTBindings, TraceLinks: args.TraceLinks,
	})
	return nil, createWorkItemOutput{Created: created}, err
}

type updateWorkItemArgs struct {
	ID               string                         `json:"id"`
	ExpectedRevision int64                          `json:"expectedRevision"`
	Title            *string                        `json:"title,omitempty"`
	Owner            *string                        `json:"owner,omitempty"`
	Priority         *delivery.Priority             `json:"priority,omitempty"`
	ReleaseID        *string                        `json:"releaseId,omitempty"`
	SprintID         *string                        `json:"sprintId,omitempty"`
	MilestoneID      *string                        `json:"milestoneId,omitempty"`
	StartDate        *string                        `json:"startDate,omitempty"`
	DueDate          *string                        `json:"dueDate,omitempty"`
	EstimatePoints   *float64                       `json:"estimatePoints,omitempty"`
	ProgressPercent  *int                           `json:"progressPercent,omitempty"`
	Dependencies     *[]delivery.WorkItemDependency `json:"dependencies,omitempty"`
	IoTBindings      *[]delivery.IoTBinding         `json:"iotBindings,omitempty"`
	TraceLinks       *[]delivery.TraceLink          `json:"traceLinks,omitempty"`
}

type workItemOutput struct {
	Item delivery.WorkItem `json:"item"`
}

func (server *server) updateWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args updateWorkItemArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	item, err := server.operations.UpdateWorkItem(ctx, args.ID, args.ExpectedRevision, delivery.WorkItemUpdate{
		Title: args.Title, Owner: args.Owner, Priority: args.Priority, ReleaseID: args.ReleaseID, SprintID: args.SprintID,
		MilestoneID: args.MilestoneID, StartDate: args.StartDate, DueDate: args.DueDate, EstimatePoints: args.EstimatePoints,
		ProgressPercent: args.ProgressPercent, Dependencies: args.Dependencies, IoTBindings: args.IoTBindings, TraceLinks: args.TraceLinks,
	})
	return nil, workItemOutput{Item: item}, err
}

type addCommentArgs struct {
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Body             string `json:"body"`
}

type commentOutput struct {
	Comment delivery.Comment `json:"comment"`
}

func (server *server) addComment(ctx context.Context, _ *mcp.CallToolRequest, args addCommentArgs) (*mcp.CallToolResult, commentOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, commentOutput{}, err
	}
	comment, err := server.operations.AddComment(ctx, args.ID, args.ExpectedRevision, delivery.CommentInput{Body: args.Body})
	return nil, commentOutput{Comment: comment}, err
}

type advanceGateArgs struct {
	ID               string         `json:"id"`
	ExpectedRevision int64          `json:"expectedRevision"`
	Gate             delivery.Gate  `json:"gate"`
	Evidence         []evidenceArgs `json:"evidence"`
}

// evidenceArgs keeps MCP's optional timestamp semantics aligned with the
// generated gRPC request. A missing timestamp is passed through as a zero
// time so the delivery service can assign its normal recorded time.
type evidenceArgs struct {
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Reference  string     `json:"reference,omitempty"`
	RecordedAt *time.Time `json:"recordedAt,omitempty"`
}

func (args evidenceArgs) toDeliveryEvidence() delivery.Evidence {
	value := delivery.Evidence{Kind: args.Kind, Title: args.Title, Reference: args.Reference}
	if args.RecordedAt != nil {
		value.RecordedAt = args.RecordedAt.UTC()
	}
	return value
}

func (server *server) advanceGate(ctx context.Context, _ *mcp.CallToolRequest, args advanceGateArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	evidence := make([]delivery.Evidence, 0, len(args.Evidence))
	for _, value := range args.Evidence {
		evidence = append(evidence, value.toDeliveryEvidence())
	}
	item, err := server.operations.AdvanceGate(ctx, args.ID, args.ExpectedRevision, args.Gate, evidence)
	return nil, workItemOutput{Item: item}, err
}

type closeWorkItemArgs struct {
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Retrospective    string `json:"retrospective"`
}

func (server *server) closeWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args closeWorkItemArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	item, err := server.operations.Close(ctx, args.ID, args.ExpectedRevision, args.Retrospective)
	return nil, workItemOutput{Item: item}, err
}

type createReleaseArgs delivery.ReleaseInput
type releaseOutput struct {
	Release delivery.Release `json:"release"`
}

type planningListArgs struct {
	ProjectID string `json:"projectId"`
}

type releasesOutput struct {
	Releases []delivery.Release `json:"releases"`
}

func (server *server) createRelease(ctx context.Context, _ *mcp.CallToolRequest, args createReleaseArgs) (*mcp.CallToolResult, releaseOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, releaseOutput{}, err
	}
	release, err := server.operations.CreateRelease(ctx, delivery.ReleaseInput(args))
	return nil, releaseOutput{Release: release}, err
}

func (server *server) listReleases(ctx context.Context, _ *mcp.CallToolRequest, args planningListArgs) (*mcp.CallToolResult, releasesOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, releasesOutput{}, err
	}
	values, err := server.operations.ListReleases(ctx, args.ProjectID)
	return nil, releasesOutput{Releases: values}, err
}

type createSprintArgs delivery.SprintInput
type sprintOutput struct {
	Sprint delivery.Sprint `json:"sprint"`
}

type sprintsOutput struct {
	Sprints []delivery.Sprint `json:"sprints"`
}

func (server *server) createSprint(ctx context.Context, _ *mcp.CallToolRequest, args createSprintArgs) (*mcp.CallToolResult, sprintOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, sprintOutput{}, err
	}
	sprint, err := server.operations.CreateSprint(ctx, delivery.SprintInput(args))
	return nil, sprintOutput{Sprint: sprint}, err
}

func (server *server) listSprints(ctx context.Context, _ *mcp.CallToolRequest, args planningListArgs) (*mcp.CallToolResult, sprintsOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, sprintsOutput{}, err
	}
	values, err := server.operations.ListSprints(ctx, args.ProjectID)
	return nil, sprintsOutput{Sprints: values}, err
}

type createMilestoneArgs delivery.MilestoneInput
type milestoneOutput struct {
	Milestone delivery.Milestone `json:"milestone"`
}

type milestonesOutput struct {
	Milestones []delivery.Milestone `json:"milestones"`
}

func (server *server) createMilestone(ctx context.Context, _ *mcp.CallToolRequest, args createMilestoneArgs) (*mcp.CallToolResult, milestoneOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, milestoneOutput{}, err
	}
	milestone, err := server.operations.CreateMilestone(ctx, delivery.MilestoneInput(args))
	return nil, milestoneOutput{Milestone: milestone}, err
}

func (server *server) listMilestones(ctx context.Context, _ *mcp.CallToolRequest, args planningListArgs) (*mcp.CallToolResult, milestonesOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, milestonesOutput{}, err
	}
	values, err := server.operations.ListMilestones(ctx, args.ProjectID)
	return nil, milestonesOutput{Milestones: values}, err
}

type memberWeekArgs struct {
	Member    string `json:"member"`
	WeekStart string `json:"weekStart,omitempty"`
}

type memberWeekOutput struct {
	Week delivery.MemberWeek `json:"week"`
}

func (server *server) memberWeek(ctx context.Context, _ *mcp.CallToolRequest, args memberWeekArgs) (*mcp.CallToolResult, memberWeekOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, memberWeekOutput{}, err
	}
	week, err := server.operations.MemberWeek(ctx, args.Member, args.WeekStart)
	return nil, memberWeekOutput{Week: week}, err
}

type projectProgressArgs struct {
	ProjectID string `json:"projectId"`
}

type projectProgressOutput struct {
	Progress delivery.ProjectProgress `json:"progress"`
}

func (server *server) projectProgress(ctx context.Context, _ *mcp.CallToolRequest, args projectProgressArgs) (*mcp.CallToolResult, projectProgressOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, projectProgressOutput{}, err
	}
	progress, err := server.operations.ProjectProgress(ctx, args.ProjectID)
	return nil, projectProgressOutput{Progress: progress}, err
}

type projectScheduleOutput struct {
	Schedule delivery.ProjectSchedule `json:"schedule"`
}

func (server *server) projectSchedule(ctx context.Context, _ *mcp.CallToolRequest, args projectProgressArgs) (*mcp.CallToolResult, projectScheduleOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, projectScheduleOutput{}, err
	}
	schedule, err := server.operations.ProjectSchedule(ctx, args.ProjectID)
	return nil, projectScheduleOutput{Schedule: schedule}, err
}

type saveViewArgs struct {
	Name   string                  `json:"name"`
	Filter delivery.WorkItemFilter `json:"filter"`
}

type savedViewOutput struct {
	View delivery.SavedView `json:"view"`
}

func (server *server) saveView(ctx context.Context, _ *mcp.CallToolRequest, args saveViewArgs) (*mcp.CallToolResult, savedViewOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, savedViewOutput{}, err
	}
	view, err := server.operations.SaveView(ctx, delivery.SavedViewInput{Name: args.Name, Filter: args.Filter})
	return nil, savedViewOutput{View: view}, err
}

type savedViewsOutput struct {
	Views []delivery.SavedView `json:"views"`
}

func (server *server) listSavedViews(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, savedViewsOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, savedViewsOutput{}, err
	}
	views, err := server.operations.ListSavedViews(ctx)
	return nil, savedViewsOutput{Views: views}, err
}

func readTool(name, title, description string) *mcp.Tool {
	falseValue := false
	return &mcp.Tool{
		Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: &falseValue, OpenWorldHint: &falseValue, IdempotentHint: true},
	}
}

func writeTool(name, title, description string) *mcp.Tool {
	falseValue := false
	return &mcp.Tool{
		Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{Title: title, DestructiveHint: &falseValue, OpenWorldHint: &falseValue},
	}
}
