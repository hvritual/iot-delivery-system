package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
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
	if len(dashboard.Boards) != 5 {
		t.Fatalf("dashboard boards = %d, want five", len(dashboard.Boards))
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

func TestHandlerReturnsUnprocessableWhenGateEvidenceIsMissing(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{"title":"网关验收","board":"产品与平台能力","owner":"平台负责人"}`)))
	var item delivery.WorkItem
	if err := json.NewDecoder(create.Body).Decode(&item); err != nil {
		t.Fatalf("decode created item: %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/items/"+item.ID+"/gates/solution_reviewed", bytes.NewBufferString(`{"evidence":[]}`)))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing-evidence response = %d, want %d: %s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
}

func TestHandlerCreatesProjectAndNestedWorkItems(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)

	projectRecorder := httptest.NewRecorder()
	handler.ServeHTTP(projectRecorder, httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewBufferString(`{
		"name":"OTA 2026.09 发布",
		"board":"研发交付效能",
		"owner":"发布负责人"
	}`)))
	if projectRecorder.Code != http.StatusCreated {
		t.Fatalf("create project response = %d, want %d: %s", projectRecorder.Code, http.StatusCreated, projectRecorder.Body.String())
	}
	var project map[string]any
	if err := json.NewDecoder(projectRecorder.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("project id = %#v, want non-empty", project["id"])
	}

	epicRecorder := httptest.NewRecorder()
	handler.ServeHTTP(epicRecorder, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{
		"title":"完成 OTA 发布范围",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"projectId":"`+projectID+`",
		"kind":"epic"
	}`)))
	if epicRecorder.Code != http.StatusCreated {
		t.Fatalf("create epic response = %d, want %d: %s", epicRecorder.Code, http.StatusCreated, epicRecorder.Body.String())
	}
	var epic map[string]any
	if err := json.NewDecoder(epicRecorder.Body).Decode(&epic); err != nil {
		t.Fatalf("decode epic: %v", err)
	}
	epicID, _ := epic["id"].(string)
	if epicID == "" {
		t.Fatalf("epic id = %#v, want non-empty", epic["id"])
	}

	childRecorder := httptest.NewRecorder()
	handler.ServeHTTP(childRecorder, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{
		"title":"验证灰度设备清单",
		"board":"研发交付效能",
		"owner":"测试负责人",
		"projectId":"`+projectID+`",
		"parentId":"`+epicID+`",
		"kind":"subtask"
	}`)))
	if childRecorder.Code != http.StatusCreated {
		t.Fatalf("create child response = %d, want %d: %s", childRecorder.Code, http.StatusCreated, childRecorder.Body.String())
	}
	var child map[string]any
	if err := json.NewDecoder(childRecorder.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if child["projectId"] != projectID || child["parentId"] != epicID || child["kind"] != "subtask" {
		t.Fatalf("child hierarchy = %#v, want project=%q parent=%q kind=subtask", child, projectID, epicID)
	}
	childID, _ := child["id"].(string)
	dependentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(dependentRecorder, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(`{
		"title":"执行 OTA 灰度发布",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"projectId":"`+projectID+`",
		"kind":"task",
		"dependencies":[{"itemId":"`+childID+`","relation":"depends_on"}]
	}`)))
	if dependentRecorder.Code != http.StatusCreated {
		t.Fatalf("create dependent response = %d, want %d: %s", dependentRecorder.Code, http.StatusCreated, dependentRecorder.Body.String())
	}
	var dependent map[string]any
	if err := json.NewDecoder(dependentRecorder.Body).Decode(&dependent); err != nil {
		t.Fatalf("decode dependent item: %v", err)
	}
	dependencies, _ := dependent["dependencies"].([]any)
	if len(dependencies) != 1 {
		t.Fatalf("dependent item = %#v, want one dependency", dependent)
	}
}

func TestHandlerReturnsSimilarWorkItemCandidates(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project, err := service.CreateProject(context.Background(), delivery.ProjectInput{
		Name:  "OTA 2026.09 发布",
		Board: delivery.BoardResearchDelivery,
		Owner: "发布负责人",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := service.Create(context.Background(), delivery.CreateInput{
		Title:     "验证 OTA 灰度发布",
		Board:     delivery.BoardResearchDelivery,
		ProjectID: project.ID,
		Kind:      delivery.WorkItemKindTask,
		Owner:     "测试负责人",
	}); err != nil {
		t.Fatalf("create work item: %v", err)
	}
	handler := httpapi.NewHandler(service)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/items/similarity", bytes.NewBufferString(`{
		"title":"OTA 灰度发布验证",
		"board":"研发交付效能",
		"projectId":"`+project.ID+`",
		"kind":"task"
	}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("similarity response = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var candidates []map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&candidates); err != nil {
		t.Fatalf("decode candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0]["id"] == "" {
		t.Fatalf("candidates = %#v, want one matching work item", candidates)
	}
}

func TestHandlerReturnsConflictForDuplicateWorkItem(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)
	project := createJSON(t, handler, http.MethodPost, "/api/projects", `{
		"name":"OTA 发布重复事项验收",
		"board":"研发交付效能",
		"owner":"发布负责人"
	}`, http.StatusCreated)
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("project id = %#v, want non-empty", project["id"])
	}
	body := `{
		"title":"执行 OTA 灰度发布",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"projectId":"` + projectID + `",
		"kind":"task"
	}`
	createJSON(t, handler, http.MethodPost, "/api/items", body, http.StatusCreated)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/items", bytes.NewBufferString(body)))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("duplicate work item response = %d, want %d: %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestHandlerSupportsPlanningTaskAuditSearchSavedViewsAndProgress(t *testing.T) {
	t.Parallel()

	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	handler := httpapi.NewHandler(service)
	project := createJSON(t, handler, http.MethodPost, "/api/projects", `{"name":"OTA 发布","board":"研发交付效能","owner":"发布负责人"}`, http.StatusCreated)
	projectID, _ := project["id"].(string)
	release := createJSON(t, handler, http.MethodPost, "/api/releases", `{"projectId":"`+projectID+`","name":"OTA 2.8","version":"2.8.0","targetDate":"2026-09-11"}`, http.StatusCreated)
	sprint := createJSON(t, handler, http.MethodPost, "/api/sprints", `{"projectId":"`+projectID+`","name":"发布冲刺","startDate":"2026-09-07","endDate":"2026-09-13"}`, http.StatusCreated)
	milestone := createJSON(t, handler, http.MethodPost, "/api/milestones", `{"projectId":"`+projectID+`","name":"灰度验收","targetDate":"2026-09-10"}`, http.StatusCreated)
	item := createJSON(t, handler, http.MethodPost, "/api/items", `{
		"title":"执行 OTA 灰度发布",
		"board":"研发交付效能",
		"projectId":"`+projectID+`",
		"kind":"task",
		"owner":"发布负责人",
		"releaseId":"`+release["id"].(string)+`",
		"sprintId":"`+sprint["id"].(string)+`",
		"milestoneId":"`+milestone["id"].(string)+`",
		"startDate":"2026-09-07",
		"dueDate":"2026-09-10",
		"estimatePoints":5,
		"progressPercent":40
	}`, http.StatusCreated)
	itemID, _ := item["id"].(string)

	patch := httptest.NewRequest(http.MethodPatch, "/api/items/"+itemID, bytes.NewBufferString(`{
		"progressPercent":60,
		"iotBindings":[{"kind":"device","reference":"SN-001","label":"测试机"}],
		"traceLinks":[{"kind":"pull_request","reference":"PR-88","title":"灰度发布实现"}]
	}`))
	patch.Header.Set("Content-Type", "application/json")
	patched := httptest.NewRecorder()
	handler.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK {
		t.Fatalf("edit work item response = %d, want %d: %s", patched.Code, http.StatusOK, patched.Body.String())
	}
	var edited delivery.WorkItem
	if err := json.NewDecoder(patched.Body).Decode(&edited); err != nil {
		t.Fatalf("decode edited work item: %v", err)
	}
	if edited.ProgressPercent != 60 || len(edited.IoTBindings) != 1 || len(edited.TraceLinks) != 1 {
		t.Fatalf("edited item = %#v, want progress and IoT/trace links", edited)
	}

	comment := httptest.NewRequest(http.MethodPost, "/api/items/"+itemID+"/comments", bytes.NewBufferString(`{"body":"首批门店已进入灰度。"}`))
	comment.Header.Set("Content-Type", "application/json")
	commented := httptest.NewRecorder()
	handler.ServeHTTP(commented, comment)
	if commented.Code != http.StatusCreated {
		t.Fatalf("comment response = %d, want %d: %s", commented.Code, http.StatusCreated, commented.Body.String())
	}

	search := httptest.NewRecorder()
	handler.ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/api/items?projectId="+projectID+"&q=灰度", nil))
	if search.Code != http.StatusOK {
		t.Fatalf("search response = %d, want %d: %s", search.Code, http.StatusOK, search.Body.String())
	}
	var items []delivery.WorkItem
	if err := json.NewDecoder(search.Body).Decode(&items); err != nil {
		t.Fatalf("decode task search: %v", err)
	}
	if len(items) != 1 || len(items[0].Comments) != 1 {
		t.Fatalf("task search = %#v, want edited commented task", items)
	}

	view := createJSON(t, handler, http.MethodPost, "/api/views", `{"name":"OTA 灰度","filter":{"projectId":"`+projectID+`","sprintId":"`+sprint["id"].(string)+`"}}`, http.StatusCreated)
	if view["id"] == "" {
		t.Fatalf("saved view = %#v, want an ID", view)
	}
	views := httptest.NewRecorder()
	handler.ServeHTTP(views, httptest.NewRequest(http.MethodGet, "/api/views", nil))
	if views.Code != http.StatusOK || !bytes.Contains(views.Body.Bytes(), []byte("OTA 灰度")) {
		t.Fatalf("saved views response = %d body=%s", views.Code, views.Body.String())
	}

	week := httptest.NewRecorder()
	handler.ServeHTTP(week, httptest.NewRequest(http.MethodGet, "/api/member-week?member=发布负责人&weekStart=2026-09-07", nil))
	if week.Code != http.StatusOK || !bytes.Contains(week.Body.Bytes(), []byte(itemID)) {
		t.Fatalf("member week response = %d body=%s", week.Code, week.Body.String())
	}
	progress := httptest.NewRecorder()
	handler.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/progress", nil))
	if progress.Code != http.StatusOK || !bytes.Contains(progress.Body.Bytes(), []byte(`"progressPercent":60`)) {
		t.Fatalf("project progress response = %d body=%s", progress.Code, progress.Body.String())
	}
}

func createJSON(t *testing.T, handler http.Handler, method, path, body string, expectedStatus int) map[string]any {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != expectedStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, recorder.Code, expectedStatus, recorder.Body.String())
	}
	var value map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&value); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return value
}
