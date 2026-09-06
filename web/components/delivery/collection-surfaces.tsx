"use client";
import { useRef, useState, type FormEvent } from "react";
import {
  Activity,
  Archive,
  ArrowRight,
  Box,
  CalendarDays,
  CheckCircle,
  ChevronRight,
  CircuitBoard,
  Inbox,
  ListChecks,
  Search,
  ShieldCheck,
  TriangleAlert,
  Users,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { todayInChina } from "@/src/lib/presentation.mjs";
import {
  Action,
  Chip,
  FormField,
  FieldGroup,
  Heading,
  Metric,
  NoData,
  Person,
  Status,
} from "./ui";
import {
  BOARD_NAMES,
  EMPTY_FILTER,
  KIND_LABELS,
  STATUS_LABELS,
  displayDate,
  gateText,
  kindText,
  type Board,
  type WorkItem,
  type ItemFilter,
  type Project,
  type SavedView,
  type MemberWeek,
  type Notification,
  type CreateCommand,
} from "./model";
const boardIcons = [CircuitBoard, Box, Activity, ShieldCheck, Users];
export function BoardGrid({
  boards,
  items = [],
  onSelectBoard,
}: {
  boards: Board[];
  items?: WorkItem[];
  activeBoard?: string | null;
  onSelectBoard: (board: string | null) => void;
}) {
  return (
    <section className="flat-panel" aria-label="五个交付板块概览">
      <div className="panel-header">
        <h3>五个交付板块</h3>
        <span className="caption">点击板块，查看所属交付事项</span>
      </div>
      <table className="board-table">
        <thead>
          <tr>
            {[
              "交付板块",
              "总事项",
              "待推进",
              "进行中",
              "受阻",
              "验证中",
              "已发布 / 关闭",
              "",
            ].map((h, i) => (
              <th key={i}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {boards.map((board, i) => {
            const Icon = boardIcons[i] ?? Activity;
            const values = items.filter((item) => item.board === board.board);
            const count = (state: string) =>
              values.filter((item) => item.status === state).length;
            return (
              <tr key={board.board} onClick={() => onSelectBoard(board.board)}>
                <td>
                  <button
                    className="table-title row-open"
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onSelectBoard(board.board);
                    }}
                  >
                    <Icon className="icon" />
                    {board.board}
                  </button>
                </td>
                <td>{board.total ?? values.length}</td>
                <td>{count("planned")}</td>
                <td>{count("in_progress")}</td>
                <td className={count("blocked") ? "danger-text" : ""}>
                  {count("blocked")}
                </td>
                <td>{count("verifying")}</td>
                <td>{count("released") + count("closed")}</td>
                <td>
                  <ChevronRight className="icon" aria-hidden="true" />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </section>
  );
}
export function StatusLegend() {
  return (
    <div className="status-legend" aria-label="状态说明">
      {Object.keys(STATUS_LABELS).map((value) => (
        <Status key={value} value={value} />
      ))}
    </div>
  );
}
export function DailyFocus({
  focus,
  onSelect,
  total = 0,
}: {
  focus: { blocked: number; overdue: number; verifying: number };
  total?: number;
  activeFilter?: string;
  onSelect: (value: string) => void;
}) {
  return (
    <div className="metric-band" aria-label="每日交付关注项">
      <Metric
        label="受阻事项"
        value={focus.blocked}
        hint="需要澄清阻塞与后续动作"
        tone="danger-text"
        onClick={() => onSelect("blocked")}
      />
      <Metric
        label="逾期事项"
        value={focus.overdue}
        hint="按目标日期计算"
        tone="warning-text"
        onClick={() => onSelect("overdue")}
      />
      <Metric
        label="验证中"
        value={focus.verifying}
        hint="待提交下一关证据"
        onClick={() => onSelect("verification")}
      />
      <Metric
        label="全部事项"
        value={total}
        hint="5 个交付板块"
        onClick={() => onSelect("all")}
      />
    </div>
  );
}
export function DeliveryTable({
  items,
  selectedId,
  onSelectItem,
  onOpenItem,
  compact = false,
}: {
  items: WorkItem[];
  selectedId?: string;
  compact?: boolean;
  onSelectItem: (id: string) => void;
  onOpenItem?: (id: string) => void;
}) {
  if (!items.length)
    return (
      <NoData title="没有匹配的交付事项" icon={Search}>
        调整筛选条件，或创建新的交付事项。
      </NoData>
    );
  return (
    <div className="table-shell">
      <table className={cn("data-table", compact && "compact")}>
        <thead>
          <tr>
            {(compact
              ? ["交付事项", "状态", "关卡"]
              : ["交付事项", "状态", "当前关卡", "负责人", "目标日期"]
            ).map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const Icon =
              item.kind === "epic"
                ? Box
                : item.kind === "defect"
                  ? TriangleAlert
                  : CheckCircle;
            return (
              <tr
                key={item.id}
                className={selectedId === item.id ? "selected" : undefined}
                onClick={() => onSelectItem(item.id)}
                onDoubleClick={() => onOpenItem?.(item.id)}
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.target !== event.currentTarget) return;
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onSelectItem(item.id);
                  }
                }}
                aria-selected={selectedId === item.id}
              >
                <td className="item-cell">
                  <button
                    type="button"
                    className="table-title row-open"
                    onClick={(event) => {
                      event.stopPropagation();
                      onSelectItem(item.id);
                    }}
                  >
                    <Icon className="icon" />
                    {item.title}
                  </button>
                  <div className="table-meta">
                    <span className="mono">{item.id}</span>
                    <span>{kindText(item.kind)}</span>
                    <span>{compact ? item.owner : item.board}</span>
                    {item.isSample ? <Chip>示例</Chip> : null}
                  </div>
                </td>
                <td>
                  <Status value={item.status} />
                </td>
                <td className="muted">{gateText(item.gate)}</td>
                {compact ? null : (
                  <>
                    <td>
                      <Person name={item.owner} />
                    </td>
                    <td
                      className={cn(
                        "mono",
                        item.dueDate &&
                          item.dueDate < todayInChina() &&
                          item.status !== "closed"
                          ? "danger-text"
                          : "muted",
                      )}
                    >
                      {displayDate(item.dueDate)}
                    </td>
                  </>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
export function TaskOperationsPanel({
  filter,
  onFilterChange,
  savedViews = [],
  onSaveView,
  onApplyView,
  onLoadMemberWeek,
  week,
  projects = [],
  mode = "filters",
  onOpenItem,
}: {
  filter: ItemFilter;
  onFilterChange: (f: ItemFilter) => void;
  savedViews?: SavedView[];
  onSaveView: CreateCommand;
  onApplyView: (view: SavedView) => void;
  onLoadMemberWeek: (member: string, start: string) => Promise<unknown>;
  week: MemberWeek | null;
  projects?: Project[];
  mode?: "filters" | "weekly";
  onOpenItem?: (id: string) => void;
  notifications?: Notification[];
}) {
  const [name, setName] = useState("");
  const [member, setMember] = useState("");
  const [start, setStart] = useState("");
  const [busy, setBusy] = useState(false);
  const submitLock = useRef(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitLock.current) return;
    submitLock.current = true;
    setBusy(true);
    try {
      if (mode === "weekly") await onLoadMemberWeek(member.trim(), start);
      else if (await onSaveView({ name: name.trim(), filter })) setName("");
    } finally {
      submitLock.current = false;
      setBusy(false);
    }
  }
  if (mode === "weekly")
    return (
      <div className="page">
        <Heading
          title="成员周视图"
          description="按成员查看排期内及未排期的开放事项；数据以服务端周视图为准。"
        />
        <form className="week-toolbar" onSubmit={submit}>
          <FormField
            label="成员名称"
            required
            value={member}
            onChange={(e) => setMember(e.target.value)}
          />
          <FormField
            label="周起始日期"
            type="date"
            value={start}
            onChange={(e) => setStart(e.target.value)}
          />
          <Action type="submit" busy={busy} icon={CalendarDays}>
            查看
          </Action>
        </form>
        {week ? (
          <>
            <div className="week-summary">
              <Person name={week.member} />
              <span className="caption">
                {week.weekStart} 至 {week.weekEnd}
              </span>
              <Chip>{week.items?.length ?? 0} 项</Chip>
            </div>
            <DeliveryTable
              items={week.items ?? []}
              onSelectItem={(id) => onOpenItem?.(id)}
            />
          </>
        ) : (
          <NoData title="选择成员，查看本周工作" icon={CalendarDays}>
            输入成员与周起始日期后查询；不会把未查询的数据显示为 0 项。
          </NoData>
        )}
      </div>
    );
  const options = (map: Record<string, string>, all: string) => [
    { value: "", label: all },
    ...Object.entries(map).map(([value, label]) => ({ value, label })),
  ];
  return (
    <section className="filter-workspace" aria-label="筛选与保存视图">
      <Heading
        title="筛选与保存视图"
        description="保存当前查询条件，下次直接复用。"
      />
      <FieldGroup className="form-grid">
        <FormField
          label="搜索事项"
          placeholder="标题、设备、PR、构建或测试证据"
          value={filter.query}
          onChange={(e) => onFilterChange({ ...filter, query: e.target.value })}
        />
        <FormField
          label="所属项目"
          options={[
            { value: "", label: "全部项目" },
            ...projects.map((p) => ({ value: p.id, label: p.name })),
          ]}
          value={filter.projectId}
          onChange={(e) =>
            onFilterChange({ ...filter, projectId: e.target.value })
          }
        />
        <FormField
          label="负责人"
          value={filter.owner}
          onChange={(e) => onFilterChange({ ...filter, owner: e.target.value })}
        />
        <FormField
          label="状态"
          options={options(STATUS_LABELS, "全部状态")}
          value={filter.status}
          onChange={(e) =>
            onFilterChange({ ...filter, status: e.target.value })
          }
        />
        <FormField
          label="类型"
          options={options(KIND_LABELS, "全部类型")}
          value={filter.kind}
          onChange={(e) => onFilterChange({ ...filter, kind: e.target.value })}
        />
      </FieldGroup>
      <div className="form-actions">
        <Action onClick={() => onFilterChange({ ...EMPTY_FILTER })}>
          清空筛选
        </Action>
      </div>
      <section className="section">
        <h3>保存为个人视图</h3>
        <form className="save-view-form" onSubmit={submit}>
          <FormField
            label="视图名称"
            required
            value={name}
            placeholder="例如：本周连接稳定性验收"
            onChange={(e) => setName(e.target.value)}
          />
          <Action type="submit" busy={busy} icon={Archive}>
            保存视图
          </Action>
        </form>
        <div className="saved-view-list">
          {savedViews.map((v) => (
            <Action key={v.id} onClick={() => onApplyView(v)}>
              {v.name}
            </Action>
          ))}
        </div>
        {!savedViews.length ? <p className="caption">尚未保存视图。</p> : null}
      </section>
    </section>
  );
}
export function NotificationsView({
  notifications,
  onOpenItem,
}: {
  notifications: Notification[];
  onOpenItem: (id: string) => void;
}) {
  return (
    <div className="page">
      <Heading
        title="通知收件箱"
        description="本地可靠投递记录；此页面不提供已读、删除或重新投递接口。"
        actions={<Chip>{notifications.length} 条最近投递</Chip>}
      />
      {notifications.length ? (
        <div className="notification-list">
          {notifications.map((n) => (
            <article
              className="notification-item"
              key={`${n.deliveryId}-${n.channel}`}
            >
              <Inbox className="icon" />
              <div>
                <h3>{n.title || n.eventType || "交付事件"}</h3>
                <p className="caption">{n.subject || "未关联事项"}</p>
                {n.body ? <p className="notification-body">{n.body}</p> : null}
                <div className="table-meta">
                  <span className="mono">{n.deliveryId}</span>
                  <span>
                    {n.deliveredAt || n.createdAt || "未返回投递时间"}
                  </span>
                </div>
              </div>
              <Chip>{n.channel || "local-inbox"}</Chip>
              {n.subject ? (
                <Action
                  icon={ArrowRight}
                  aria-label={`查看关联事项 ${n.subject}`}
                  onClick={() => onOpenItem(n.subject!)}
                >
                  查看事项
                </Action>
              ) : null}
            </article>
          ))}
        </div>
      ) : (
        <NoData title="尚无投递记录" icon={Inbox}>
          任务状态、截止提醒与发布事件会先写入本地收件箱。
        </NoData>
      )}
    </div>
  );
}
