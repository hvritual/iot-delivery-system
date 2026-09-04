package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
)

func TestHandlerReturnsProjectScheduleHealth(t *testing.T) {
	t.Parallel()

	fixture := newRESTFixture(t)
	project := createJSON(t, fixture.handler, http.MethodPost, "/api/projects", `{"name":"发布健康 API","board":"研发交付效能","owner":"发布负责人"}`, http.StatusCreated)
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("project id = %#v, want non-empty", project["id"])
	}
	createJSON(t, fixture.handler, http.MethodPost, "/api/items", `{"title":"准备发布证据","board":"研发交付效能","projectId":"`+projectID+`","kind":"task","owner":"发布负责人"}`, http.StatusCreated)

	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/schedule", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("schedule response = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var schedule delivery.ProjectSchedule
	if err := json.NewDecoder(recorder.Body).Decode(&schedule); err != nil {
		t.Fatalf("decode project schedule: %v", err)
	}
	if schedule.ProjectID != projectID || schedule.TotalItems != 1 || schedule.UnscheduledItems != 1 {
		t.Fatalf("project schedule = %#v, want one unscheduled work item", schedule)
	}
}
