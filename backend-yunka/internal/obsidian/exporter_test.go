package obsidian_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/obsidian"
)

func TestExporterWritesTraceableLinkedDeliveryNotes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	exporter := obsidian.NewExporter(root)
	item := delivery.WorkItem{
		ID:        "IOT-DELIVERY-001",
		Title:     "设备 OTA 发布验收",
		Board:     delivery.BoardResearchDelivery,
		ProjectID: "PRJ-IOT-DELIVERY",
		ParentID:  "IOT-DELIVERY-EPIC-001",
		Kind:      delivery.WorkItemKindTask,
		Dependencies: []delivery.WorkItemDependency{{
			ItemID:   "IOT-DELIVERY-DEP-001",
			Relation: delivery.DependencyDependsOn,
		}},
		Type:            "release",
		Owner:           "研发负责人",
		Priority:        delivery.PriorityP0,
		Status:          delivery.StatusClosed,
		Gate:            delivery.GateProductionValidated,
		ReleaseID:       "REL-2.0.0",
		SprintID:        "SPRINT-2026-W36",
		MilestoneID:     "MILESTONE-GA",
		StartDate:       "2026-09-01",
		DueDate:         "2026-09-05",
		EstimatePoints:  5,
		ProgressPercent: 100,
		Plan:            "完成灰度策略与回滚演练。",
		Solution:        "按设备分组灰度，并记录发布窗口。",
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
		IoTBindings: []delivery.IoTBinding{{
			Kind:       delivery.IoTBindingDevice,
			Reference:  "SN-OTA-001",
			Label:      "灰度设备",
			Attributes: map[string]string{"region": "cn-east", "site": "lab-a"},
		}, {
			Kind:      delivery.IoTBindingRolloutBatch,
			Reference: "BATCH-CN-EAST-01",
			Label:     "华东灰度批次",
		}},
		TraceLinks: []delivery.TraceLink{{
			Kind:       delivery.TracePullRequest,
			Reference:  "PR-481",
			Title:      "OTA 回滚保护",
			URL:        "https://git.example.test/pr/481",
			Status:     "merged",
			RecordedAt: time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC),
		}, {
			Kind:       delivery.TraceTest,
			Reference:  "TEST-OTA-009",
			Title:      "灰度验收",
			Status:     "passed",
			RecordedAt: time.Date(2026, 9, 2, 10, 45, 0, 0, time.UTC),
		}},
		Comments: []delivery.Comment{{
			ID:        "COMMENT-001",
			Author:    "发布负责人",
			Body:      "已确认回滚窗口。",
			CreatedAt: time.Date(2026, 9, 2, 10, 50, 0, 0, time.UTC),
		}},
		Activities: []delivery.Activity{{
			ID:         "ACTIVITY-001",
			Type:       "work_item_updated",
			Summary:    "关联灰度批次并更新进度。",
			Actor:      "研发负责人",
			OccurredAt: time.Date(2026, 9, 2, 10, 55, 0, 0, time.UTC),
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
		if !strings.Contains(string(content), "generated_by: \"iot-delivery-system-yunka/v1\"") {
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

	plan, err := os.ReadFile(filepath.Join(root, "10-交付管理", "01-规划", "IOT-DELIVERY-001-规划.md"))
	if err != nil {
		t.Fatalf("read projected plan: %v", err)
	}
	for _, expected := range []string{
		"| 项目 | PRJ-IOT-DELIVERY |",
		"| 类型 | task |",
		"| 版本 | REL-2.0.0 |",
		"| Sprint | SPRINT-2026-W36 |",
		"| 进度 | 100% |",
		"## 依赖关系",
		"IOT-DELIVERY-DEP-001",
	} {
		if !strings.Contains(string(plan), expected) {
			t.Fatalf("projected plan is missing %q:\n%s", expected, plan)
		}
	}

	validation, err := os.ReadFile(filepath.Join(root, "10-交付管理", "04-发布与验证", "IOT-DELIVERY-001-验证.md"))
	if err != nil {
		t.Fatalf("read projected validation: %v", err)
	}
	for _, expected := range []string{
		"## IoT 交付范围",
		"SN-OTA-001",
		`{"region":"cn-east","site":"lab-a"}`,
		"BATCH-CN-EAST-01",
		"## 研发交付关联",
		"PR-481",
		"https://git.example.test/pr/481",
		"2026-09-02 18:30",
		"TEST-OTA-009",
	} {
		if !strings.Contains(string(validation), expected) {
			t.Fatalf("projected validation is missing %q:\n%s", expected, validation)
		}
	}

	retrospective, err := os.ReadFile(filepath.Join(root, "10-交付管理", "05-复盘", "IOT-DELIVERY-001-复盘.md"))
	if err != nil {
		t.Fatalf("read projected retrospective: %v", err)
	}
	for _, expected := range []string{
		"## 协作与活动",
		"发布负责人",
		"已确认回滚窗口。",
		"关联灰度批次并更新进度。",
	} {
		if !strings.Contains(string(retrospective), expected) {
			t.Fatalf("projected retrospective is missing %q:\n%s", expected, retrospective)
		}
	}
}

func TestExporterWritesDailyFiveBoardDashboardWithBoardDrillDown(t *testing.T) {
	root := t.TempDir()
	exporter := obsidian.NewExporter(root)
	items := []delivery.WorkItem{
		{
			ID:        "IOT-DAILY-001",
			Title:     "设备 OTA 灰度发布",
			Board:     delivery.BoardResearchDelivery,
			Owner:     "研发负责人",
			Priority:  delivery.PriorityP0,
			Status:    delivery.StatusBlocked,
			Gate:      delivery.GateSolutionReviewed,
			DueDate:   "2020-01-01",
			Blocker:   "等待安全评审结论",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        "IOT-DAILY-002",
			Title:     "设备连接诊断优化",
			Board:     delivery.BoardDeviceQuality,
			Owner:     "设备负责人",
			Priority:  delivery.PriorityP1,
			Status:    delivery.StatusVerifying,
			Gate:      delivery.GateTestPassed,
			UpdatedAt: time.Now().UTC(),
		},
	}

	if err := exporter.Export(context.Background(), items); err != nil {
		t.Fatalf("export daily delivery dashboard: %v", err)
	}

	dailyDirectory := filepath.Join(root, "10-交付管理", "00-每日驾驶舱")
	entries, err := os.ReadDir(dailyDirectory)
	if err != nil {
		t.Fatalf("read daily dashboard directory: %v", err)
	}
	dashboardName := ""
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "-交付驾驶舱.md") {
			dashboardName = entry.Name()
			break
		}
	}
	if dashboardName == "" {
		t.Fatal("daily dashboard file was not generated")
	}
	date := strings.TrimSuffix(dashboardName, "-交付驾驶舱.md")
	dailyRelativePath := "10-交付管理/00-每日驾驶舱/" + date + "-交付驾驶舱.md"
	daily, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dailyRelativePath)))
	if err != nil {
		t.Fatalf("read daily delivery dashboard: %v", err)
	}
	dailyContent := string(daily)
	for _, expected := range []string{
		"## 五个板块",
		"[[10-交付管理/00-每日驾驶舱/" + date + "-研发交付效能|研发交付效能]]",
		"## 今日需关注",
		"受阻：1 项",
		"逾期：1 项",
		"IOT-DAILY-001",
	} {
		if !strings.Contains(dailyContent, expected) {
			t.Fatalf("daily dashboard is missing %q:\n%s", expected, dailyContent)
		}
	}

	boardRelativePath := "10-交付管理/00-每日驾驶舱/" + date + "-研发交付效能.md"
	board, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(boardRelativePath)))
	if err != nil {
		t.Fatalf("read board drill-down note: %v", err)
	}
	if !strings.Contains(string(board), "[[10-交付管理/01-规划/IOT-DAILY-001-规划|IOT-DAILY-001 · 设备 OTA 灰度发布]]") {
		t.Fatalf("board drill-down does not link to the delivery item:\n%s", board)
	}

	overview, err := os.ReadFile(filepath.Join(root, "10-交付管理", "00-交付总览.md"))
	if err != nil {
		t.Fatalf("read delivery overview: %v", err)
	}
	if !strings.Contains(string(overview), "[["+dailyRelativePath[:len(dailyRelativePath)-3]+"|查看今日驾驶舱]]") {
		t.Fatalf("overview does not link to today's dashboard:\n%s", overview)
	}
}
