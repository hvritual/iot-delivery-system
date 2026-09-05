package obsidian

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hvritual/iot-delivery-system/backend-yunka/internal/delivery"
)

const generatedBy = "iot-delivery-system-yunka/v1"

const dailyDashboardFolder = "10-交付管理/00-每日驾驶舱"

type Exporter struct {
	root string
}

func NewExporter(root string) *Exporter {
	return &Exporter{root: filepath.Clean(root)}
}

// Export creates a one-way, generated Obsidian projection. SQLite remains the
// system of record; generated files are deliberately never read as task state.
func (exporter *Exporter) Export(ctx context.Context, items []delivery.WorkItem) error {
	if exporter == nil || strings.TrimSpace(exporter.root) == "" || exporter.root == "." {
		return fmt.Errorf("obsidian vault root is required")
	}
	items = append([]delivery.WorkItem(nil), items...)
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })

	if err := exporter.write("10-交付管理/README.md", readme()); err != nil {
		return err
	}
	day := localDay(time.Now())
	for _, board := range boards() {
		if err := exporter.write(dailyBoardPath(day, board), boardDashboard(day, board, items)); err != nil {
			return err
		}
	}
	if err := exporter.write(dailyDashboardPath(day), dailyDashboard(day, items)); err != nil {
		return err
	}
	if err := exporter.write("10-交付管理/00-交付总览.md", overview(items, day)); err != nil {
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
		"source_of_truth: \"iot-delivery-system-yunka\"\n" +
		"---\n\n" +
		"# 交付管理沉淀\n\n" +
		"本目录由 `iot-delivery-system` 的 Yunka 运行时单向生成。SQLite 是任务状态的唯一主数据源；请在交付系统中操作，再等待投影刷新。\n\n" +
		"- [[00-交付总览|查看交付总览]]\n"
}

func overview(items []delivery.WorkItem, day time.Time) string {
	var builder strings.Builder
	builder.WriteString(generatedHeader("交付总览", ""))
	builder.WriteString("## 每日驾驶舱\n\n")
	fmt.Fprintf(&builder, "- %s\n\n", vaultLink(dailyDashboardPath(day), "查看今日驾驶舱"))
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
	for _, board := range boards() {
		fmt.Fprintf(&builder, "- %s\n", board)
	}
	return builder.String()
}

func dailyDashboard(day time.Time, items []delivery.WorkItem) string {
	var builder strings.Builder
	builder.WriteString(generatedHeader(day.Format("2006-01-02")+" · 交付驾驶舱", ""))
	fmt.Fprintf(&builder, "本快照生成于 %s，展示五个交付板块的当前状态。\n\n", formatTime(day))
	builder.WriteString("## 五个板块\n\n")
	builder.WriteString("| 板块 | 事项 | 推进 | 受阻 | 验证 | 已发布 | 已关闭 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, board := range boards() {
		summary := summarizeBoard(board, items)
		fmt.Fprintf(&builder, "| %s | %d | %d | %d | %d | %d | %d |\n", vaultLink(dailyBoardPath(day, board), string(board)), summary.total, summary.active, summary.blocked, summary.verifying, summary.released, summary.closed)
	}

	blocked, overdue := attention(items, day)
	builder.WriteString("\n## 今日需关注\n\n")
	fmt.Fprintf(&builder, "- 受阻：%d 项\n- 逾期：%d 项\n", len(blocked), len(overdue))
	if len(blocked) == 0 && len(overdue) == 0 {
		builder.WriteString("\n当前没有受阻或逾期事项。\n")
		return builder.String()
	}
	builder.WriteString("\n| 类型 | 事项 | 原因或到期日 |\n| --- | --- | --- |\n")
	for _, item := range blocked {
		fmt.Fprintf(&builder, "| 受阻 | %s | %s |\n", itemLink(item), markdownCell(emptyAsUnknown(item.Blocker)))
	}
	for _, item := range overdue {
		fmt.Fprintf(&builder, "| 逾期 | %s | %s |\n", itemLink(item), markdownCell(item.DueDate))
	}
	return builder.String()
}

