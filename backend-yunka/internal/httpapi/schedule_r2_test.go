package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
)

func TestHandlerReturnsProjectScheduleHealth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project, err := service.CreateProject(ctx, delivery.ProjectInput{
		Name:  "发布健康 API",
		Board: delivery.BoardResearchDelivery,
		Owner: "发布负责人",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := service.Create(ctx, delivery.CreateInput{
		Title:     "准备发布证据",
		Board:     delivery.BoardResearchDelivery,
		ProjectID: project.ID,
		Kind:      delivery.WorkItemKindTask,
		Owner:     "发布负责人",
	}); err != nil {
		t.Fatalf("create work item: %v", err)
	}

	recorder := httptest.NewRecorder()
	httpapi.NewHandler(service).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/schedule", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("schedule response = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var schedule delivery.ProjectSchedule
	if err := json.NewDecoder(recorder.Body).Decode(&schedule); err != nil {
		t.Fatalf("decode project schedule: %v", err)
	}
	if schedule.ProjectID != project.ID || schedule.TotalItems != 1 || schedule.UnscheduledItems != 1 {
		t.Fatalf("project schedule = %#v, want one unscheduled work item", schedule)
	}
}
