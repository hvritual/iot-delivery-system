package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
)

// R2Service is the cohesive task-lifecycle boundary used by the expanded HTTP
// API. It intentionally remains optional so the original MVP handler can be
// embedded in a narrow test fixture without acquiring unrelated capabilities.
type R2Service interface {
	Get(context.Context, string) (delivery.WorkItem, error)
	UpdateWorkItem(context.Context, string, delivery.WorkItemUpdate) (delivery.WorkItem, error)
	AddComment(context.Context, string, delivery.CommentInput) (delivery.Comment, error)
	Search(context.Context, delivery.WorkItemFilter) ([]delivery.WorkItem, error)
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

func (api *api) requireR2(writer http.ResponseWriter) bool {
	if api != nil && api.r2Service != nil {
		return true
	}
	writeJSON(writer, http.StatusNotImplemented, map[string]string{"error": "R2 delivery management is not configured"})
	return false
}

func workItemFilterFromRequest(request *http.Request) delivery.WorkItemFilter {
	query := request.URL.Query()
	return delivery.WorkItemFilter{
		ProjectID:   strings.TrimSpace(query.Get("projectId")),
		Board:       delivery.Board(strings.TrimSpace(query.Get("board"))),
		Owner:       strings.TrimSpace(query.Get("owner")),
		Status:      delivery.Status(strings.TrimSpace(query.Get("status"))),
		Kind:        delivery.WorkItemKind(strings.TrimSpace(query.Get("kind"))),
		ReleaseID:   strings.TrimSpace(query.Get("releaseId")),
		SprintID:    strings.TrimSpace(query.Get("sprintId")),
		MilestoneID: strings.TrimSpace(query.Get("milestoneId")),
		Query:       strings.TrimSpace(query.Get("q")),
	}
}

func (api *api) item(writer http.ResponseWriter, request *http.Request, id string) {
	if !api.requireR2(writer) {
		return
	}
	item, err := api.r2Service.Get(request.Context(), id)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

type itemPatch struct {
	delivery.WorkItemUpdate
	Plan     *string            `json:"plan,omitempty"`
	Solution *string            `json:"solution,omitempty"`
	Blocker  *string            `json:"blocker,omitempty"`
	Decision *delivery.Decision `json:"decision,omitempty"`
}

func (api *api) patchItem(writer http.ResponseWriter, request *http.Request, id string) {
	var patch itemPatch
	if err := decodeJSON(request, &patch); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var item delivery.WorkItem
	var err error
	if patch.hasWorkItemUpdate() {
		if !api.requireR2(writer) {
			return
		}
		item, err = api.r2Service.UpdateWorkItem(request.Context(), id, patch.WorkItemUpdate)
		if err != nil {
			writeError(writer, err)
			return
		}
	}
	if patch.hasContextUpdate() {
		item, err = api.service.UpdateContext(request.Context(), id, delivery.ContextUpdate{
			Plan:     patch.Plan,
			Solution: patch.Solution,
			Blocker:  patch.Blocker,
			Decision: patch.Decision,
		})
		if err != nil {
			writeError(writer, err)
			return
		}
	}
	if !patch.hasWorkItemUpdate() && !patch.hasContextUpdate() {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "delivery work item update has no editable fields"})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (patch itemPatch) hasWorkItemUpdate() bool {
	return patch.Title != nil || patch.Owner != nil || patch.Priority != nil ||
		patch.ReleaseID != nil || patch.SprintID != nil || patch.MilestoneID != nil ||
		patch.StartDate != nil || patch.DueDate != nil || patch.EstimatePoints != nil ||
		patch.ProgressPercent != nil || patch.Dependencies != nil || patch.IoTBindings != nil || patch.TraceLinks != nil
}

func (patch itemPatch) hasContextUpdate() bool {
	return patch.Plan != nil || patch.Solution != nil || patch.Blocker != nil || patch.Decision != nil
}

func (api *api) comments(writer http.ResponseWriter, request *http.Request, id string) {
	if !api.requireR2(writer) {
		return
	}
	var input delivery.CommentInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	comment, err := api.r2Service.AddComment(request.Context(), id, input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *api) releases(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	if request.Method == http.MethodGet {
		values, err := api.r2Service.ListReleases(request.Context(), request.URL.Query().Get("projectId"))
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, values)
		return
	}
	var input delivery.ReleaseInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	value, err := api.r2Service.CreateRelease(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) sprints(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	if request.Method == http.MethodGet {
		values, err := api.r2Service.ListSprints(request.Context(), request.URL.Query().Get("projectId"))
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, values)
		return
	}
	var input delivery.SprintInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	value, err := api.r2Service.CreateSprint(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) milestones(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	if request.Method == http.MethodGet {
		values, err := api.r2Service.ListMilestones(request.Context(), request.URL.Query().Get("projectId"))
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, values)
		return
	}
	var input delivery.MilestoneInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	value, err := api.r2Service.CreateMilestone(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) views(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	if request.Method == http.MethodGet {
		values, err := api.r2Service.ListSavedViews(request.Context())
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, values)
		return
	}
	var input delivery.SavedViewInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	value, err := api.r2Service.SaveView(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) memberWeek(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	value, err := api.r2Service.MemberWeek(request.Context(), request.URL.Query().Get("member"), request.URL.Query().Get("weekStart"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (api *api) projectAction(writer http.ResponseWriter, request *http.Request) {
	if !api.requireR2(writer) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/projects/"), "/"), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "progress" {
		value, err := api.r2Service.ProjectProgress(request.Context(), parts[0])
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "schedule" {
		value, err := api.r2Service.ProjectSchedule(request.Context(), parts[0])
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
		return
	}
	writeJSON(writer, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
}
