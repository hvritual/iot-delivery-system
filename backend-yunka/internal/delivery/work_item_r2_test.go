package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"yunka.io/framework/core/identity"
)

func TestServiceEditsWorkItemWithAuditCommentIoTScopeAndTraceLinks(t *testing.T) {
	t.Parallel()

	ctx := identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		UserID:        "delivery-owner",
		Subject:       "delivery-owner",
	})
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project, err := service.CreateProject(ctx, delivery.ProjectInput{
		Name:  "OTA 交付治理",
		Board: delivery.BoardResearchDelivery,
		Owner: "delivery-owner",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	item, err := service.Create(ctx, delivery.CreateInput{
		Title:     "设备 OTA 灰度发布",
		Board:     delivery.BoardResearchDelivery,
		ProjectID: project.ID,
		Kind:      delivery.WorkItemKindTask,
		Owner:     "delivery-owner",
	})
	if err != nil {
		t.Fatalf("create delivery task: %v", err)
	}
	startDate := "2026-09-07"
	dueDate := "2026-09-11"
	estimatePoints := 5.0
	progress := 40
	bindings := []delivery.IoTBinding{
		{Kind: delivery.IoTBindingDevice, Reference: "SN-OTA-001", Label: "测试机 A"},
		{Kind: delivery.IoTBindingFirmware, Reference: "fw-2.8.0", Label: "2.8.0"},
		{Kind: delivery.IoTBindingEnvironment, Reference: "staging", Label: "预发布"},
		{Kind: delivery.IoTBindingRolloutBatch, Reference: "gray-batch-01", Label: "首批灰度"},
	}
	traceLinks := []delivery.TraceLink{
		{Kind: delivery.TracePullRequest, Reference: "PR-123", Title: "灰度策略实现"},
		{Kind: delivery.TraceBuild, Reference: "build-456", Title: "固件构建"},
		{Kind: delivery.TraceTest, Reference: "test-789", Title: "回归测试"},
		{Kind: delivery.TraceRelease, Reference: "release-2.8.0", Title: "发布证据"},
	}
	updated, err := service.UpdateWorkItem(ctx, item.ID, item.Revision, delivery.WorkItemUpdate{
		StartDate:       &startDate,
		DueDate:         &dueDate,
		EstimatePoints:  &estimatePoints,
		ProgressPercent: &progress,
		IoTBindings:     &bindings,
		TraceLinks:      &traceLinks,
	})
	if err != nil {
		t.Fatalf("update delivery task: %v", err)
	}
	if updated.StartDate != startDate || updated.DueDate != dueDate || updated.EstimatePoints != estimatePoints || updated.ProgressPercent != progress {
		t.Fatalf("updated task schedule = %#v, want start/due/estimate/progress fields", updated)
	}
	if len(updated.IoTBindings) != len(bindings) || len(updated.TraceLinks) != len(traceLinks) {
		t.Fatalf("updated task bindings/traces = %#v, want %d bindings and %d trace links", updated, len(bindings), len(traceLinks))
	}
	comment, err := service.AddComment(ctx, item.ID, updated.Revision, delivery.CommentInput{Body: "等待首批灰度设备完成刷写。"})
	if err != nil {
		t.Fatalf("add task comment: %v", err)
	}
	if comment.Author != "delivery-owner" {
		t.Fatalf("comment author = %q, want authenticated actor", comment.Author)
	}
	stored, err := service.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("get updated task: %v", err)
	}
	if len(stored.Comments) != 1 || len(stored.Activities) < 3 {
		t.Fatalf("task audit = comments %#v activities %#v, want create/update/comment records", stored.Comments, stored.Activities)
	}
	found, err := service.Search(ctx, delivery.WorkItemFilter{ProjectID: project.ID, Owner: "delivery-owner", Query: "灰度"})
	if err != nil {
		t.Fatalf("search delivery tasks: %v", err)
	}
	if len(found) != 1 || found[0].ID != item.ID {
		t.Fatalf("search result = %#v, want task %q", found, item.ID)
	}
	if stored.Activities[len(stored.Activities)-1].OccurredAt.Before(time.Now().UTC().Add(-time.Minute)) {
		t.Fatalf("latest audit activity timestamp = %s, want recent", stored.Activities[len(stored.Activities)-1].OccurredAt)
	}
}
