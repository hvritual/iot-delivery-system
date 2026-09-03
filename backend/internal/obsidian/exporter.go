package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend/internal/delivery"
)

const generatedBy = "iot-delivery-system/v1"

type Exporter struct {
	root string
}

func NewExporter(root string) *Exporter {
	return &Exporter{root: filepath.Clean(root)}
}

func (exporter *Exporter) Export(ctx context.Context, items []delivery.WorkItem) error {
	if exporter == nil || strings.TrimSpace(exporter.root) == "" || exporter.root == "." {
		return fmt.Errorf("obsidian vault root is required")
	}
	items = append([]delivery.WorkItem(nil), items...)
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })

	if err := exporter.write("10-交付管理/README.md", readme()); err != nil {
		return err
	}
	if err := exporter.write("10-交付管理/00-交付总览.md", overview(items)); err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := exporter.write(itemPath("01-规划", item, "规划"), plan(item)); err != nil {
			return err
		}
		if err := exporter.write(itemPath("02-方案", item, "方案"), solution(item)); err != nil {
			return err
		}
		for index, decision := range item.Decisions {
			decisionID := safeName(decision.ID)
			if decisionID == "" {
				decisionID = fmt.Sprintf("ADR-%s-%03d", safeName(item.ID), index+1)
			}
			if err := exporter.write(filepath.ToSlash(filepath.Join("10-交付管理", "03-决策", decisionID+".md")), decisionNote(item, decision)); err != nil {
				return err
			}
		}
		if err := exporter.write(itemPath("04-发布与验证", item, "验证"), validation(item)); err != nil {
			return err
		}
		if err := exporter.write(itemPath("05-复盘", item, "复盘"), retrospective(item)); err != nil {
			return err
		}
	}
	return nil
}

func (exporter *Exporter) write(relativePath, content string) error {
	target := filepath.Join(exporter.root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create note directory: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", relativePath, err)
	}
	return nil
}

func readme() string {
	return "---\n" +
		"generated_by: \"" + generatedBy + "\"\n" +
		"source_of_truth: \"iot-delivery-system\"\n" +
		"---\n\n" +
		"# 交付管理沉淀\n\n" +
		"本目录由 `iot-delivery-system` 单向生成。系统数据库是任务状态的唯一主数据源；如需修改任务，请在交付系统中操作，再等待投影刷新。\n\n" +
		"- [[00-交付总览|查看交付总览]]\n"
}

func overview(items []delivery.WorkItem) string {
	var builder strings.Builder
	builder.WriteString(generatedHeader("交付总览", ""))
	builder.WriteString("## 当前事项\n\n")
	if len(items) == 0 {
		builder.WriteString("暂无交付事项。请在交付系统中创建第一条事项。\n")
		return builder.String()
	}
	builder.WriteString("| 事项 | 板块 | 负责人 | 优先级 | 状态 | 当前关卡 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, item := range items {
		id := safeName(item.ID)
		fmt.Fprintf(&builder, "| [[10-交付管理/01-规划/%s-规划]]<br>%s | %s | %s | %s | %s | %s |\n", id, markdownCell(item.Title), markdownCell(string(item.Board)), markdownCell(item.Owner), markdownCell(string(item.Priority)), markdownCell(statusLabel(item.Status)), markdownCell(gateLabel(item.Gate)))
	}
	builder.WriteString("\n## 进入板块\n\n")
	for _, board := range []delivery.Board{
		delivery.BoardDeviceQuality,
		delivery.BoardProductPlatform,
		delivery.BoardResearchDelivery,
		delivery.BoardOperations,
		delivery.BoardCustomerValue,
	} {
		fmt.Fprintf(&builder, "- %s\n", board)
	}
	return builder.String()
}

func plan(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 规划", item.ID) +
		itemMetadata(item) +
		"## 规划\n\n" + emptyAsUnknown(item.Plan) + "\n\n" +
		"## 关联沉淀\n\n" + links(item)
}

func solution(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 方案", item.ID) +
		itemMetadata(item) +
		"## 方案\n\n" + emptyAsUnknown(item.Solution) + "\n\n" +
		"## 关联沉淀\n\n" + links(item)
}

