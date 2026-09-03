package delivery

import (
	"context"
	"testing"
)

func TestServiceRecordsPlanSolutionDecisionAndBlocker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(NewMemoryRepository(), nil)
	item, err := service.Create(ctx, CreateInput{
		Title: "设备离线率治理",
		Board: BoardDeviceQuality,
		Owner: "设备平台主管",
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	plan := "按型号和网络运营商拆分离线率。"
	solution := "对持续离线设备触发诊断任务。"
	blocker := "等待现场网络抓包。"
	updated, err := service.UpdateContext(ctx, item.ID, ContextUpdate{
		Plan:     &plan,
		Solution: &solution,
		Blocker:  &blocker,
		Decision: &Decision{
			Title:        "优先以型号聚合排查",
			Context:      "离线率异常需要可比较的分组。",
			Outcome:      "先按型号和网络运营商切分。",
			Consequences: "需要补齐设备标签。",
		},
	})
	if err != nil {
		t.Fatalf("record delivery context: %v", err)
	}
	if updated.Status != StatusBlocked || updated.Plan != plan || updated.Solution != solution {
		t.Fatalf("updated item = %#v, want blocked item with recorded plan and solution", updated)
	}
	if len(updated.Decisions) != 1 || updated.Decisions[0].ID == "" {
		t.Fatalf("updated decisions = %#v, want one identified ADR", updated.Decisions)
	}
}
