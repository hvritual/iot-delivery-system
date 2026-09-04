package delivery

import (
	"context"
	"testing"
)

func TestServiceRequiresGateEvidenceAndRetrospective(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	implementer := humanPrincipalContext(t, "service-test-implementer")
	reviewer := humanPrincipalContext(t, "service-test-reviewer")
	service := NewService(NewMemoryRepository(), nil)
	item, err := service.Create(ctx, CreateInput{
		Title:    "设备 OTA 发布验收",
		Board:    BoardResearchDelivery,
		Owner:    "研发负责人",
		Priority: PriorityP0,
		Plan:     "先完成灰度发布与回滚演练。",
	})
	if err != nil {
		t.Fatalf("create delivery item: %v", err)
	}

	if _, err := service.AdvanceGate(ctx, item.ID, item.Revision, GateSolutionReviewed, nil); err == nil {
		t.Fatal("advance without evidence should fail")
	}

	for _, nextGate := range []Gate{
		GateSolutionReviewed,
		GateDevelopmentCompleted,
		GateTestPassed,
		GateProductionValidated,
	} {
		actor := implementer
		if nextGate == GateProductionValidated {
			actor = reviewer
		}
		item, err = service.AdvanceGate(actor, item.ID, item.Revision, nextGate, []Evidence{{
			Kind:  "test-or-review",
			Title: "证明 " + string(nextGate) + " 已完成",
		}})
		if err != nil {
			t.Fatalf("advance to %s: %v", nextGate, err)
		}
	}

	if _, err := service.Close(reviewer, item.ID, item.Revision, ""); err == nil {
		t.Fatal("close without a retrospective should fail")
	}

	closed, err := service.Close(reviewer, item.ID, item.Revision, "灰度阶段发现回滚证据需要在发布前归档。")
	if err != nil {
		t.Fatalf("close delivery item: %v", err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("closed status = %q, want %q", closed.Status, StatusClosed)
	}
}

func TestServiceRejectsExactDuplicateWorkItemsWithinAProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(NewMemoryRepository(), nil)
	project, err := service.CreateProject(ctx, ProjectInput{
		Name:  "OTA 2026.09 发布",
		Board: BoardResearchDelivery,
		Owner: "发布负责人",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	input := CreateInput{
		Title:     "完成灰度设备清单",
		Board:     BoardResearchDelivery,
		ProjectID: project.ID,
		Kind:      WorkItemKindTask,
		Owner:     "测试负责人",
	}
	if _, err := service.Create(ctx, input); err != nil {
		t.Fatalf("create first work item: %v", err)
	}
	if _, err := service.Create(ctx, input); err == nil {
		t.Fatal("create exact duplicate work item should fail")
	}
}
