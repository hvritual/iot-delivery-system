// Package mcpserver exposes the local delivery lifecycle as a Model Context
// Protocol server. It uses the same application operations as HTTP, so MCP
// calls inherit Yunka's execution, authorization, and transaction boundaries.
package mcpserver

import (
	"context"
	"errors"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yunka.io/framework/core/identity"
)

// Lifecycle is deliberately an application-use-case port rather than a
// repository. This prevents MCP tools from bypassing task validation, Outbox
// staging, and local authorization policy.
type Lifecycle interface {
	List(context.Context) ([]delivery.WorkItem, error)
	Search(context.Context, delivery.WorkItemFilter) ([]delivery.WorkItem, error)
	Create(context.Context, delivery.CreateInput) (delivery.WorkItem, error)
	UpdateWorkItem(context.Context, string, delivery.WorkItemUpdate) (delivery.WorkItem, error)
	AddComment(context.Context, string, delivery.CommentInput) (delivery.Comment, error)
	AdvanceGate(context.Context, string, delivery.Gate, []delivery.Evidence) (delivery.WorkItem, error)
	Close(context.Context, string, string) (delivery.WorkItem, error)
	CreateProject(context.Context, delivery.ProjectInput) (delivery.Project, error)
	ListProjects(context.Context) ([]delivery.Project, error)
	FindSimilar(context.Context, delivery.SimilarityQuery) ([]delivery.SimilarityCandidate, error)
	CreateRelease(context.Context, delivery.ReleaseInput) (delivery.Release, error)
	ListReleases(context.Context, string) ([]delivery.Release, error)
	CreateSprint(context.Context, delivery.SprintInput) (delivery.Sprint, error)
	ListSprints(context.Context, string) ([]delivery.Sprint, error)
	CreateMilestone(context.Context, delivery.MilestoneInput) (delivery.Milestone, error)
	ListMilestones(context.Context, string) ([]delivery.Milestone, error)
	SaveView(context.Context, delivery.SavedViewInput) (delivery.SavedView, error)
	ListSavedViews(context.Context) ([]delivery.SavedView, error)
	MemberWeek(context.Context, string, string) (delivery.MemberWeek, error)
	ProjectProgress(context.Context, string) (delivery.ProjectProgress, error)
	ProjectSchedule(context.Context, string) (delivery.ProjectSchedule, error)
}

type server struct {
	lifecycle Lifecycle
	principal identity.Principal
}

// New creates an in-process MCP server. Run it over stdio from the local
// command; it never exposes a remote MCP endpoint or sends notifications to a
// third party by itself.
func New(lifecycle Lifecycle, principal identity.Principal) *mcp.Server {
	implementation := mcp.NewServer(&mcp.Implementation{
		Name:    "iot-delivery-system",
		Version: "0.2.0",
	}, &mcp.ServerOptions{
		Instructions: "用于本地 IoT 研发交付生命周期管理。写操作仅改变本地交付系统；创建相似任务前必须先确认。",
	})
	current := &server{lifecycle: lifecycle, principal: principal}
	current.addTools(implementation)
	return implementation
}