func boardDashboard(day time.Time, board delivery.Board, items []delivery.WorkItem) string {
	var builder strings.Builder
	summary := summarizeBoard(board, items)
	builder.WriteString(generatedHeader(day.Format("2006-01-02")+" · "+string(board), ""))
	fmt.Fprintf(&builder, "- 返回：%s\n\n", vaultLink(dailyDashboardPath(day), "每日驾驶舱"))
	builder.WriteString("## 板块状态\n\n")
	fmt.Fprintf(&builder, "| 事项 | 推进 | 受阻 | 验证 | 已发布 | 已关闭 |\n| ---: | ---: | ---: | ---: | ---: | ---: |\n| %d | %d | %d | %d | %d | %d |\n", summary.total, summary.active, summary.blocked, summary.verifying, summary.released, summary.closed)
	builder.WriteString("\n## 交付事项\n\n")
	builder.WriteString("| 事项 | 负责人 | 优先级 | 状态 | 当前关卡 | 到期日 |\n| --- | --- | --- | --- | --- | --- |\n")
	count := 0
	for _, item := range items {
		if item.Board != board {
			continue
		}
		count++
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | %s |\n", itemLink(item), markdownCell(item.Owner), markdownCell(string(item.Priority)), markdownCell(statusLabel(item.Status)), markdownCell(gateLabel(item.Gate)), markdownCell(item.DueDate))
	}
	if count == 0 {
		builder.WriteString("| — | — | — | 暂无事项 | — | — |\n")
	}
	return builder.String()
}

type boardSummary struct {
	total     int
	active    int
	blocked   int
	verifying int
	released  int
	closed    int
}

func summarizeBoard(board delivery.Board, items []delivery.WorkItem) boardSummary {
	var summary boardSummary
	for _, item := range items {
		if item.Board != board {
			continue
		}
		summary.total++
		switch item.Status {
		case delivery.StatusBlocked:
			summary.blocked++
		case delivery.StatusVerifying:
			summary.verifying++
		case delivery.StatusReleased:
			summary.released++
		case delivery.StatusClosed:
			summary.closed++
		default:
			summary.active++
		}
	}
	return summary
}

func attention(items []delivery.WorkItem, day time.Time) (blocked, overdue []delivery.WorkItem) {
	for _, item := range items {
		if item.Status == delivery.StatusBlocked {
			blocked = append(blocked, item)
		}
		if isOverdue(item, day) {
			overdue = append(overdue, item)
		}
	}
	return blocked, overdue
}

func isOverdue(item delivery.WorkItem, day time.Time) bool {
	if item.Status == delivery.StatusClosed || strings.TrimSpace(item.DueDate) == "" {
		return false
	}
	dueDate, err := time.Parse("2006-01-02", strings.TrimSpace(item.DueDate))
	if err != nil {
		return false
	}
	return dueDate.Before(time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()))
}

func dailyDashboardPath(day time.Time) string {
	return filepath.ToSlash(filepath.Join(dailyDashboardFolder, day.Format("2006-01-02")+"-交付驾驶舱.md"))
}

func dailyBoardPath(day time.Time, board delivery.Board) string {
	return filepath.ToSlash(filepath.Join(dailyDashboardFolder, day.Format("2006-01-02")+"-"+string(board)+".md"))
}

func itemLink(item delivery.WorkItem) string {
	return vaultLink(itemPath("01-规划", item, "规划"), item.ID+" · "+item.Title)
}

func vaultLink(path, label string) string {
	return "[[" + strings.TrimSuffix(path, ".md") + "|" + label + "]]"
}

func localDay(value time.Time) time.Time {
	return value.In(time.FixedZone("CST", 8*60*60))
}

func plan(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 规划", item.ID) + itemMetadata(item) +
		"## 规划\n\n" + emptyAsUnknown(item.Plan) + "\n\n" + dependencies(item) + "\n\n## 关联沉淀\n\n" + links(item)
}

func solution(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 方案", item.ID) + itemMetadata(item) +
		"## 方案\n\n" + emptyAsUnknown(item.Solution) + "\n\n## 关联沉淀\n\n" + links(item)
}

