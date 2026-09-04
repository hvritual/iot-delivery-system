package delivery

import "time"

type Board string

const (
	BoardDeviceQuality    Board = "设备质量与连接"
	BoardProductPlatform  Board = "产品与平台能力"
	BoardResearchDelivery Board = "研发交付效能"
	BoardOperations       Board = "运营保障与安全"
	BoardCustomerValue    Board = "客户与业务价值"
)

type Priority string

const (
	PriorityP0 Priority = "P0"
	PriorityP1 Priority = "P1"
	PriorityP2 Priority = "P2"
)

// WorkItemKind makes the delivery hierarchy explicit while retaining Type for
// backwards compatibility with existing MVP records.
type WorkItemKind string

const (
	WorkItemKindEpic    WorkItemKind = "epic"
	WorkItemKindTask    WorkItemKind = "task"
	WorkItemKindSubtask WorkItemKind = "subtask"
	WorkItemKindDefect  WorkItemKind = "defect"
)

type DependencyRelation string

const (
	DependencyDependsOn DependencyRelation = "depends_on"
	DependencyBlocks    DependencyRelation = "blocks"
	DependencyRelated   DependencyRelation = "related"
)

type WorkItemDependency struct {
	ItemID   string             `json:"itemId"`
	Relation DependencyRelation `json:"relation"`
}

type Project struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"-"`
	Name           string    `json:"name"`
	Board          Board     `json:"board"`
	Owner          string    `json:"owner"`
	Description    string    `json:"description,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type ProjectInput struct {
	OrganizationID string `json:"-"`
	Name           string `json:"name"`
	Board          Board  `json:"board"`
	Owner          string `json:"owner"`
	Description    string `json:"description"`
}

// Release, Sprint, and Milestone are first-class planning records. Work items
// refer to them by ID so a project can retain scheduling history even as task
// text changes over time.
type Release struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Status      string    `json:"status,omitempty"`
	TargetDate  string    `json:"targetDate,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ReleaseInput struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	TargetDate  string `json:"targetDate"`
	Description string `json:"description"`
}

type Sprint struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	Goal      string    `json:"goal,omitempty"`
	StartDate string    `json:"startDate"`
	EndDate   string    `json:"endDate"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SprintInput struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Goal      string `json:"goal"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Status    string `json:"status"`
}

type Milestone struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	TargetDate  string    `json:"targetDate"`
	Status      string    `json:"status,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type MilestoneInput struct {
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	TargetDate  string `json:"targetDate"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

type SimilarityQuery struct {
	Title     string       `json:"title"`
	Board     Board        `json:"board"`
	ProjectID string       `json:"projectId,omitempty"`
	Kind      WorkItemKind `json:"kind,omitempty"`
	Limit     int          `json:"limit,omitempty"`
}

// SimilarityCandidate intentionally embeds the task so API consumers can show
// the candidate without an additional lookup round-trip.
type SimilarityCandidate struct {
	WorkItem
	Score float64 `json:"score"`
	Exact bool    `json:"exact"`
}

type Status string

const (
	StatusPlanned    Status = "planned"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusVerifying  Status = "verifying"
	StatusReleased   Status = "released"
	StatusClosed     Status = "closed"
)

type Gate string

const (
	GatePlanning             Gate = "planning"
	GateSolutionReviewed     Gate = "solution_reviewed"
	GateDevelopmentCompleted Gate = "development_completed"
	GateTestPassed           Gate = "test_passed"
	GateProductionValidated  Gate = "production_validated"
)

type Evidence struct {
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Reference  string    `json:"reference,omitempty"`
	RecordedAt time.Time `json:"recordedAt"`
}

// IoTBinding keeps the delivery scope explicitly connected to the real-world
// object it affects. It is intentionally reference based: the MVP stores no
// device or customer master data and therefore cannot accidentally become the
// authority for those external systems.
type IoTBindingKind string

const (
	IoTBindingDevice       IoTBindingKind = "device"
	IoTBindingFirmware     IoTBindingKind = "firmware"
	IoTBindingCustomer     IoTBindingKind = "customer"
	IoTBindingEnvironment  IoTBindingKind = "environment"
	IoTBindingRolloutBatch IoTBindingKind = "rollout_batch"
)

type IoTBinding struct {
	Kind       IoTBindingKind    `json:"kind"`
	Reference  string            `json:"reference"`
	Label      string            `json:"label,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// TraceLink describes evidence held by engineering delivery systems. The
// external record remains the source of truth; this link is the auditable
// association from a work item to its PR, build, test, defect, or release.
type TraceKind string

const (
	TracePullRequest TraceKind = "pull_request"
	TraceBuild       TraceKind = "build"
	TraceTest        TraceKind = "test"
	TraceDefect      TraceKind = "defect"
	TraceRelease     TraceKind = "release"
)

