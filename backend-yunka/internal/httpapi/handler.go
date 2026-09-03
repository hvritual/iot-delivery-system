package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"yunka.io/gateway/authz"
)

type Dashboard struct {
	Boards      []BoardSummary      `json:"boards"`
	Items       []delivery.WorkItem `json:"items"`
	GeneratedAt time.Time           `json:"generatedAt"`
}

type BoardSummary struct {
	Board     delivery.Board `json:"board"`
	Total     int            `json:"total"`
	Active    int            `json:"active"`
	Blocked   int            `json:"blocked"`
	Verifying int            `json:"verifying"`
	Released  int            `json:"released"`
	Closed    int            `json:"closed"`
}

type Service interface {
	Dashboard(context.Context) ([]delivery.WorkItem, error)
	List(context.Context) ([]delivery.WorkItem, error)
	Create(context.Context, delivery.CreateInput) (delivery.WorkItem, error)
	UpdateContext(context.Context, string, delivery.ContextUpdate) (delivery.WorkItem, error)
	AdvanceGate(context.Context, string, delivery.Gate, []delivery.Evidence) (delivery.WorkItem, error)
	Close(context.Context, string, string) (delivery.WorkItem, error)
}

type ProjectService interface {
	CreateProject(context.Context, delivery.ProjectInput) (delivery.Project, error)
	ListProjects(context.Context) ([]delivery.Project, error)
}

type SimilarityService interface {
	FindSimilar(context.Context, delivery.SimilarityQuery) ([]delivery.SimilarityCandidate, error)
}

type NotificationService interface {
	ListNotifications(context.Context, int) ([]notification.Notification, error)
}

func (dashboard Dashboard) Board(board delivery.Board) BoardSummary {
	for _, summary := range dashboard.Boards {
		if summary.Board == board {
			return summary
		}
	}
	return BoardSummary{Board: board}
}

func NewHandler(service Service) http.Handler {
	mux := http.NewServeMux()
	api := newAPI(service)
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /api/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/items", api.items)
	mux.HandleFunc("POST /api/items", api.items)
	mux.HandleFunc("GET /api/items/", api.itemAction)
	mux.HandleFunc("POST /api/items/similarity", api.similarity)
	mux.HandleFunc("POST /api/items/", api.itemAction)
	mux.HandleFunc("PATCH /api/items/", api.itemAction)
	mux.HandleFunc("GET /api/projects", api.projects)
	mux.HandleFunc("POST /api/projects", api.projects)
	mux.HandleFunc("GET /api/projects/", api.projectAction)
	mux.HandleFunc("GET /api/releases", api.releases)
	mux.HandleFunc("POST /api/releases", api.releases)
	mux.HandleFunc("GET /api/sprints", api.sprints)
	mux.HandleFunc("POST /api/sprints", api.sprints)
	mux.HandleFunc("GET /api/milestones", api.milestones)
	mux.HandleFunc("POST /api/milestones", api.milestones)
	mux.HandleFunc("GET /api/views", api.views)
	mux.HandleFunc("POST /api/views", api.views)
	mux.HandleFunc("GET /api/member-week", api.memberWeek)
	mux.HandleFunc("GET /api/notifications", api.notifications)
	return mux
}

// Register adds only business routes to a host-owned mux. It lets Yunka's
// runtime host reserve /health and diagnostics without changing the UI API.
func Register(mux *http.ServeMux, service Service) {
	if mux == nil {
		return
	}
	api := newAPI(service)
	mux.HandleFunc("GET /api/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/items", api.items)
	mux.HandleFunc("POST /api/items", api.items)
	mux.HandleFunc("GET /api/items/", api.itemAction)
	mux.HandleFunc("POST /api/items/similarity", api.similarity)
	mux.HandleFunc("POST /api/items/", api.itemAction)
	mux.HandleFunc("PATCH /api/items/", api.itemAction)
	mux.HandleFunc("GET /api/projects", api.projects)
	mux.HandleFunc("POST /api/projects", api.projects)
	mux.HandleFunc("GET /api/projects/", api.projectAction)
	mux.HandleFunc("GET /api/releases", api.releases)
	mux.HandleFunc("POST /api/releases", api.releases)
	mux.HandleFunc("GET /api/sprints", api.sprints)
	mux.HandleFunc("POST /api/sprints", api.sprints)
	mux.HandleFunc("GET /api/milestones", api.milestones)
	mux.HandleFunc("POST /api/milestones", api.milestones)
	mux.HandleFunc("GET /api/views", api.views)
	mux.HandleFunc("POST /api/views", api.views)
	mux.HandleFunc("GET /api/member-week", api.memberWeek)
	mux.HandleFunc("GET /api/notifications", api.notifications)
}

type api struct {
	service             Service
	projectsService     ProjectService
	similarityService   SimilarityService
	notificationService NotificationService
	r2Service           R2Service
}

func newAPI(service Service) *api {
	projects, _ := service.(ProjectService)
	similarity, _ := service.(SimilarityService)
	notifications, _ := service.(NotificationService)
	r2, _ := service.(R2Service)
	return &api{service: service, projectsService: projects, similarityService: similarity, notificationService: notifications, r2Service: r2}
}

