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

type Decision struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Context      string    `json:"context"`
	Outcome      string    `json:"outcome"`
	Consequences string    `json:"consequences"`
	CreatedAt    time.Time `json:"createdAt"`
}

type WorkItem struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Board         Board      `json:"board"`
	Type          string     `json:"type"`
	Owner         string     `json:"owner"`
	Priority      Priority   `json:"priority"`
	Status        Status     `json:"status"`
	Gate          Gate       `json:"gate"`
	DueDate       string     `json:"dueDate,omitempty"`
	Plan          string     `json:"plan,omitempty"`
	Solution      string     `json:"solution,omitempty"`
	Decisions     []Decision `json:"decisions,omitempty"`
	Evidence      []Evidence `json:"evidence,omitempty"`
	Retrospective string     `json:"retrospective,omitempty"`
	Blocker       string     `json:"blocker,omitempty"`
	IsSample      bool       `json:"isSample"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type CreateInput struct {
	Title    string   `json:"title"`
	Board    Board    `json:"board"`
	Type     string   `json:"type"`
	Owner    string   `json:"owner"`
	Priority Priority `json:"priority"`
	DueDate  string   `json:"dueDate"`
	Plan     string   `json:"plan"`
	Solution string   `json:"solution"`
	IsSample bool     `json:"isSample"`
}

type ContextUpdate struct {
	Plan     *string   `json:"plan,omitempty"`
	Solution *string   `json:"solution,omitempty"`
	Blocker  *string   `json:"blocker,omitempty"`
	Decision *Decision `json:"decision,omitempty"`
}
