package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend/internal/httpapi"
)

func TestHandlerCreatesAdvancesAndSummarizesDeliveryItems(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{
		"title":"设备 OTA 发布验收",
		"board":"研发交付效能",
		"owner":"研发负责人",
		"priority":"P0",
		"plan":"完成灰度与回滚演练。"
	}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create response = %d, want %d: %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}
	var created delivery.WorkItem
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("decode created item: %v", err)
	}

	advanceRequest := httptest.NewRequest(http.MethodPost, "/api/items/"+created.ID+"/gates/solution_reviewed", bytes.NewBufferString(`{
		"evidence":[{"kind":"review","title":"方案评审通过","reference":"ADR-001"}]
	}`))
	advanceRequest.Header.Set("Content-Type", "application/json")
	advanceRecorder := httptest.NewRecorder()
	handler.ServeHTTP(advanceRecorder, advanceRequest)
	if advanceRecorder.Code != http.StatusOK {
		t.Fatalf("advance response = %d, want %d: %s", advanceRecorder.Code, http.StatusOK, advanceRecorder.Body.String())
	}

	dashboardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(dashboardRecorder, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashboardRecorder.Code != http.StatusOK {
		t.Fatalf("dashboard response = %d, want %d", dashboardRecorder.Code, http.StatusOK)
	}
	var dashboard httpapi.Dashboard
	if err := json.NewDecoder(dashboardRecorder.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if len(dashboard.Items) != 1 || dashboard.Items[0].Gate != delivery.GateSolutionReviewed {
		t.Fatalf("dashboard items = %#v, want one solution-reviewed item", dashboard.Items)
	}
	if dashboard.Board(delivery.BoardResearchDelivery).Active != 1 {
		t.Fatalf("research delivery summary = %#v, want one active item", dashboard.Board(delivery.BoardResearchDelivery))
	}
}

func TestHandlerRecordsDeliveryContext(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{
		"title":"设备离线率治理", "board":"设备质量与连接", "owner":"设备平台主管"
	}`)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create response = %d, want %d", createRecorder.Code, http.StatusCreated)
	}
	var created delivery.WorkItem
	if err := json.NewDecoder(createRecorder.Body).Decode(&created); err != nil {
		t.Fatalf("decode created item: %v", err)
	}

	request := httptest.NewRequest(http.MethodPatch, "/api/items/"+created.ID, bytes.NewBufferString(`{
		"plan":"按型号拆分离线率。",
		"solution":"持续离线自动创建诊断任务。",
		"blocker":"等待现场网络抓包。",
		"decision":{"title":"先按型号聚合", "outcome":"先完成型号维度分析"}
	}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("context response = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var updated delivery.WorkItem
	if err := json.NewDecoder(recorder.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated item: %v", err)
	}
	if updated.Status != delivery.StatusBlocked || len(updated.Decisions) != 1 {
		t.Fatalf("updated item = %#v, want blocked item with one decision", updated)
	}
}