func (api *api) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *api) dashboard(writer http.ResponseWriter, request *http.Request) {
	items, err := api.service.Dashboard(request.Context())
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, makeDashboard(items))
}

func (api *api) items(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		if api.r2Service != nil {
			items, err := api.r2Service.Search(request.Context(), workItemFilterFromRequest(request))
			if err != nil {
				writeError(writer, err)
				return
			}
			writeJSON(writer, http.StatusOK, items)
			return
		}
		items, err := api.service.List(request.Context())
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, items)
		return
	}
	var input delivery.CreateInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, err := api.service.Create(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, item)
}

func (api *api) projects(writer http.ResponseWriter, request *http.Request) {
	if api.projectsService == nil {
		writeJSON(writer, http.StatusNotImplemented, map[string]string{"error": "project management is not configured"})
		return
	}
	if request.Method == http.MethodGet {
		projects, err := api.projectsService.ListProjects(request.Context())
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, projects)
		return
	}
	var input delivery.ProjectInput
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	project, err := api.projectsService.CreateProject(request.Context(), input)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, project)
}

func (api *api) similarity(writer http.ResponseWriter, request *http.Request) {
	if api.similarityService == nil {
		writeJSON(writer, http.StatusNotImplemented, map[string]string{"error": "work item similarity is not configured"})
		return
	}
	var query delivery.SimilarityQuery
	if err := decodeJSON(request, &query); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	candidates, err := api.similarityService.FindSimilar(request.Context(), query)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, candidates)
}

func (api *api) notifications(writer http.ResponseWriter, request *http.Request) {
	if api.notificationService == nil {
		writeJSON(writer, http.StatusNotImplemented, map[string]string{"error": "notification inbox is not configured"})
		return
	}
	limit := 0
	if rawLimit := strings.TrimSpace(request.URL.Query().Get("limit")); rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil || value < 1 || value > 200 {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "notification limit must be between 1 and 200"})
			return
		}
		limit = value
	}
	notifications, err := api.notificationService.ListNotifications(request.Context(), limit)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, notifications)
}

func (api *api) itemAction(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/items/"), "/"), "/")
	if request.Method == http.MethodGet && len(parts) == 1 {
		api.item(writer, request, parts[0])
		return
	}
	if request.Method == http.MethodPatch && len(parts) == 1 {
		api.patchItem(writer, request, parts[0])
		return
	}
	if request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "comments" {
		api.comments(writer, request, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "gates" {
		var input struct {
			Evidence []delivery.Evidence `json:"evidence"`
		}
		if err := decodeJSON(request, &input); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		item, err := api.service.AdvanceGate(request.Context(), parts[0], delivery.Gate(parts[2]), input.Evidence)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
		return
	}
	if len(parts) == 2 && parts[1] == "close" {
		var input struct {
			Retrospective string `json:"retrospective"`
		}
		if err := decodeJSON(request, &input); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		item, err := api.service.Close(request.Context(), parts[0], input.Retrospective)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
		return
	}
	writeJSON(writer, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
}

func makeDashboard(items []delivery.WorkItem) Dashboard {
	boards := []delivery.Board{
		delivery.BoardDeviceQuality,
		delivery.BoardProductPlatform,
		delivery.BoardResearchDelivery,
		delivery.BoardOperations,
		delivery.BoardCustomerValue,
	}
	summaries := make(map[delivery.Board]BoardSummary, len(boards))
	for _, board := range boards {
		summaries[board] = BoardSummary{Board: board}
	}
	for _, item := range items {
		summary, exists := summaries[item.Board]
		if !exists {
			summary = BoardSummary{Board: item.Board}
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
		summaries[item.Board] = summary
	}
	result := Dashboard{Items: items, GeneratedAt: time.Now().UTC()}
	for _, board := range boards {
		result.Boards = append(result.Boards, summaries[board])
	}
	return result
}

func decodeJSON(request *http.Request, target any) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case authz.IsDenied(err):
		status = http.StatusForbidden
		var denied *authz.DeniedError
		if errors.As(err, &denied) && (denied.Decision.Reason == authz.ReasonUnauthenticated || denied.Decision.Reason == authz.ReasonAuthenticationMethod) {
			status = http.StatusUnauthorized
		}
	case errors.Is(err, delivery.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, delivery.ErrDuplicateWorkItem):
		status = http.StatusConflict
	case errors.Is(err, delivery.ErrEvidenceRequired),
		errors.Is(err, delivery.ErrInvalidGateTransition),
		errors.Is(err, delivery.ErrRetrospectiveRequired),
		errors.Is(err, delivery.ErrReleaseNotValidated),
		errors.Is(err, delivery.ErrProjectParentMismatch),
		errors.Is(err, delivery.ErrInvalidDependency),
		errors.Is(err, delivery.ErrInvalidIoTBinding),
		errors.Is(err, delivery.ErrInvalidTraceLink),
		errors.Is(err, delivery.ErrInvalidWorkItemUpdate),
		errors.Is(err, delivery.ErrPlanningScopeMismatch):
		status = http.StatusUnprocessableEntity
	case strings.Contains(err.Error(), "required"):
		status = http.StatusBadRequest
	}
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