func (server *server) addTools(target *mcp.Server) {
	mcp.AddTool(target, readTool("delivery.list_projects", "列出交付项目", "列出当前本地交付项目。"), server.listProjects)
	mcp.AddTool(target, writeTool("delivery.create_project", "创建交付项目", "在本地交付系统创建项目。"), server.createProject)
	mcp.AddTool(target, readTool("delivery.list_work_items", "查询交付事项", "按项目、负责人、状态、类型和关键词查询事项。"), server.listWorkItems)
	mcp.AddTool(target, readTool("delivery.find_similar", "检查相似事项", "在创建前检查同项目或同板块的相似事项。"), server.findSimilar)
	mcp.AddTool(target, writeTool("delivery.create_work_item", "创建交付事项", "创建事项；若存在相似候选，先返回候选，需显式确认后才创建。"), server.createWorkItem)
	mcp.AddTool(target, writeTool("delivery.update_work_item", "更新交付事项", "编辑任务的排期、进度、IoT 绑定、依赖和研发证据。"), server.updateWorkItem)
	mcp.AddTool(target, writeTool("delivery.add_comment", "新增事项评论", "为交付事项添加可审计评论。"), server.addComment)
	mcp.AddTool(target, writeTool("delivery.advance_gate", "推进交付关卡", "提交证据并推进交付关卡。"), server.advanceGate)
	mcp.AddTool(target, writeTool("delivery.close_work_item", "关闭交付事项", "在生产验证后记录复盘并关闭事项。"), server.closeWorkItem)
	mcp.AddTool(target, writeTool("delivery.create_release", "创建发布版本", "为项目创建发布版本。"), server.createRelease)
	mcp.AddTool(target, writeTool("delivery.create_sprint", "创建 Sprint", "为项目创建 Sprint。"), server.createSprint)
	mcp.AddTool(target, writeTool("delivery.create_milestone", "创建里程碑", "为项目创建里程碑。"), server.createMilestone)
	mcp.AddTool(target, readTool("delivery.get_member_week", "查看成员周任务", "查看成员在指定自然周的任务事项。"), server.memberWeek)
	mcp.AddTool(target, readTool("delivery.get_project_progress", "查看项目进度", "按估算权重汇总项目任务进度。"), server.projectProgress)
	mcp.AddTool(target, readTool("delivery.get_project_schedule", "查看项目交付健康", "查看依赖阻塞、逾期、未排期和成员剩余估算。"), server.projectSchedule)
	mcp.AddTool(target, writeTool("delivery.save_view", "保存任务视图", "保存当前本地身份的任务筛选视图。"), server.saveView)
	mcp.AddTool(target, readTool("delivery.list_saved_views", "列出保存视图", "列出当前本地身份保存的任务筛选视图。"), server.listSavedViews)
}

func (server *server) toolContext(ctx context.Context) (context.Context, error) {
	if server == nil || server.lifecycle == nil {
		return nil, errors.New("delivery MCP lifecycle is not configured")
	}
	if !server.principal.Authenticated {
		return nil, errors.New("local MCP principal is not authenticated")
	}
	return identity.WithPrincipal(ctx, server.principal), nil
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
	project, err := server.lifecycle.CreateProject(ctx, delivery.ProjectInput(args))
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
	projects, err := server.lifecycle.ListProjects(ctx)
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

func (server *server) listWorkItems(ctx context.Context, _ *mcp.CallToolRequest, args listWorkItemsArgs) (*mcp.CallToolResult, workItemsOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemsOutput{}, err
	}
	filter := delivery.WorkItemFilter{
		ProjectID: args.ProjectID, Board: args.Board, Owner: args.Owner, Status: args.Status, Kind: args.Kind,
		ReleaseID: args.ReleaseID, SprintID: args.SprintID, MilestoneID: args.MilestoneID, Query: args.Query,
	}
	items, err := server.lifecycle.Search(ctx, filter)
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
	candidates, err := server.lifecycle.FindSimilar(ctx, delivery.SimilarityQuery(args))
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
	candidates, err := server.lifecycle.FindSimilar(ctx, delivery.SimilarityQuery{
		Title: args.Title, Board: args.Board, ProjectID: args.ProjectID, Kind: kind, Limit: 5,
	})
	if err != nil {
		return nil, createWorkItemOutput{}, err
	}
	if len(candidates) > 0 && !args.ConfirmSimilar {
		return nil, createWorkItemOutput{SimilarCandidates: candidates, RequiresConfirmation: true}, nil
	}
	created, err := server.lifecycle.Create(ctx, delivery.CreateInput{
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
	ID              string                         `json:"id"`
	Title           *string                        `json:"title,omitempty"`
	Owner           *string                        `json:"owner,omitempty"`
	Priority        *delivery.Priority             `json:"priority,omitempty"`
	ReleaseID       *string                        `json:"releaseId,omitempty"`
	SprintID        *string                        `json:"sprintId,omitempty"`
	MilestoneID     *string                        `json:"milestoneId,omitempty"`
	StartDate       *string                        `json:"startDate,omitempty"`
	DueDate         *string                        `json:"dueDate,omitempty"`
	EstimatePoints  *float64                       `json:"estimatePoints,omitempty"`
	ProgressPercent *int                           `json:"progressPercent,omitempty"`
	Dependencies    *[]delivery.WorkItemDependency `json:"dependencies,omitempty"`
	IoTBindings     *[]delivery.IoTBinding         `json:"iotBindings,omitempty"`
	TraceLinks      *[]delivery.TraceLink          `json:"traceLinks,omitempty"`
}

type workItemOutput struct {
	Item delivery.WorkItem `json:"item"`
}

func (server *server) updateWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args updateWorkItemArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	item, err := server.lifecycle.UpdateWorkItem(ctx, args.ID, delivery.WorkItemUpdate{
		Title: args.Title, Owner: args.Owner, Priority: args.Priority, ReleaseID: args.ReleaseID, SprintID: args.SprintID,
		MilestoneID: args.MilestoneID, StartDate: args.StartDate, DueDate: args.DueDate, EstimatePoints: args.EstimatePoints,
		ProgressPercent: args.ProgressPercent, Dependencies: args.Dependencies, IoTBindings: args.IoTBindings, TraceLinks: args.TraceLinks,
	})
	return nil, workItemOutput{Item: item}, err
}

type addCommentArgs struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

type commentOutput struct {
	Comment delivery.Comment `json:"comment"`
}

func (server *server) addComment(ctx context.Context, _ *mcp.CallToolRequest, args addCommentArgs) (*mcp.CallToolResult, commentOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, commentOutput{}, err
	}
	comment, err := server.lifecycle.AddComment(ctx, args.ID, delivery.CommentInput{Body: args.Body})
	return nil, commentOutput{Comment: comment}, err
}

