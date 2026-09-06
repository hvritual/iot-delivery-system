"use client";
import { useRef, useState, type CSSProperties, type FormEvent } from "react";
import {
  ArrowRight,
  Calendar,
  Flag,
  Folder,
  GitBranch,
  Plus,
  X,
} from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Action,
  Chip,
  Failure,
  FieldGroup,
  FormField,
  Heading,
  Metric,
  NoData,
  Notice,
  Person,
  Section,
  Status,
} from "./ui";
import {
  BOARD_NAMES,
  EMPTY_PLANNING,
  displayDate,
  kindText,
  type CreateCommand,
  type Planning,
  type Progress,
  type Project,
  type Schedule,
  type WorkItem,
} from "./model";

type View =
  | "projects"
  | "health"
  | "releases"
  | "sprints"
  | "milestones"
  | "hierarchy";
type Create = "project" | "release" | "sprint" | "milestone";
const views: [View, string][] = [
  ["projects", "项目与排期"],
  ["health", "交付健康"],
  ["releases", "发布版本"],
  ["sprints", "Sprint"],
  ["milestones", "里程碑"],
  ["hierarchy", "事项层级"],
];
type Props = {
  activeProjectId: string;
  projects: Project[];
  planning?: Planning;
  progress: Progress | null;
  schedule: Schedule | null;
  items?: WorkItem[];
  onSelectProject: (id: string) => void;
  onOpenItem?: (id: string) => void;
  onCreateProject: CreateCommand;
  onCreateRelease: CreateCommand;
  onCreateSprint: CreateCommand;
  onCreateMilestone: CreateCommand;
};
export function ProjectWorkspace({
  activeProjectId,
  projects,
  planning = EMPTY_PLANNING,
  progress,
  schedule,
  items = [],
  onSelectProject,
  onOpenItem,
  ...commands
}: Props) {
  const [view, setView] = useState<View>("projects");
  const [creating, setCreating] = useState<Create | null>(null);
  const project = projects.find((p) => p.id === activeProjectId);
  const scoped = items.filter((i) => i.projectId === activeProjectId);
  function select(id: string) {
    onSelectProject(id);
    setView("health");
  }
  return (
    <div className="project-surface">
      <nav className="tabs" aria-label="项目工作区">
        {views.map(([key, title]) => (
          <button
            type="button"
            key={key}
            onClick={() => setView(key)}
            className={view === key ? "active" : ""}
            aria-current={view === key ? "page" : undefined}
          >
            {title}
          </button>
        ))}
      </nav>
      <div className="page">
        {view === "projects" ? (
          <>
            <Heading
              title="项目与排期"
              description="项目承载版本、Sprint、里程碑与交付事项。"
              actions={
                <Action
                  primary
                  icon={Plus}
                  onClick={() => setCreating("project")}
                >
                  新建项目
                </Action>
              }
            />
            {projects.length ? (
              <table className="data-table projects-table">
                <thead>
                  <tr>
                    <th>项目</th>
                    <th>所属板块</th>
                    <th>负责人</th>
                    <th>关联事项</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {projects.map((p) => (
                    <tr key={p.id}>
                      <td>
                        <button
                          className="row-open"
                          onClick={() => select(p.id)}
                        >
                          <Folder className="icon" />
                          <strong>{p.name}</strong>
                        </button>
                        <small className="mono">{p.id}</small>
                      </td>
                      <td>{p.board}</td>
                      <td>
                        <Person name={p.owner} />
                      </td>
                      <td>
                        {items.filter((i) => i.projectId === p.id).length} 项
                      </td>
                      <td>
                        <Action
                          icon={ArrowRight}
                          aria-label={`打开项目 ${p.name}`}
                          onClick={() => select(p.id)}
                        >
                          进入项目
                        </Action>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <NoData
                title="尚无项目"
                icon={Folder}
                action={
                  <Action primary onClick={() => setCreating("project")}>
                    创建项目
                  </Action>
                }
              >
                先定义交付目标，再关联版本与任务。
              </NoData>
            )}
          </>
        ) : (
          <>
            <Heading
              title={project?.name || "选择一个项目"}
              description={
                project
                  ? `${project.id} · ${project.board} · 负责人 ${project.owner}`
                  : "项目未选择时不显示伪造的汇总数据。"
              }
              actions={
                <FormField
                  label="当前项目"
                  className="inline-project-select"
                  options={[
                    { value: "", label: "选择项目" },
                    ...projects.map((p) => ({ value: p.id, label: p.name })),
                  ]}
                  value={activeProjectId}
                  onChange={(e) => onSelectProject(e.target.value)}
                />
              }
            />
            {!project ? (
              <NoData title="选择项目后查看交付数据" icon={Folder}>
                版本、Sprint、里程碑和健康计算均限于当前项目。
              </NoData>
            ) : view === "health" ? (
              <>
                <div className="metric-band">
                  <Metric
                    label="按估算加权进度"
                    value={
                      progress
                        ? `${Math.round(progress.progressPercent)}%`
                        : "—"
                    }
                    hint={
                      progress
                        ? `${progress.completedItems} / ${progress.totalItems} 项完成`
                        : "正在读取项目进度"
                    }
                  />
                  <Metric
                    label="已排期事项"
                    value={
                      schedule
                        ? `${schedule.scheduledItems ?? 0} / ${schedule.totalItems ?? 0}`
                        : "—"
                    }
                    hint={
                      schedule?.asOfDate
                        ? `截至 ${schedule.asOfDate}`
                        : "正在读取排期健康"
                    }
                  />
                  <Metric
                    label="逾期 / 受阻"
                    value={
                      schedule
                        ? `${schedule.overdueItems ?? 0} / ${schedule.blockedItems ?? 0}`
                        : "—"
                    }
                    hint="需要确认阻塞与目标日期"
                  />
                </div>
                <ProjectScheduleHealth
                  schedule={schedule}
                  onOpenItem={onOpenItem}
                />
                <Section title="进度说明">
                  <p>
                    项目进度按估算权重汇总；进度百分比不替代测试或生产验证关卡证据。
                  </p>
                </Section>
              </>
            ) : view === "hierarchy" ? (
              <>
                <div className="section-title">
                  <h3>Epic / 任务 / 子任务 / 缺陷</h3>
                  <p>层级与依赖分开表达；同一事项仅展示一次。</p>
                </div>
                <Hierarchy items={scoped} onOpenItem={onOpenItem} />
              </>
            ) : (
              <PlanningTable
                records={planning[
                  view as "releases" | "sprints" | "milestones"
                ].filter((p) => p.projectId === activeProjectId)}
                view={view as "releases" | "sprints" | "milestones"}
                items={scoped}
                onOpenItem={onOpenItem}
                onCreate={() =>
                  setCreating(
                    view === "releases"
                      ? "release"
                      : view === "sprints"
                        ? "sprint"
                        : "milestone",
                  )
                }
              />
            )}
          </>
        )}
      </div>
      {creating ? (
        <PlanningDialog
          kind={creating}
          project={project}
          onClose={() => setCreating(null)}
          onCreate={
            creating === "project"
              ? commands.onCreateProject
              : creating === "release"
                ? commands.onCreateRelease
                : creating === "sprint"
                  ? commands.onCreateSprint
                  : commands.onCreateMilestone
          }
        />
      ) : null}
    </div>
  );
}
function PlanningTable({
  records,
  view,
  items,
  onOpenItem,
  onCreate,
}: {
  records: Planning["releases"];
  view: "releases" | "sprints" | "milestones";
  items: WorkItem[];
  onOpenItem?: Props["onOpenItem"];
  onCreate: () => void;
}) {
  const label =
    view === "releases" ? "发布版本" : view === "sprints" ? "Sprint" : "里程碑";
  const field =
    view === "releases"
      ? "releaseId"
      : view === "sprints"
        ? "sprintId"
        : "milestoneId";
  const [expanded, setExpanded] = useState<string | null>(null);
  return (
    <>
      <div className="section-title with-action">
        <h3>{label}</h3>
        <Action primary icon={Plus} onClick={onCreate}>
          新增{label}
        </Action>
      </div>
      {records.length ? (
        <table className="data-table">
          <thead>
            <tr>
              <th>{label}</th>
              <th>{view === "sprints" ? "周期" : "目标日期"}</th>
              <th>关联事项</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={r.id}>
                <td>
                  <strong>
                    {r.version ? `${r.version} · ` : ""}
                    {r.name}
                  </strong>
                  <small className="mono">{r.id}</small>
                </td>
                <td>
                  {view === "sprints"
                    ? `${displayDate(r.startDate)} — ${displayDate(r.endDate)}`
                    : displayDate(r.targetDate)}
                </td>
                <td>{items.filter((i) => i[field] === r.id).length} 项</td>
                <td>
                  <Action
                    onClick={() => setExpanded(expanded === r.id ? null : r.id)}
                  >
                    查看关联
                  </Action>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <NoData title={`尚无${label}`} icon={Calendar}>
          通过右上角创建，之后可在事项排期中关联。
        </NoData>
      )}
      {expanded ? (
        <Section
          title={`${records.find((r) => r.id === expanded)?.name || label} · 关联事项`}
        >
          {items.filter((i) => i[field] === expanded).length ? (
            items
              .filter((i) => i[field] === expanded)
              .map((i) => (
                <div className="linked-item" key={i.id}>
                  <button onClick={() => onOpenItem?.(i.id)}>{i.title}</button>
                  <Status value={i.status} />
                </div>
              ))
          ) : (
            <p className="caption">当前尚无关联事项。</p>
          )}
        </Section>
      ) : null}
    </>
  );
}
export function ProjectScheduleHealth({
  schedule,
  onOpenItem,
}: {
  schedule: Schedule | null;
  onOpenItem?: (id: string) => void;
}) {
  if (!schedule)
    return (
      <NoData title="排期健康尚未返回" icon={Calendar}>
        选择项目后查看排期、依赖与成员剩余估算。
      </NoData>
    );
  return (
    <section className="health-columns" aria-label="项目交付健康">
      <div>
        <h3>排期风险</h3>
        {schedule.risks?.length ? (
          <table className="data-table">
            <thead>
              <tr>
                <th>风险事项</th>
                <th>原因</th>
              </tr>
            </thead>
            <tbody>
              {schedule.risks.map((r, index) => (
                <tr key={`${r.itemId}-${r.reason}-${index}`}>
                  <td>
                    <button
                      className="row-open"
                      onClick={() => onOpenItem?.(r.itemId)}
                    >
                      {r.title}
                    </button>
                    <small className="mono">
                      {r.itemId} · {displayDate(r.dueDate)}
                    </small>
                  </td>
                  <td>
                    <Chip tone="warning">{r.reason}</Chip>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="caption">本次计算未返回排期风险。</p>
        )}
      </div>
      <div>
        <h3>负责人剩余估算</h3>
        {schedule.capacity?.length ? (
          schedule.capacity.map((c) => (
            <div className="capacity-row" key={c.owner}>
              <div className="with-action">
                <Person name={c.owner} />
                <strong>
                  {Number(c.remainingEstimatePoints).toFixed(1)} pts
                </strong>
              </div>
              <p className="caption">受阻事项 {c.blockedItems ?? 0}</p>
            </div>
          ))
        ) : (
          <p className="caption">暂无成员估算数据。</p>
        )}
        <p className="caption">
          剩余估算 = 估算 × 未完成比例。未配置容量阈值，不推断“超负荷”。
        </p>
        <dl className="health-signals">
          <div>
            <dt>依赖阻塞</dt>
            <dd>{schedule.dependencyBlockedItems ?? 0}</dd>
          </div>
          <div>
            <dt>未排期事项</dt>
            <dd>{schedule.unscheduledItems ?? 0}</dd>
          </div>
        </dl>
      </div>
    </section>
  );
}
function Hierarchy({
  items,
  onOpenItem,
}: {
  items: WorkItem[];
  onOpenItem?: Props["onOpenItem"];
}) {
  const byId = new Map(items.map((i) => [i.id, i]));
  const depth = (item: WorkItem) => {
    let n = 0,
      p = item.parentId;
    const seen = new Set([item.id]);
    while (p && byId.has(p) && !seen.has(p)) {
      seen.add(p);
      n++;
      p = byId.get(p)?.parentId;
    }
    return Math.min(n, 8);
  };
  // Traversal is bounded and cycle-safe even when a stale projection is malformed.
  const ordered: WorkItem[] = [];
  const seen = new Set<string>();
  const visit = (item: WorkItem) => {
    if (seen.has(item.id)) return;
    seen.add(item.id);
    ordered.push(item);
    items.filter((i) => i.parentId === item.id).forEach(visit);
  };
  items.filter((i) => !i.parentId || !byId.has(i.parentId)).forEach(visit);
  items.forEach(visit);
  return items.length ? (
    <table className="data-table">
      <thead>
        <tr>
          <th>事项 / 层级</th>
          <th>类型</th>
          <th>状态</th>
          <th>负责人</th>
        </tr>
      </thead>
      <tbody>
        {ordered.map((i) => (
          <tr key={i.id}>
            <td style={{ paddingLeft: 12 + depth(i) * 20 }}>
              <button className="row-open" onClick={() => onOpenItem?.(i.id)}>
                <GitBranch className="icon" />
                {i.title}
              </button>
              <small className="mono">{i.id}</small>
            </td>
            <td>
              <Chip>{kindText(i.kind)}</Chip>
            </td>
            <td>
              <Status value={i.status} />
            </td>
            <td>
              <Person name={i.owner} />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  ) : (
    <NoData title="项目还没有交付事项" icon={GitBranch}>
      新建事项时可关联项目与父事项。
    </NoData>
  );
}
function PlanningDialog({
  kind,
  project,
  onCreate,
  onClose,
}: {
  kind: Create;
  project?: Project;
  onCreate: CreateCommand;
  onClose: () => void;
}) {
  const [form, setForm] = useState({
    name: "",
    owner: "",
    board: "研发交付效能",
    version: "",
    targetDate: "",
    startDate: "",
    endDate: "",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const lock = useRef(false);
  const label =
    kind === "project"
      ? "项目"
      : kind === "release"
        ? "版本"
        : kind === "sprint"
          ? "Sprint"
          : "里程碑";
  const update = (p: Partial<typeof form>) => setForm((f) => ({ ...f, ...p }));
  const close = () => {
    if (!busy && (!form.name || window.confirm("确定放弃本次创建内容吗？")))
      onClose();
  };
  async function submit(e: FormEvent) {
    e.preventDefault();
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError("");
    try {
      if (kind !== "project" && !project) throw new Error("请先选择项目。");
      if (kind === "sprint" && form.endDate < form.startDate)
        throw new Error("结束日期不能早于开始日期。");
      const value =
        kind === "project"
          ? { name: form.name, board: form.board, owner: form.owner }
          : {
              name: form.name,
              projectId: project!.id,
              ...(kind === "release"
                ? { version: form.version, targetDate: form.targetDate }
                : kind === "sprint"
                  ? { startDate: form.startDate, endDate: form.endDate }
                  : { targetDate: form.targetDate }),
            };
      const result = await onCreate(value);
      if (result) onClose();
      else setError("创建未完成，输入已保留。");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "创建未完成。");
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(v) => {
        if (!v) close();
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="modal planning-modal"
        style={{ "--modal-width": "560px" } as CSSProperties}
      >
        <header className="modal-head">
          <div>
            <DialogTitle>新建{label}</DialogTitle>
            <DialogDescription>
              {project
                ? project.name
                : "按五个交付板块组织项目，之后添加版本和任务。"}
            </DialogDescription>
          </div>
          <button
            className="icon-button"
            aria-label="关闭"
            disabled={busy}
            onClick={close}
          >
            <X className="icon" />
          </button>
        </header>
        <form
          className="modal-form"
          onSubmit={(e) => void submit(e)}
          aria-label={`新建${label}`}
        >
          <div className="modal-body">
            <Failure error={error} />
            <FieldGroup>
              <FormField
                label={`${label}名称`}
                required
                value={form.name}
                onChange={(e) => update({ name: e.target.value })}
              />
              {kind === "project" ? (
                <>
                  <FormField
                    label="项目负责人"
                    required
                    value={form.owner}
                    onChange={(e) => update({ owner: e.target.value })}
                  />
                  <FormField
                    label="所属板块"
                    options={BOARD_NAMES.map((b) => ({ value: b, label: b }))}
                    value={form.board}
                    onChange={(e) => update({ board: e.target.value })}
                  />
                </>
              ) : kind === "sprint" ? (
                <>
                  <FormField
                    label="开始日期"
                    type="date"
                    required
                    value={form.startDate}
                    onChange={(e) => update({ startDate: e.target.value })}
                  />
                  <FormField
                    label="结束日期"
                    type="date"
                    required
                    value={form.endDate}
                    onChange={(e) => update({ endDate: e.target.value })}
                  />
                </>
              ) : (
                <>
                  {kind === "release" ? (
                    <FormField
                      label="版本号"
                      required
                      value={form.version}
                      onChange={(e) => update({ version: e.target.value })}
                    />
                  ) : null}
                  <FormField
                    label="目标日期"
                    required={kind === "milestone"}
                    type="date"
                    value={form.targetDate}
                    onChange={(e) => update({ targetDate: e.target.value })}
                  />
                </>
              )}
            </FieldGroup>
          </div>
          <footer className="modal-foot">
            <Action disabled={busy} onClick={close}>
              取消
            </Action>
            <Action primary type="submit" busy={busy} icon={Plus}>
              创建{label}
            </Action>
          </footer>
        </form>
      </DialogContent>
    </Dialog>
  );
}