func validation(item delivery.WorkItem) string {
	var builder strings.Builder
	builder.WriteString(generatedHeader(item.Title+" · 发布与验证", item.ID))
	builder.WriteString(itemMetadata(item))
	builder.WriteString("## 关卡\n\n")
	fmt.Fprintf(&builder, "当前关卡：`%s`  \\n状态：`%s`\n\n", gateLabel(item.Gate), statusLabel(item.Status))
	builder.WriteString("## 证据\n\n")
	if len(item.Evidence) == 0 {
		builder.WriteString("未知：尚未记录关卡证据。\n")
	} else {
		builder.WriteString("| 类型 | 证据 | 引用 | 记录时间 |\n| --- | --- | --- | --- |\n")
		for _, evidence := range item.Evidence {
			fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", markdownCell(evidence.Kind), markdownCell(evidence.Title), markdownCell(evidence.Reference), formatTime(evidence.RecordedAt))
		}
	}
	builder.WriteString("\n## 关联沉淀\n\n")
	builder.WriteString(links(item))
	return builder.String()
}

func retrospective(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 复盘", item.ID) +
		itemMetadata(item) +
		"## 复盘\n\n" + emptyAsUnknown(item.Retrospective) + "\n\n" +
		"## 关联沉淀\n\n" + links(item)
}

func decisionNote(item delivery.WorkItem, decision delivery.Decision) string {
	return generatedHeader(decision.Title, item.ID) +
		"## 决策上下文\n\n" + emptyAsUnknown(decision.Context) + "\n\n" +
		"## 决策结果\n\n" + emptyAsUnknown(decision.Outcome) + "\n\n" +
		"## 影响与后果\n\n" + emptyAsUnknown(decision.Consequences) + "\n\n" +
		"## 关联事项\n\n" + "- [[10-交付管理/01-规划/" + safeName(item.ID) + "-规划|" + item.Title + "]]\n"
}

func generatedHeader(title, itemID string) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "generated_by: \"%s\"\n", generatedBy)
	builder.WriteString("source_of_truth: \"iot-delivery-system\"\n")
	if itemID != "" {
		fmt.Fprintf(&builder, "source_item: \"%s\"\n", safeName(itemID))
	}
	builder.WriteString("---\n\n# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	return builder.String()
}

func itemMetadata(item delivery.WorkItem) string {
	return fmt.Sprintf("| 属性 | 值 |\n| --- | --- |\n| 板块 | %s |\n| 负责人 | %s |\n| 优先级 | %s |\n| 状态 | %s |\n| 关卡 | %s |\n| 到期日 | %s |\n| 最后更新 | %s |\n\n", markdownCell(string(item.Board)), markdownCell(item.Owner), markdownCell(string(item.Priority)), markdownCell(statusLabel(item.Status)), markdownCell(gateLabel(item.Gate)), markdownCell(item.DueDate), formatTime(item.UpdatedAt))
}

func links(item delivery.WorkItem) string {
	id := safeName(item.ID)
	var builder strings.Builder
	fmt.Fprintf(&builder, "- [[10-交付管理/01-规划/%s-规划|规划]]\n- [[10-交付管理/02-方案/%s-方案|方案]]\n- [[10-交付管理/04-发布与验证/%s-验证|发布与验证]]\n- [[10-交付管理/05-复盘/%s-复盘|复盘]]\n", id, id, id, id)
	for index, decision := range item.Decisions {
		decisionID := safeName(decision.ID)
		if decisionID == "" {
			decisionID = fmt.Sprintf("ADR-%s-%03d", id, index+1)
		}
		fmt.Fprintf(&builder, "- [[10-交付管理/03-决策/%s|%s]]\n", decisionID, decision.Title)
	}
	return builder.String()
}

func itemPath(folder string, item delivery.WorkItem, suffix string) string {
	return filepath.ToSlash(filepath.Join("10-交付管理", folder, safeName(item.ID)+"-"+suffix+".md"))
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return -1
		}
	}, value)
	return value
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	if value == "" {
		return "—"
	}
	return value
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知：尚未在交付系统中记录。"
	}
	return strings.TrimSpace(value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "未知"
	}
	return value.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04")
}

func statusLabel(status delivery.Status) string {
	switch status {
	case delivery.StatusPlanned:
		return "待推进"
	case delivery.StatusInProgress:
		return "进行中"
	case delivery.StatusBlocked:
		return "受阻"
	case delivery.StatusVerifying:
		return "验证中"
	case delivery.StatusReleased:
		return "已发布"
	case delivery.StatusClosed:
		return "已复盘关闭"
	default:
		return "未知"
	}
}

func gateLabel(gate delivery.Gate) string {
	switch gate {
	case delivery.GatePlanning:
		return "规划确认"
	case delivery.GateSolutionReviewed:
		return "方案评审"
	case delivery.GateDevelopmentCompleted:
		return "研发完成"
	case delivery.GateTestPassed:
		return "测试通过"
	case delivery.GateProductionValidated:
		return "生产验证"
	default:
		return "未知"
	}
}