func validation(item delivery.WorkItem) string {
	var builder strings.Builder
	builder.WriteString(generatedHeader(item.Title+" · 发布与验证", item.ID))
	builder.WriteString(itemMetadata(item))
	builder.WriteString("## 关卡\n\n")
	fmt.Fprintf(&builder, "当前关卡：`%s`  \n状态：`%s`\n\n", gateLabel(item.Gate), statusLabel(item.Status))
	builder.WriteString("## 证据\n\n")
	if len(item.Evidence) == 0 {
		builder.WriteString("未知：尚未记录关卡证据。\n")
	} else {
		builder.WriteString("| 类型 | 证据 | 引用 | 记录时间 |\n| --- | --- | --- | --- |\n")
		for _, evidence := range item.Evidence {
			fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", markdownCell(evidence.Kind), markdownCell(evidence.Title), markdownCell(evidence.Reference), formatTime(evidence.RecordedAt))
		}
	}
	builder.WriteString("\n")
	builder.WriteString(iotScope(item))
	builder.WriteString("\n\n")
	builder.WriteString(traceLinks(item))
	builder.WriteString("\n## 关联沉淀\n\n")
	builder.WriteString(links(item))
	return builder.String()
}

func retrospective(item delivery.WorkItem) string {
	return generatedHeader(item.Title+" · 复盘", item.ID) + itemMetadata(item) +
		"## 复盘\n\n" + emptyAsUnknown(item.Retrospective) + "\n\n" + collaboration(item) + "\n\n## 关联沉淀\n\n" + links(item)
}

func decisionNote(item delivery.WorkItem, decision delivery.Decision) string {
	return generatedHeader(decision.Title, item.ID) +
		"## 决策上下文\n\n" + emptyAsUnknown(decision.Context) + "\n\n" +
		"## 决策结果\n\n" + emptyAsUnknown(decision.Outcome) + "\n\n" +
		"## 影响与后果\n\n" + emptyAsUnknown(decision.Consequences) + "\n\n" +
		"## 关联事项\n\n- [[10-交付管理/01-规划/" + safeName(item.ID) + "-规划|" + item.Title + "]]\n"
}

func generatedHeader(title, itemID string) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "generated_by: \"%s\"\n", generatedBy)
	builder.WriteString("source_of_truth: \"iot-delivery-system-yunka\"\n")
	if itemID != "" {
		fmt.Fprintf(&builder, "source_item: \"%s\"\n", safeName(itemID))
	}
	builder.WriteString("---\n\n# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	return builder.String()
}

func itemMetadata(item delivery.WorkItem) string {
	return fmt.Sprintf("| 属性 | 值 |\n| --- | --- |\n| 项目 | %s |\n| 父事项 | %s |\n| 类型 | %s |\n| 板块 | %s |\n| 负责人 | %s |\n| 优先级 | %s |\n| 状态 | %s |\n| 关卡 | %s |\n| 版本 | %s |\n| Sprint | %s |\n| 里程碑 | %s |\n| 开始日 | %s |\n| 到期日 | %s |\n| 估算 | %s |\n| 进度 | %s |\n| 最后更新 | %s |\n\n", markdownCell(item.ProjectID), markdownCell(item.ParentID), markdownCell(string(item.Kind)), markdownCell(string(item.Board)), markdownCell(item.Owner), markdownCell(string(item.Priority)), markdownCell(statusLabel(item.Status)), markdownCell(gateLabel(item.Gate)), markdownCell(item.ReleaseID), markdownCell(item.SprintID), markdownCell(item.MilestoneID), markdownCell(item.StartDate), markdownCell(item.DueDate), estimateLabel(item.EstimatePoints), progressLabel(item.ProgressPercent), formatTime(item.UpdatedAt))
}

func dependencies(item delivery.WorkItem) string {
	if len(item.Dependencies) == 0 {
		return "## 依赖关系\n\n未知：尚未在交付系统中记录依赖关系。"
	}
	var builder strings.Builder
	builder.WriteString("## 依赖关系\n\n| 关系 | 事项 |\n| --- | --- |\n")
	for _, dependency := range item.Dependencies {
		fmt.Fprintf(&builder, "| %s | %s |\n", markdownCell(dependencyLabel(dependency.Relation)), markdownCell(dependency.ItemID))
	}
	return builder.String()
}