type TraceLink struct {
	Kind       TraceKind `json:"kind"`
	Reference  string    `json:"reference"`
	Title      string    `json:"title,omitempty"`
	URL        string    `json:"url,omitempty"`
	Status     string    `json:"status,omitempty"`
	RecordedAt time.Time `json:"recordedAt"`
}

type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	// WorkItemRevision is populated only on mutation results, so a caller can
	// continue a sequence with the server-confirmed aggregate revision.
	WorkItemRevision int64 `json:"revision,omitempty"`
}

type Activity struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Summary    string    `json:"summary"`
	Actor      string    `json:"actor"`
	OccurredAt time.Time `json:"occurredAt"`
}

type Decision struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Context      string    `json:"context"`
	Outcome      string    `json:"outcome"`
	Consequences string    `json:"consequences"`
	CreatedAt    time.Time `json:"createdAt"`
}

type WorkItem struct {
	ID                            string               `json:"id"`
	Revision                      int64                `json:"revision"`
	Title                         string               `json:"title"`
	Board                         Board                `json:"board"`
	ProjectID                     string               `json:"projectId,omitempty"`
	ParentID                      string               `json:"parentId,omitempty"`
	Kind                          WorkItemKind         `json:"kind,omitempty"`
	Dependencies                  []WorkItemDependency `json:"dependencies,omitempty"`
	Type                          string               `json:"type"`
	Owner                         string               `json:"owner"`
	Priority                      Priority             `json:"priority"`
	Status                        Status               `json:"status"`
	Gate                          Gate                 `json:"gate"`
	ReleaseID                     string               `json:"releaseId,omitempty"`
	SprintID                      string               `json:"sprintId,omitempty"`
	MilestoneID                   string               `json:"milestoneId,omitempty"`
	StartDate                     string               `json:"startDate,omitempty"`
	DueDate                       string               `json:"dueDate,omitempty"`
	EstimatePoints                float64              `json:"estimatePoints,omitempty"`
	ProgressPercent               int                  `json:"progressPercent"`
	Plan                          string               `json:"plan,omitempty"`
	Solution                      string               `json:"solution,omitempty"`
	Decisions                     []Decision           `json:"decisions,omitempty"`
	Evidence                      []Evidence           `json:"evidence,omitempty"`
	IoTBindings                   []IoTBinding         `json:"iotBindings,omitempty"`
	TraceLinks                    []TraceLink          `json:"traceLinks,omitempty"`
	Comments                      []Comment            `json:"comments,omitempty"`
	Activities                    []Activity           `json:"activities,omitempty"`
	ImplementationPrincipal       PrincipalSource      `json:"implementationPrincipal,omitzero"`
	ProductionValidationPrincipal PrincipalSource      `json:"productionValidationPrincipal,omitzero"`
	Retrospective                 string               `json:"retrospective,omitempty"`
	Blocker                       string               `json:"blocker,omitempty"`
	IsSample                      bool                 `json:"isSample"`
	CreatedAt                     time.Time            `json:"createdAt"`
	UpdatedAt                     time.Time            `json:"updatedAt"`
}

// PrincipalSource is a server-derived execution identity. It is not accepted
// from any client request, so it remains suitable for separation-of-duties
// decisions after SQLite persistence and process restart.
type PrincipalSource struct {
	Kind       string `json:"kind"`
	AuthMethod string `json:"authMethod"`
	SubjectID  string `json:"subjectId"`
	TenantID   string `json:"tenantId"`
}

type CreateInput struct {
	Title           string               `json:"title"`
	Board           Board                `json:"board"`
	ProjectID       string               `json:"projectId"`
	ParentID        string               `json:"parentId"`
	Kind            WorkItemKind         `json:"kind"`
	Dependencies    []WorkItemDependency `json:"dependencies"`
	Type            string               `json:"type"`
	Owner           string               `json:"owner"`
	Priority        Priority             `json:"priority"`
	ReleaseID       string               `json:"releaseId"`
	SprintID        string               `json:"sprintId"`
	MilestoneID     string               `json:"milestoneId"`
	StartDate       string               `json:"startDate"`
	DueDate         string               `json:"dueDate"`
	EstimatePoints  float64              `json:"estimatePoints"`
	ProgressPercent int                  `json:"progressPercent"`
	Plan            string               `json:"plan"`
	Solution        string               `json:"solution"`
	IoTBindings     []IoTBinding         `json:"iotBindings"`
	TraceLinks      []TraceLink          `json:"traceLinks"`
	IsSample        bool                 `json:"isSample"`
}

