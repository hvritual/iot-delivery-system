package delivery_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/yunka.io/framework/core/identity"
)

func TestServicePlansReleaseSprintAndMilestoneThenDerivesWeeklyWorkAndProjectProgress(t *testing.T) {
	t.Parallel()

	ctx := identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		UserID:        "release-owner",
	})
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project, err := service.CreateProject(ctx, delivery.ProjectInput{
		Name:  "秋季 OTA 发布",
		Board: delivery.BoardResearchDelivery,
		Owner: "release-owner",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	release, err := service.CreateRelease(ctx, delivery.ReleaseInput{
		ProjectID:  project.ID,
		Name:       "2026.09 OTA",
		Version:    "2.8.0",
		TargetDate: "2026-09-11",
	})
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	sprint, err := service.CreateSprint(ctx, delivery.SprintInput{
		ProjectID: project.ID,
		Name:      "发布冲刺",
		StartDate: "2026-09-07",
		EndDate:   "2026-09-13",
	})
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	milestone, err := service.CreateMilestone(ctx, delivery.MilestoneInput{
		ProjectID:  project.ID,
		Name:       "灰度验收",
		TargetDate: "2026-09-10",
	})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	first, err := service.Create(ctx, delivery.CreateInput{
		Title:           "完成灰度设备刷写",
		Board:           delivery.BoardResearchDelivery,
		ProjectID:       project.ID,
		Kind:            delivery.WorkItemKindTask,
		Owner:           "release-owner",
		ReleaseID:       release.ID,
		SprintID:        sprint.ID,
		MilestoneID:     milestone.ID,
		StartDate:       "2026-09-07",
		DueDate:         "2026-09-10",
		EstimatePoints:  5,
		ProgressPercent: 40,
	})
	if err != nil {
		t.Fatalf("create scheduled work item: %v", err)
	}
	_, err = service.Create(ctx, delivery.CreateInput{
		Title:           "归档 OTA 发布证据",
		Board:           delivery.BoardResearchDelivery,
		ProjectID:       project.ID,
		Kind:            delivery.WorkItemKindTask,
		Owner:           "release-owner",
		ReleaseID:       release.ID,
		SprintID:        sprint.ID,
		MilestoneID:     milestone.ID,
		StartDate:       "2026-09-09",
		DueDate:         "2026-09-11",
		EstimatePoints:  5,
		ProgressPercent: 100,
	})
	if err != nil {
		t.Fatalf("create second scheduled work item: %v", err)
	}

	week, err := service.MemberWeek(ctx, "release-owner", "2026-09-07")
	if err != nil {
		t.Fatalf("get member weekly work: %v", err)
	}
	if week.WeekStart != "2026-09-07" || week.WeekEnd != "2026-09-13" || len(week.Items) != 2 || week.Items[0].ID != first.ID {
		t.Fatalf("weekly work = %#v, want the two sprint items", week)
	}
	progress, err := service.ProjectProgress(ctx, project.ID)
	if err != nil {
		t.Fatalf("get project progress: %v", err)
	}
	if progress.TotalItems != 2 || math.Abs(progress.ProgressPercent-70) > 0.001 {
		t.Fatalf("project progress = %#v, want 2 weighted items at 70%%", progress)
	}
	view, err := service.SaveView(ctx, delivery.SavedViewInput{
		Name:   "我的发布冲刺",
		Filter: delivery.WorkItemFilter{ProjectID: project.ID, SprintID: sprint.ID, Owner: "release-owner"},
	})
	if err != nil {
		t.Fatalf("save task view: %v", err)
	}
	if view.Owner != "release-owner" || view.ID == "" {
		t.Fatalf("saved view = %#v, want actor-owned saved view", view)
	}
	views, err := service.ListSavedViews(ctx)
	if err != nil {
		t.Fatalf("list saved task views: %v", err)
	}
	if len(views) != 1 || views[0].ID != view.ID {
		t.Fatalf("saved views = %#v, want %#v", views, view)
	}
}

func TestSavedViewsUseCanonicalUserIDAndNeverDisplayOrServiceIdentity(t *testing.T) {
	t.Parallel()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	alice := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-alice", Subject: "Alice Display Name"})
	view, err := service.SaveView(alice, delivery.SavedViewInput{Name: "Alice view"})
	if err != nil || view.Owner != "user-alice" {
		t.Fatalf("saved view = %#v err=%v, want canonical user-alice owner", view, err)
	}
	bob := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodJWT, TenantID: "org-a", UserID: "user-bob", Subject: "Alice Display Name"})
	views, err := service.ListSavedViews(bob)
	if err != nil || len(views) != 0 {
		t.Fatalf("same display name leaked another user's views: %#v err=%v", views, err)
	}
	serviceAccount := identity.WithPrincipal(context.Background(), identity.Principal{Authenticated: true, AuthMethod: identity.AuthMethodServiceToken, TenantID: "org-a", Subject: "service-account/reporter"})
	if _, err := service.SaveView(serviceAccount, delivery.SavedViewInput{Name: "service view"}); !errors.Is(err, delivery.ErrCanonicalUserRequired) {
		t.Fatalf("service identity saved a personal view: %v", err)
	}
}