type advanceGateArgs struct {
	ID       string              `json:"id"`
	Gate     delivery.Gate       `json:"gate"`
	Evidence []delivery.Evidence `json:"evidence"`
}

func (server *server) advanceGate(ctx context.Context, _ *mcp.CallToolRequest, args advanceGateArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	item, err := server.lifecycle.AdvanceGate(ctx, args.ID, args.Gate, args.Evidence)
	return nil, workItemOutput{Item: item}, err
}

type closeWorkItemArgs struct {
	ID            string `json:"id"`
	Retrospective string `json:"retrospective"`
}

func (server *server) closeWorkItem(ctx context.Context, _ *mcp.CallToolRequest, args closeWorkItemArgs) (*mcp.CallToolResult, workItemOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, workItemOutput{}, err
	}
	item, err := server.lifecycle.Close(ctx, args.ID, args.Retrospective)
	return nil, workItemOutput{Item: item}, err
}

type createReleaseArgs delivery.ReleaseInput
type releaseOutput struct {
	Release delivery.Release `json:"release"`
}

func (server *server) createRelease(ctx context.Context, _ *mcp.CallToolRequest, args createReleaseArgs) (*mcp.CallToolResult, releaseOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, releaseOutput{}, err
	}
	release, err := server.lifecycle.CreateRelease(ctx, delivery.ReleaseInput(args))
	return nil, releaseOutput{Release: release}, err
}

type createSprintArgs delivery.SprintInput
type sprintOutput struct {
	Sprint delivery.Sprint `json:"sprint"`
}

func (server *server) createSprint(ctx context.Context, _ *mcp.CallToolRequest, args createSprintArgs) (*mcp.CallToolResult, sprintOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, sprintOutput{}, err
	}
	sprint, err := server.lifecycle.CreateSprint(ctx, delivery.SprintInput(args))
	return nil, sprintOutput{Sprint: sprint}, err
}

type createMilestoneArgs delivery.MilestoneInput
type milestoneOutput struct {
	Milestone delivery.Milestone `json:"milestone"`
}

func (server *server) createMilestone(ctx context.Context, _ *mcp.CallToolRequest, args createMilestoneArgs) (*mcp.CallToolResult, milestoneOutput, error) {
	ctx, err := server.toolContext(ctx)
	if err != nil {
		return nil, milestoneOutput{}, err
	}
	milestone, err := server.lifecycle.CreateMilestone(ctx, delivery.MilestoneInput(args))
	return nil, milestoneOutput{Milestone: milestone}, err
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
	week, err := server.lifecycle.MemberWeek(ctx, args.Member, args.WeekStart)
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
	progress, err := server.lifecycle.ProjectProgress(ctx, args.ProjectID)
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
	schedule, err := server.lifecycle.ProjectSchedule(ctx, args.ProjectID)
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
	view, err := server.lifecycle.SaveView(ctx, delivery.SavedViewInput{Name: args.Name, Filter: args.Filter})
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
	views, err := server.lifecycle.ListSavedViews(ctx)
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
