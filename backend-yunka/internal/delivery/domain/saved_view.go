package domain

import (
	"errors"
	"strings"
	"time"
)

// Shared delivery filter discriminants remain string-valued; existing constants
// and public aliases stay in delivery during the incremental migration.
type Board string
type Status string
type WorkItemKind string

var ErrCanonicalUserRequired = errors.New("saved views require a canonical user identity")

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

// NormalizeWorkItemFilter preserves the original normalization policy. Enum
// values are not trimmed or validated here; changing that would alter behavior.
func NormalizeWorkItemFilter(filter WorkItemFilter) WorkItemFilter {
	filter.ProjectID = strings.TrimSpace(filter.ProjectID)
	filter.Owner = strings.TrimSpace(filter.Owner)
	filter.ReleaseID = strings.TrimSpace(filter.ReleaseID)
	filter.SprintID = strings.TrimSpace(filter.SprintID)
	filter.MilestoneID = strings.TrimSpace(filter.MilestoneID)
	filter.Query = strings.TrimSpace(filter.Query)
	return filter
}