func iotScope(item delivery.WorkItem) string {
	if len(item.IoTBindings) == 0 {
		return "## IoT 交付范围\n\n未知：尚未在交付系统中绑定设备、固件、客户、环境或灰度批次。"
	}
	var builder strings.Builder
	builder.WriteString("## IoT 交付范围\n\n| 类型 | 引用 | 标识 | 属性 |\n| --- | --- | --- | --- |\n")
	for _, binding := range item.IoTBindings {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", markdownCell(iotBindingLabel(binding.Kind)), markdownCell(binding.Reference), markdownCell(binding.Label), markdownCell(iotAttributes(binding.Attributes)))
	}
	return builder.String()
}

func iotAttributes(attributes map[string]string) string {
	if len(attributes) == 0 {
		return ""
	}
	encoded, err := json.Marshal(attributes)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func traceLinks(item delivery.WorkItem) string {
	if len(item.TraceLinks) == 0 {
		return "## 研发交付关联\n\n未知：尚未在交付系统中关联 PR、构建、测试、缺陷或发布证据。"
	}
	var builder strings.Builder
	builder.WriteString("## 研发交付关联\n\n| 类型 | 引用 | 标题 | 状态 | 链接 | 记录时间 |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, link := range item.TraceLinks {
		fmt.Fprintf(&builder, "| %s | %s | %s | %s | %s | %s |\n", markdownCell(traceKindLabel(link.Kind)), markdownCell(link.Reference), markdownCell(link.Title), markdownCell(link.Status), markdownCell(link.URL), formatTime(link.RecordedAt))
	}
	return builder.String()
}

func collaboration(item delivery.WorkItem) string {
	if len(item.Comments) == 0 && len(item.Activities) == 0 {
		return "## 协作与活动\n\n未知：尚未在交付系统中记录评论或活动审计。"
	}
	var builder strings.Builder
	builder.WriteString("## 协作与活动\n\n")
	if len(item.Comments) > 0 {
		builder.WriteString("### 评论\n\n| 作者 | 时间 | 内容 |\n| --- | --- | --- |\n")
		for _, comment := range item.Comments {
			fmt.Fprintf(&builder, "| %s | %s | %s |\n", markdownCell(comment.Author), formatTime(comment.CreatedAt), markdownCell(comment.Body))
		}
	}
	if len(item.Activities) > 0 {
		if len(item.Comments) > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("### 活动审计\n\n| 类型 | 操作者 | 时间 | 摘要 |\n| --- | --- | --- | --- |\n")
		for _, activity := range item.Activities {
			fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", markdownCell(activity.Type), markdownCell(activity.Actor), formatTime(activity.OccurredAt), markdownCell(activity.Summary))
		}
	}
	return strings.TrimSpace(builder.String())
}

func dependencyLabel(relation delivery.DependencyRelation) string {
	switch relation {
	case delivery.DependencyDependsOn:
		return "依赖"
	case delivery.DependencyBlocks:
		return "阻塞"
	case delivery.DependencyRelated:
		return "关联"
	default:
		return string(relation)
	}
}

func iotBindingLabel(kind delivery.IoTBindingKind) string {
	switch kind {
	case delivery.IoTBindingDevice:
		return "设备"
	case delivery.IoTBindingFirmware:
		return "固件"
	case delivery.IoTBindingCustomer:
		return "客户"
	case delivery.IoTBindingEnvironment:
		return "环境"
	case delivery.IoTBindingRolloutBatch:
		return "灰度批次"
	default:
		return string(kind)
	}
}

func traceKindLabel(kind delivery.TraceKind) string {
	switch kind {
	case delivery.TracePullRequest:
		return "PR"
	case delivery.TraceBuild:
		return "构建"
	case delivery.TraceTest:
		return "测试"
	case delivery.TraceDefect:
		return "缺陷"
	case delivery.TraceRelease:
		return "发布"
	default:
		return string(kind)
	}
}

func estimateLabel(points float64) string {
	if points <= 0 {
		return "—"
	}
	return fmt.Sprintf("%g 点", points)
}

func progressLabel(progress int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	return fmt.Sprintf("%d%%", progress)
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

func boards() []delivery.Board {
	return []delivery.Board{
		delivery.BoardDeviceQuality,
		delivery.BoardProductPlatform,
		delivery.BoardResearchDelivery,
		delivery.BoardOperations,
		delivery.BoardCustomerValue,
	}
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return -1
		}
	}, value)
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
