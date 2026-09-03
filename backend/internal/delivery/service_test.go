package delivery

import (
	"context"
	"testing"
)

func TestServiceRequiresGateEvidenceAndRetrospective(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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

	if _, err := service.AdvanceGate(ctx, item.ID, GateSolutionReviewed, nil); err == nil {
		t.Fatal("advance without evidence should fail")
	}

	for _, nextGate := range []Gate{
		GateSolutionReviewed,
		GateDevelopmentCompleted,
		GateTestPassed,
		GateProductionValidated,
	} {
		item, err = service.AdvanceGate(ctx, item.ID, nextGate, []Evidence{{
			Kind:  "test-or-review",
			Title: "证明 " + string(nextGate) + " 已完成",
		}})
		if err != nil {
			t.Fatalf("advance to %s: %v", nextGate, err)
		}
	}

	if _, err := service.Close(ctx, item.ID, ""); err == nil {
		t.Fatal("close without a retrospective should fail")
	}

	closed, err := service.Close(ctx, item.ID, "灰度阶段发现回滚证据需要在发布前归档。")
	if err != nil {
		t.Fatalf("close delivery item: %v", err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("closed status = %q, want %q", closed.Status, StatusClosed)
	}
}
