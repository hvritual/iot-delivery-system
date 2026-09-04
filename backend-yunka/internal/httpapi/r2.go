package httpapi

import (
	"net/http"
	"strings"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
)

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
	item, err := api.operations.Get(request.Context(), id)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

type itemPatch struct {
	delivery.WorkItemUpdate
	ExpectedRevision int64              `json:"expectedRevision"`
	Plan             *string            `json:"plan,omitempty"`
	Solution         *string            `json:"solution,omitempty"`
	Blocker          *string            `json:"blocker,omitempty"`
	Decision         *delivery.Decision `json:"decision,omitempty"`
}

func (api *api) patchItem(writer http.ResponseWriter, request *http.Request, id string) {
	var patch itemPatch
	if err := decodeJSON(request, &patch); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var item delivery.WorkItem
	var err error
	expectedRevision := patch.ExpectedRevision
	if patch.hasWorkItemUpdate() {
		item, err = api.operations.UpdateWorkItem(request.Context(), id, expectedRevision, patch.WorkItemUpdate)
		if err != nil {
			writeError(writer, err)
			return
		}
		expectedRevision = item.Revision
	}
	if patch.hasContextUpdate() {
		item, err = api.operations.UpdateContext(request.Context(), id, expectedRevision, delivery.ContextUpdate{
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
	var input delivery.CommentInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	comment, err := api.operations.AddComment(request.Context(), id, input.ExpectedRevision, input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *api) releases(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		values, err := api.operations.ListReleases(request.Context(), request.URL.Query().Get("projectId"))
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
	value, err := api.operations.CreateRelease(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) sprints(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		values, err := api.operations.ListSprints(request.Context(), request.URL.Query().Get("projectId"))
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
	value, err := api.operations.CreateSprint(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) milestones(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		values, err := api.operations.ListMilestones(request.Context(), request.URL.Query().Get("projectId"))
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
	value, err := api.operations.CreateMilestone(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) views(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		values, err := api.operations.ListSavedViews(request.Context())
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
	value, err := api.operations.SaveView(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (api *api) memberWeek(writer http.ResponseWriter, request *http.Request) {
	value, err := api.operations.MemberWeek(request.Context(), request.URL.Query().Get("member"), request.URL.Query().Get("weekStart"))
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (api *api) projectAction(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/projects/"), "/"), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "progress" {
		value, err := api.operations.ProjectProgress(request.Context(), parts[0])
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
		return
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "schedule" {
		value, err := api.operations.ProjectSchedule(request.Context(), parts[0])
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
		return
	}
	writeJSON(writer, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
}