// WorkItemUpdate holds only explicitly supplied editable fields. Pointer
// fields distinguish an omitted field from a deliberate zero/empty value.
type WorkItemUpdate struct {
	Title           *string               `json:"title,omitempty"`
	Owner           *string               `json:"owner,omitempty"`
	Priority        *Priority             `json:"priority,omitempty"`
	ReleaseID       *string               `json:"releaseId,omitempty"`
	SprintID        *string               `json:"sprintId,omitempty"`
	MilestoneID     *string               `json:"milestoneId,omitempty"`
	StartDate       *string               `json:"startDate,omitempty"`
	DueDate         *string               `json:"dueDate,omitempty"`
	EstimatePoints  *float64              `json:"estimatePoints,omitempty"`
	ProgressPercent *int                  `json:"progressPercent,omitempty"`
	Dependencies    *[]WorkItemDependency `json:"dependencies,omitempty"`
	IoTBindings     *[]IoTBinding         `json:"iotBindings,omitempty"`
	TraceLinks      *[]TraceLink          `json:"traceLinks,omitempty"`
}

type CommentInput struct {
	Body             string `json:"body"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// WorkItemFilter is deliberately reusable by item search, saved views, and
// member/week reporting. All non-empty fields compose with AND semantics.
type WorkItemFilter struct {
	ProjectID   string       `json:"projectId,omitempty"`
	Board       Board        `json:"board,omitempty"`
	Owner       string       `json:"owner,omitempty"`
	Status      Status       `json:"status,omitempty"`
	Kind        WorkItemKind `json:"kind,omitempty"`
	ReleaseID   string       `json:"releaseId,omitempty"`
	SprintID    string       `json:"sprintId,omitempty"`
	MilestoneID string       `json:"milestoneId,omitempty"`
	Query       string       `json:"query,omitempty"`
}

type SavedView struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Owner     string         `json:"owner"`
	Filter    WorkItemFilter `json:"filter"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type SavedViewInput struct {
	Name   string         `json:"name"`
	Filter WorkItemFilter `json:"filter"`
}

type MemberWeek struct {
	Member    string     `json:"member"`
	WeekStart string     `json:"weekStart"`
	WeekEnd   string     `json:"weekEnd"`
	Items     []WorkItem `json:"items"`
}

// ProjectProgress is a weighted rollup. The denominator uses estimate points
// when present and a weight of one for unestimated items; epics are containers
// and therefore excluded to avoid double counting their descendants.
type ProjectProgress struct {
	ProjectID       string  `json:"projectId"`
	TotalItems      int     `json:"totalItems"`
	CompletedItems  int     `json:"completedItems"`
	TotalWeight     float64 `json:"totalWeight"`
	CompletedWeight float64 `json:"completedWeight"`
	ProgressPercent float64 `json:"progressPercent"`
}

// OwnerCapacity is the current project load grouped by delivery owner. The
// values intentionally use estimate points (or one point for an unestimated
// actionable item) so the schedule remains useful before a team introduces a
// separate time-tracking system.
type OwnerCapacity struct {
	Owner                   string  `json:"owner"`
	ItemCount               int     `json:"itemCount"`
	BlockedItems            int     `json:"blockedItems"`
	OverdueItems            int     `json:"overdueItems"`
	TotalEstimatePoints     float64 `json:"totalEstimatePoints"`
	CompletedEstimatePoints float64 `json:"completedEstimatePoints"`
	RemainingEstimatePoints float64 `json:"remainingEstimatePoints"`
}

// ScheduleRisk explains a delivery-health signal with the work item that
// requires attention. It is deliberately compact so the desktop cockpit, MCP,
// and Obsidian projections can use the same stable record.
type ScheduleRisk struct {
	ItemID  string `json:"itemId"`
	Title   string `json:"title"`
	Owner   string `json:"owner"`
	DueDate string `json:"dueDate,omitempty"`
	Reason  string `json:"reason"`
}

// ProjectSchedule complements the weighted ProjectProgress roll-up. It makes
// risk and remaining owner load visible without claiming to be a replacement
// for a workforce planning system.
type ProjectSchedule struct {
	ProjectID              string          `json:"projectId"`
	AsOfDate               string          `json:"asOfDate"`
	TotalItems             int             `json:"totalItems"`
	ScheduledItems         int             `json:"scheduledItems"`
	UnscheduledItems       int             `json:"unscheduledItems"`
	BlockedItems           int             `json:"blockedItems"`
	OverdueItems           int             `json:"overdueItems"`
	DependencyBlockedItems int             `json:"dependencyBlockedItems"`
	Capacity               []OwnerCapacity `json:"capacity"`
	Risks                  []ScheduleRisk  `json:"risks"`
}

type ContextUpdate struct {
	Plan     *string   `json:"plan,omitempty"`
	Solution *string   `json:"solution,omitempty"`
	Blocker  *string   `json:"blocker,omitempty"`
	Decision *Decision `json:"decision,omitempty"`
}
