package bootstrap_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	deliveryv1 "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/httpapi"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/localauth"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestApplicationUsesYunkaRuntimeHostForDeliveryAPI(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "host-test-key")
	ctx := context.Background()
	vault := t.TempDir()
	application, err := bootstrap.New(ctx, bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: vault,
	})
	if err != nil {
		t.Fatalf("bootstrap Yunka application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown Yunka application: %v", err)
		}
	})

	if application.HTTPAddress() == "" || application.GRPCAddress() == "" {
		t.Fatalf("resolved runtime addresses are required: http=%q grpc=%q", application.HTTPAddress(), application.GRPCAddress())
	}

	health := get(t, "http://"+application.HTTPAddress()+"/health")
	if health.StatusCode != http.StatusOK || !strings.Contains(health.Body, `"status":"ok"`) {
		t.Fatalf("host health = status %d body %q", health.StatusCode, health.Body)
	}

	diagnostics := get(t, "http://"+application.HTTPAddress()+"/__yunka/diagnostics")
	if diagnostics.StatusCode != http.StatusOK || !strings.Contains(diagnostics.Body, `"schemaVersion":1`) {
		t.Fatalf("host diagnostics = status %d body %q", diagnostics.StatusCode, diagnostics.Body)
	}

	dashboardResponse := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/dashboard", "host-test-key")
	if dashboardResponse.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%q", dashboardResponse.StatusCode, dashboardResponse.Body)
	}
	var dashboard httpapi.Dashboard
	if err := json.Unmarshal([]byte(dashboardResponse.Body), &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if len(dashboard.Boards) != 5 || len(dashboard.Items) != 1 || !dashboard.Items[0].IsSample {
		t.Fatalf("dashboard = %#v, want five boards and one sample item", dashboard)
	}

	grpcCtx, grpcCancel := context.WithTimeout(ctx, 5*time.Second)
	defer grpcCancel()
	connection, err := grpc.DialContext(grpcCtx, application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial generated gRPC contract: %v", err)
	}
	defer connection.Close()
	grpcDashboard, err := deliveryv1.NewDeliveryServiceClient(connection).GetDashboard(
		metadata.AppendToOutgoingContext(ctx, strings.ToLower(localauth.APIKeyHeader), "host-test-key"),
		&deliveryv1.GetDashboardRequest{},
	)
	if err != nil {
		t.Fatalf("call generated gRPC dashboard: %v", err)
	}
	if grpcDashboard.GetDashboard() == nil || len(grpcDashboard.GetDashboard().GetBoards()) != 5 {
		t.Fatalf("gRPC dashboard = %#v, want five boards", grpcDashboard)
	}

	createRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", bytes.NewBufferString(`{"title":"Yunka 运行时验收","board":"研发交付效能","owner":"研发负责人"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set(localauth.APIKeyHeader, "host-test-key")
	createResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	createBody, err := io.ReadAll(createResponse.Body)
	_ = createResponse.Body.Close()
	if err != nil {
		t.Fatalf("read create response: %v", err)
	}
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create through hosted application status = %d body=%q, want %d", createResponse.StatusCode, createBody, http.StatusCreated)
	}
	overviewPath := filepath.Join(vault, "10-交付管理", "00-交付总览.md")
	deadline := time.Now().Add(2 * time.Second)
	for {
		contents, readErr := os.ReadFile(overviewPath)
		if readErr == nil && strings.Contains(string(contents), "Yunka 运行时验收") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outbox projection did not reach %s before deadline: %v", overviewPath, readErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationProtectsBusinessHTTPAndGeneratedGRPCWithLocalAPIKey(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "bootstrap-test-key")
	t.Setenv(localauth.ViewerAPIKeyEnvironment, "bootstrap-viewer-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("bootstrap protected application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown protected application: %v", err)
		}
	})

	if health := get(t, "http://"+application.HTTPAddress()+"/health"); health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", health.StatusCode, http.StatusOK)
	}
	if dashboard := get(t, "http://"+application.HTTPAddress()+"/api/dashboard"); dashboard.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard status = %d, want %d", dashboard.StatusCode, http.StatusUnauthorized)
	}
	request, err := http.NewRequest(http.MethodGet, "http://"+application.HTTPAddress()+"/api/dashboard", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(localauth.APIKeyHeader, "bootstrap-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated dashboard status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	viewerCreate, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{"title":"viewer cannot create","board":"研发交付效能","owner":"viewer"}`))
	if err != nil {
		t.Fatal(err)
	}
	viewerCreate.Header.Set("Content-Type", "application/json")
	viewerCreate.Header.Set(localauth.APIKeyHeader, "bootstrap-viewer-key")
	viewerResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(viewerCreate)
	if err != nil {
		t.Fatal(err)
	}
	_ = viewerResponse.Body.Close()
	if viewerResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want %d", viewerResponse.StatusCode, http.StatusForbidden)
	}

	grpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := grpc.DialContext(grpcCtx, application.GRPCAddress(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("dial generated gRPC contract: %v", err)
	}
	defer connection.Close()
	client := deliveryv1.NewDeliveryServiceClient(connection)
	if _, err := client.GetDashboard(context.Background(), &deliveryv1.GetDashboardRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated gRPC dashboard error = %v, want Unauthenticated", err)
	}
	authenticatedCtx := metadata.AppendToOutgoingContext(context.Background(), strings.ToLower(localauth.APIKeyHeader), "bootstrap-test-key")
	if _, err := client.GetDashboard(authenticatedCtx, &deliveryv1.GetDashboardRequest{}); err != nil {
		t.Fatalf("authenticated gRPC dashboard: %v", err)
	}
}

func TestApplicationCreatesProjectThroughAuthorizedRuntime(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "project-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/projects", strings.NewReader(`{
		"name":"OTA 2026.09 发布",
		"board":"研发交付效能",
		"owner":"发布负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read create project response: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create project through hosted application status = %d body=%q, want %d", response.StatusCode, body, http.StatusCreated)
	}
	var project map[string]any
	if err := json.Unmarshal(body, &project); err != nil {
		t.Fatalf("decode hosted project: %v", err)
	}
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("hosted project id = %#v, want non-empty", project["id"])
	}

	itemRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"完成 OTA 发布范围",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"projectId":"`+projectID+`",
		"kind":"epic"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	itemRequest.Header.Set("Content-Type", "application/json")
	itemRequest.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	itemResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(itemRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer itemResponse.Body.Close()
	itemBody, err := io.ReadAll(itemResponse.Body)
	if err != nil {
		t.Fatalf("read hosted item response: %v", err)
	}
	if itemResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create hosted work item status = %d body=%q, want %d", itemResponse.StatusCode, itemBody, http.StatusCreated)
	}
	var item map[string]any
	if err := json.Unmarshal(itemBody, &item); err != nil {
		t.Fatalf("decode hosted work item: %v", err)
	}
	if item["projectId"] != projectID || item["kind"] != "epic" {
		t.Fatalf("hosted hierarchy = %#v, want project=%q kind=epic", item, projectID)
	}

	listRequest, err := http.NewRequest(http.MethodGet, "http://"+application.HTTPAddress()+"/api/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	listRequest.Header.Set(localauth.APIKeyHeader, "project-runtime-test-key")
	listResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(listRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	listBody, err := io.ReadAll(listResponse.Body)
	if err != nil {
		t.Fatalf("read hosted list response: %v", err)
	}
	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("list hosted work items status = %d body=%q, want %d", listResponse.StatusCode, listBody, http.StatusOK)
	}
	var items []map[string]any
	if err := json.Unmarshal(listBody, &items); err != nil {
		t.Fatalf("decode hosted work item list: %v", err)
	}
	if len(items) < 2 || items[0]["projectId"] != projectID || items[0]["kind"] != "epic" {
		t.Fatalf("hosted work item list = %#v, want project hierarchy fields", items)
	}
}

func TestApplicationDeliversWorkItemEventsToLocalNotificationInbox(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "notification-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	createRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"通知本地收件箱验收",
		"board":"研发交付效能",
		"owner":"通知负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set(localauth.APIKeyHeader, "notification-runtime-test-key")
	createResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(createRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer createResponse.Body.Close()
	createBody, err := io.ReadAll(createResponse.Body)
	if err != nil {
		t.Fatalf("read created work item: %v", err)
	}
	if createResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create work item status = %d body=%q, want %d", createResponse.StatusCode, createBody, http.StatusCreated)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatalf("decode created work item: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created work item id = %q, want non-empty", created.ID)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		response := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/notifications", "notification-runtime-test-key")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("list local notifications status = %d body=%q, want %d", response.StatusCode, response.Body, http.StatusOK)
		}
		var notifications []struct {
			Channel   string `json:"channel"`
			EventType string `json:"eventType"`
			Subject   string `json:"subject"`
		}
		if err := json.Unmarshal([]byte(response.Body), &notifications); err != nil {
			t.Fatalf("decode local notifications: %v", err)
		}
		for _, notification := range notifications {
			if notification.Channel == "local-inbox" && notification.EventType == "delivery.work-item.created" && notification.Subject == created.ID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("local notification inbox = %#v, want creation event for %q", notifications, created.ID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationSchedulesDueRemindersThroughTheDurableOutbox(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "due-reminder-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
		DueReminder:   delivery.DueReminderConfig{LeadDays: 0, Interval: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("bootstrap application with reminder worker: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"运行时截止提醒验收",
		"board":"研发交付效能",
		"owner":"发布负责人",
		"dueDate":"`+time.Now().UTC().Format("2006-01-02")+`"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "due-reminder-runtime-test-key")
	created, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(created.Body)
		if readErr != nil {
			t.Fatalf("read create response: %v", readErr)
		}
		t.Fatalf("create due item status = %d body=%q", created.StatusCode, body)
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&item); err != nil || item.ID == "" {
		t.Fatalf("decode due item = %#v, error=%v", item, err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		response := getWithAPIKey(t, "http://"+application.HTTPAddress()+"/api/notifications", "due-reminder-runtime-test-key")
		var notifications []struct {
			EventType string `json:"eventType"`
			Subject   string `json:"subject"`
		}
		if response.StatusCode == http.StatusOK && json.Unmarshal([]byte(response.Body), &notifications) == nil {
			for _, notification := range notifications {
				if notification.EventType == delivery.DueReminderEventType && notification.Subject == item.ID {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("due reminder notifications = %#v, want reminder for %q", notifications, item.ID)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestApplicationRegistersAdditionalNotificationChannelsAtAssembly(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "notification-channel-assembly-test-key")
	delivered := make(chan notification.Notification, 1)
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
		NotificationChannels: []notification.Channel{
			recordingNotificationChannel{deliveries: delivered},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap application with additional channel: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/items", strings.NewReader(`{
		"title":"可插拔通道装配验收",
		"board":"研发交付效能",
		"owner":"通知负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(localauth.APIKeyHeader, "notification-channel-assembly-test-key")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read create response: %v", readErr)
		}
		t.Fatalf("create work item status = %d body=%q, want %d", response.StatusCode, body, http.StatusCreated)
	}

	select {
	case deliveredNotification := <-delivered:
		if deliveredNotification.Channel != "recording-test" || deliveredNotification.EventType != "delivery.work-item.created" {
			t.Fatalf("additional notification delivery = %#v, want recording creation event", deliveredNotification)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("additional notification channel did not receive the work-item event")
	}
}

type recordingNotificationChannel struct {
	deliveries chan<- notification.Notification
}

func (channel recordingNotificationChannel) Name() string {
	return "recording-test"
}

func (channel recordingNotificationChannel) Deliver(_ context.Context, value notification.Notification) error {
	channel.deliveries <- value
	return nil
}

func TestApplicationRunsR2PlanningThroughAuthorizedRuntime(t *testing.T) {
	t.Setenv(localauth.APIKeyEnvironment, "r2-runtime-test-key")
	application, err := bootstrap.New(context.Background(), bootstrap.Config{
		HTTPAddress:   "127.0.0.1:0",
		GRPCAddress:   "127.0.0.1:0",
		DatabasePath:  filepath.Join(t.TempDir(), "iot-delivery-yunka.db"),
		ObsidianVault: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("bootstrap application: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := application.Close(shutdownCtx); err != nil {
			t.Errorf("shutdown application: %v", err)
		}
	})

	projectRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/projects", strings.NewReader(`{
		"name":"R2 运行时计划验收", "board":"研发交付效能", "owner":"发布负责人"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	projectRequest.Header.Set("Content-Type", "application/json")
	projectRequest.Header.Set(localauth.APIKeyHeader, "r2-runtime-test-key")
	projectResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(projectRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer projectResponse.Body.Close()
	var project struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(projectResponse.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if projectResponse.StatusCode != http.StatusCreated || project.ID == "" {
		t.Fatalf("create runtime project status=%d project=%#v", projectResponse.StatusCode, project)
	}

	releaseRequest, err := http.NewRequest(http.MethodPost, "http://"+application.HTTPAddress()+"/api/releases", strings.NewReader(`{
		"projectId":"`+project.ID+`", "name":"R2 版本", "version":"2.9.0", "targetDate":"2026-09-11"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	releaseRequest.Header.Set("Content-Type", "application/json")
	releaseRequest.Header.Set(localauth.APIKeyHeader, "r2-runtime-test-key")
	releaseResponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(releaseRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseResponse.Body.Close()
	body, err := io.ReadAll(releaseResponse.Body)
	if err != nil {
		t.Fatalf("read release response: %v", err)
	}
	if releaseResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create runtime release status=%d body=%q, want %d", releaseResponse.StatusCode, body, http.StatusCreated)
	}
}

type response struct {
	StatusCode int
	Body       string
}

func get(t *testing.T, url string) response {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	result, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: result.StatusCode, Body: string(body)}
}

func getWithAPIKey(t *testing.T, url, apiKey string) response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(localauth.APIKeyHeader, apiKey)
	result, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{StatusCode: result.StatusCode, Body: string(body)}
}
