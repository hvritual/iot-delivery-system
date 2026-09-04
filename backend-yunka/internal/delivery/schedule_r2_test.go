package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"yunka.io/framework/core/identity"
)

func TestServiceRejectsCircularDependenciesOnUpdate(t *testing.T) {
	t.Parallel()

	ctx := deliveryTestContext()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project := deliveryTestProject(t, ctx, service)
	first := deliveryTestItem(t, ctx, service, project.ID, "准备灰度设备", "固件负责人", 3)
	second := deliveryTestItem(t, ctx, service, project.ID, "执行灰度发布", "发布负责人", 5)

	if _, err := service.UpdateWorkItem(ctx, second.ID, second.Revision, delivery.WorkItemUpdate{Dependencies: &[]delivery.WorkItemDependency{{
		ItemID: first.ID, Relation: delivery.DependencyDependsOn,
	}}}); err != nil {
		t.Fatalf("create one-way dependency: %v", err)
	}
	if _, err := service.UpdateWorkItem(ctx, first.ID, first.Revision, delivery.WorkItemUpdate{Dependencies: &[]delivery.WorkItemDependency{{
		ItemID: second.ID, Relation: delivery.DependencyDependsOn,
	}}}); !errors.Is(err, delivery.ErrCircularDependency) {
		t.Fatalf("reverse dependency error = %v, want ErrCircularDependency", err)
	}
}

func TestServiceBuildsProjectScheduleWithCapacityAndDeliveryRisks(t *testing.T) {
	t.Parallel()

	ctx := deliveryTestContext()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project := deliveryTestProject(t, ctx, service)
	prerequisite := deliveryTestItem(t, ctx, service, project.ID, "准备灰度设备", "固件负责人", 3)
	dependent := deliveryTestItem(t, ctx, service, project.ID, "执行灰度发布", "发布负责人", 5)
	blocked := deliveryTestItem(t, ctx, service, project.ID, "补齐回滚预案", "测试负责人", 2)

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := service.UpdateWorkItem(ctx, dependent.ID, dependent.Revision, delivery.WorkItemUpdate{
		DueDate: &yesterday,
		Dependencies: &[]delivery.WorkItemDependency{{
			ItemID: prerequisite.ID, Relation: delivery.DependencyDependsOn,
		}},
	}); err != nil {
		t.Fatalf("schedule dependent work item: %v", err)
	}
	blocker := "等待设备回归环境恢复"
	if _, err := service.UpdateContext(ctx, blocked.ID, blocked.Revision, delivery.ContextUpdate{Blocker: &blocker}); err != nil {
		t.Fatalf("block work item: %v", err)
	}

	schedule, err := service.ProjectSchedule(ctx, project.ID)
	if err != nil {
		t.Fatalf("build project schedule: %v", err)
	}
	if schedule.ProjectID != project.ID || schedule.TotalItems != 3 || schedule.ScheduledItems != 1 || schedule.UnscheduledItems != 2 {
		t.Fatalf("schedule summary = %#v, want 3 items with 1 scheduled and 2 unscheduled", schedule)
	}
	if schedule.OverdueItems != 1 || schedule.DependencyBlockedItems != 1 || schedule.BlockedItems != 1 {
		t.Fatalf("schedule risks = %#v, want one overdue, dependency-blocked, and blocked item", schedule)
	}
	capacityByOwner := make(map[string]delivery.OwnerCapacity)
	for _, capacity := range schedule.Capacity {
		capacityByOwner[capacity.Owner] = capacity
	}
	if capacityByOwner["发布负责人"].RemainingEstimatePoints != 5 {
		t.Fatalf("release-owner capacity = %#v, want five remaining points", capacityByOwner["发布负责人"])
	}
	if capacityByOwner["固件负责人"].RemainingEstimatePoints != 3 || capacityByOwner["测试负责人"].BlockedItems != 1 {
		t.Fatalf("owner capacity = %#v, want firmware remaining and one test blockage", capacityByOwner)
	}
}

func TestServiceTreatsBlocksRelationshipAsResolvedWhenBlockingItemCompletes(t *testing.T) {
	t.Parallel()

	ctx := deliveryTestContext()
	service := delivery.NewService(delivery.NewMemoryRepository(), nil)
	project := deliveryTestProject(t, ctx, service)
	blocker := deliveryTestItem(t, ctx, service, project.ID, "完成回滚验证", "研发负责人", 3)
	blocked := deliveryTestItem(t, ctx, service, project.ID, "发布生产版本", "发布负责人", 5)
	if _, err := service.UpdateWorkItem(ctx, blocker.ID, blocker.Revision, delivery.WorkItemUpdate{Dependencies: &[]delivery.WorkItemDependency{{
		ItemID: blocked.ID, Relation: delivery.DependencyBlocks,
	}}}); err != nil {
		t.Fatalf("mark work item as blocking another: %v", err)
	}
	before, err := service.ProjectSchedule(ctx, project.ID)
	if err != nil || before.DependencyBlockedItems != 1 {
		t.Fatalf("schedule before blocker completes = %#v, error=%v; want one dependency blockage", before, err)
	}
	completed := 100
	if _, err := service.UpdateWorkItem(ctx, blocker.ID, blocker.Revision+1, delivery.WorkItemUpdate{ProgressPercent: &completed}); err != nil {
		t.Fatalf("complete blocking work item: %v", err)
	}
	after, err := service.ProjectSchedule(ctx, project.ID)
	if err != nil {
		t.Fatalf("schedule after blocker completes: %v", err)
	}
	if after.DependencyBlockedItems != 0 {
		t.Fatalf("schedule after blocker completes = %#v, want no dependency blockage", after)
	}
}

func deliveryTestContext() context.Context {
	return identity.WithPrincipal(context.Background(), identity.Principal{
		Authenticated: true,
		AuthMethod:    identity.AuthMethodAPIKey,
		UserID:        "release-owner",
		Subject:       "release-owner",
	})
}

func deliveryTestProject(t *testing.T, ctx context.Context, service *delivery.Service) delivery.Project {
	t.Helper()
	project, err := service.CreateProject(ctx, delivery.ProjectInput{
		Name:  "交付排期健康测试",
		Board: delivery.BoardResearchDelivery,
		Owner: "release-owner",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return project
}

func deliveryTestItem(t *testing.T, ctx context.Context, service *delivery.Service, projectID, title, owner string, estimate float64) delivery.WorkItem {
	t.Helper()
	item, err := service.Create(ctx, delivery.CreateInput{
		Title:          title,
		Board:          delivery.BoardResearchDelivery,
		ProjectID:      projectID,
		Kind:           delivery.WorkItemKindTask,
		Owner:          owner,
		EstimatePoints: estimate,
	})
	if err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	return item
}
