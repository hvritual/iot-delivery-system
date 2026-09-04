package obsidian_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend/internal/obsidian"
)

func TestExporterWritesTraceableLinkedDeliveryNotes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	exporter := obsidian.NewExporter(root)
	item := delivery.WorkItem{
		ID:       "IOT-DELIVERY-001",
		Title:    "设备 OTA 发布验收",
		Board:    delivery.BoardResearchDelivery,
		Type:     "release",
		Owner:    "研发负责人",
		Priority: delivery.PriorityP0,
		Status:   delivery.StatusClosed,
		Gate:     delivery.GateProductionValidated,
		Plan:     "完成灰度策略与回滚演练。",
		Solution: "按设备分组灰度，并记录发布窗口。",
		Decisions: []delivery.Decision{{
			ID:           "ADR-IOT-DELIVERY-001-001",
			Title:        "采用分组灰度发布",
			Context:      "设备型号与网络质量存在差异。",
			Outcome:      "先覆盖低风险门店。",
			Consequences: "需要保存回滚证据。",
			CreatedAt:    time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC),
		}},
		Evidence: []delivery.Evidence{{
			Kind:       "test",
			Title:      "灰度发布验收通过",
			Reference:  "TEST-20260902-01",
			RecordedAt: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC),
		}},
		Retrospective: "把回滚演练纳入发布门禁。",
		UpdatedAt:     time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC),
	}

	if err := exporter.Export(context.Background(), []delivery.WorkItem{item}); err != nil {
		t.Fatalf("export delivery notes: %v", err)
	}

	paths := []string{
		"10-交付管理/00-交付总览.md",
		"10-交付管理/01-规划/IOT-DELIVERY-001-规划.md",
		"10-交付管理/02-方案/IOT-DELIVERY-001-方案.md",
		"10-交付管理/03-决策/ADR-IOT-DELIVERY-001-001.md",
		"10-交付管理/04-发布与验证/IOT-DELIVERY-001-验证.md",
		"10-交付管理/05-复盘/IOT-DELIVERY-001-复盘.md",
	}
	for _, relativePath := range paths {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read generated note %s: %v", relativePath, err)
		}
		if !strings.Contains(string(content), "generated_by: \"iot-delivery-system/v1\"") {
			t.Fatalf("%s is missing the generated-source marker", relativePath)
		}
	}

	overview, err := os.ReadFile(filepath.Join(root, "10-交付管理", "00-交付总览.md"))
	if err != nil {
		t.Fatalf("read overview: %v", err)
	}
	if !strings.Contains(string(overview), "[[10-交付管理/01-规划/IOT-DELIVERY-001-规划]]") {
		t.Fatal("overview does not link to the item plan")
	}
	if !strings.Contains(string(overview), "已复盘关闭") || !strings.Contains(string(overview), "生产验证") {
		t.Fatal("overview does not translate internal status and gate values into delivery language")
	}
}
