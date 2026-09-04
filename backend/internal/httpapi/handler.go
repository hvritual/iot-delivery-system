package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend/internal/delivery"
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

func (dashboard Dashboard) Board(board delivery.Board) BoardSummary {
	for _, summary := range dashboard.Boards {
		if summary.Board == board {
			return summary
		}
	}
	return BoardSummary{Board: board}
}

func NewHandler(service *delivery.Service) http.Handler {
	mux := http.NewServeMux()
	api := &api{service: service}
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /api/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/items", api.items)
	mux.HandleFunc("POST /api/items", api.items)
	mux.HandleFunc("POST /api/items/", api.itemAction)
	mux.HandleFunc("PATCH /api/items/", api.itemAction)
	return mux
}

type api struct {
	service *delivery.Service
}

func (api *api) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *api) dashboard(writer http.ResponseWriter, request *http.Request) {
	items, err := api.service.List(request.Context())
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, makeDashboard(items))
}

func (api *api) items(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
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

func (api *api) itemAction(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/items/"), "/"), "/")
	if request.Method == http.MethodPatch && len(parts) == 1 {
		var input delivery.ContextUpdate
		if err := decodeJSON(request, &input); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		item, err := api.service.UpdateContext(request.Context(), parts[0], input)
		if err != nil {
			writeError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
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
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func writeError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, delivery.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, delivery.ErrEvidenceRequired),
		errors.Is(err, delivery.ErrInvalidGateTransition),
		errors.Is(err, delivery.ErrRetrospectiveRequired),
		errors.Is(err, delivery.ErrReleaseNotValidated):
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
