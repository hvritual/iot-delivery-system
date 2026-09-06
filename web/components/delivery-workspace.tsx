"use client";
import {
  type ComponentType,
  type ReactNode,
  type CSSProperties,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ArrowRight,
  Archive,
  Filter,
  LayoutDashboard,
  ListChecks,
  PanelRight,
  Plus,
  RefreshCw,
  Search,
  TriangleAlert,
} from "lucide-react";
import { SidebarProvider } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { Input } from "@/components/ui/input";
import {
  Action,
  Chip,
  Failure,
  GateTrack,
  Heading,
  NoData,
  Notice,
  Status,
} from "./delivery/ui";
import { NotificationsView } from "./delivery/collection-surfaces";
import {
  EMPTY_FILTER,
  EMPTY_PLANNING,
  type Board,
  type WorkItem,
  type Project,
  type Planning,
  type ItemFilter,
  type SavedView,
  type Notification,
  type MemberWeek,
  type Progress,
  type Schedule,
  type RequestFailure,
} from "./delivery/model";
import {
  addComment,
  advanceGate,
  closeItem,
  createItem,
  createMilestone,
  createProject,
  createRelease,
  createSprint,
  fetchDashboard,
  fetchMemberWeek,
  fetchMilestones,
  fetchNotifications,
  fetchProjectProgress,
  fetchProjectSchedule,
  fetchProjects,
  fetchReleases,
  fetchSavedViews,
  fetchSprints,
  findSimilar,
  saveView,
  updateItemContext,
  updateWorkItem,
} from "@/src/api.js";
import { BoardGrid, StatusLegend } from "@/src/components/BoardGrid.jsx";
import { CreateItemDialog } from "@/src/components/CreateItemDialog.jsx";
import { DailyFocus } from "@/src/components/DailyFocus.jsx";
import { DeliveryTable } from "@/src/components/DeliveryTable.jsx";
import { ItemPanel } from "@/src/components/ItemPanel.jsx";
import { ProjectScheduleHealth } from "@/src/components/ProjectScheduleHealth.jsx";
import { ProjectWorkspace } from "@/src/components/ProjectWorkspace.jsx";
import { TaskOperationsPanel } from "@/src/components/TaskOperationsPanel.jsx";
import {
  dailyFocus,
  filterItems,
  normalizeDashboard,
} from "@/src/lib/presentation.mjs";
import { loadR2Workspace } from "@/src/lib/r2-capability.mjs";
import { filterDeliveryItems } from "@/src/lib/r2-presentation.mjs";
import {
  DeliverySidebar,
  type DeliverySurface,
} from "@/components/delivery-sidebar";

type Dashboard = {
  boards: Board[];
  items: WorkItem[];
  generatedAt?: string | null;
};
type WorkspaceProps = {
  sessionName?: string;
  sessionDescription?: string;
  onAccount?: () => void;
  onLogout?: () => void;
  accountContent?: ReactNode;
  accountOpen?: boolean;
  onLeaveAccount?: () => boolean;
  onSessionExpired?: () => void;
  sessionBusy?: boolean;
  notice?: ReactNode;
};
const surfaceCopy: Record<DeliverySurface, { title: string }> = {
  cockpit: { title: "交付驾驶舱" },
  items: { title: "交付事项" },
  projects: { title: "项目与排期" },
  weekly: { title: "成员周视图" },
  notifications: { title: "通知收件箱" },
  account: { title: "账号与管理" },
};
// Keep the established import seams so contract tests exercise the coordinator independently.
const ViewBoard = BoardGrid as ComponentType<any>;
const ViewCreate = CreateItemDialog as ComponentType<any>;
const ViewFocus = DailyFocus as ComponentType<any>;
const ViewTable = DeliveryTable as ComponentType<any>;
const ViewItem = ItemPanel as ComponentType<any>;
const ViewProject = ProjectWorkspace as ComponentType<any>;
const ViewTools = TaskOperationsPanel as ComponentType<any>;
export function DeliveryWorkspace({
  sessionName,
  sessionDescription,
  onAccount,
  onLogout,
  accountContent,
  accountOpen,
  onLeaveAccount,
  onSessionExpired,
  sessionBusy,
  notice,
}: WorkspaceProps = {}) {
  const [dashboard, setDashboard] = useState<Dashboard>(
    () => normalizeDashboard() as Dashboard,
  );
  const [projects, setProjects] = useState<Project[]>([]);
  const [planning, setPlanning] = useState<Planning>(EMPTY_PLANNING);
  const [savedViews, setSavedViews] = useState<SavedView[]>([]);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [taskFilter, setTaskFilter] = useState<ItemFilter>(EMPTY_FILTER);
  const [projectProgress, setProjectProgress] = useState<Progress | null>(null);
  const [projectSchedule, setProjectSchedule] = useState<Schedule | null>(null);
  const [memberWeek, setMemberWeek] = useState<MemberWeek | null>(null);
  const [r2Available, setR2Available] = useState(true);
  const [activeBoard, setActiveBoard] = useState<string | null>(null);
  const [activeSurface, setActiveSurface] =
    useState<DeliverySurface>("cockpit");
  const [focusFilter, setFocusFilter] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [detailStart, setDetailStart] = useState<string>("overview");
  const [split, setSplit] = useState(true);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [isCreateOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [dataUnavailable, setDataUnavailable] = useState(false);
  const [error, setError] = useState<RequestFailure | string | null>(null);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const dirty = useRef(false);
  const mutationLock = useRef(false);
  const alive = useRef(true);
  const dashboardSeq = useRef(0);
  const workspaceSeq = useRef(0);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      dashboardSeq.current++;
      workspaceSeq.current++;
    };
  }, []);
  const onError = useCallback(
    (cause: unknown) => {
      if (!alive.current) return;
      if (cause instanceof Error && (cause as RequestFailure).status === 401) {
        onSessionExpired?.();
        return;
      }
      setError(cause instanceof Error ? cause : "操作未完成。");
    },
    [onSessionExpired],
  );
  const refreshDashboard = useCallback(async () => {
    const seq = ++dashboardSeq.current;
    setLoading(true);
    try {
      const next = normalizeDashboard(await fetchDashboard()) as Dashboard;
      if (alive.current && seq === dashboardSeq.current) {
        setDashboard(next);
        setDataUnavailable(false);
      }
      return next;
    } catch (cause) {
      if (seq === dashboardSeq.current) {
        setDataUnavailable(true);
        onError(cause);
      }
      return null;
    } finally {
      if (alive.current && seq === dashboardSeq.current) setLoading(false);
    }
  }, [onError]);
  const refreshWorkspace = useCallback(async () => {
    const seq = ++workspaceSeq.current;
    try {
      const result = (await loadR2Workspace({
        projects: { load: fetchProjects, fallback: [] },
        releases: { load: fetchReleases, fallback: [] },
        sprints: { load: fetchSprints, fallback: [] },
        milestones: { load: fetchMilestones, fallback: [] },
        views: { load: fetchSavedViews, fallback: [] },
        inbox: { load: fetchNotifications, fallback: [] },
      })) as {
        available: boolean;
        values: {
          projects: Project[];
          releases: Planning["releases"];
          sprints: Planning["sprints"];
          milestones: Planning["milestones"];
          views: SavedView[];
          inbox: Notification[];
        };
      };
      if (!alive.current || seq !== workspaceSeq.current) return;
      setProjects(result.values.projects);
      setPlanning({
        releases: result.values.releases,
        sprints: result.values.sprints,
        milestones: result.values.milestones,
      });
      setSavedViews(result.values.views);
      setNotifications(result.values.inbox);
      setR2Available(result.available);
    } catch (cause) {
      onError(cause);
    }
  }, [onError]);
  const refreshAll = useCallback(async () => {
    const [next] = await Promise.all([refreshDashboard(), refreshWorkspace()]);
    if (alive.current) setRefreshVersion((v) => v + 1);
    return next;
  }, [refreshDashboard, refreshWorkspace]);
  useEffect(() => {
    void refreshAll();
  }, [refreshAll]);
  useEffect(() => {
    setProjectProgress(null);
    setProjectSchedule(null);
    if (!taskFilter.projectId || !r2Available) return;
    let active = true;
    void Promise.all([
      fetchProjectProgress(taskFilter.projectId),
      fetchProjectSchedule(taskFilter.projectId),
    ])
      .then(([p, s]) => {
        if (active && alive.current) {
          setProjectProgress(p);
          setProjectSchedule(s);
        }
      })
      .catch((cause) => {
        if (active) onError(cause);
      });
    return () => {
      active = false;
    };
  }, [taskFilter.projectId, r2Available, refreshVersion, onError]);
  const visibleItems = useMemo(
    () =>
      filterDeliveryItems(
        filterItems(
          activeBoard
            ? dashboard.items.filter((i) => i.board === activeBoard)
            : dashboard.items,
          focusFilter,
        ),
        taskFilter,
      ) as WorkItem[],
    [dashboard.items, activeBoard, focusFilter, taskFilter],
  );
  const selectedItem = expanded
    ? (dashboard.items.find((i) => i.id === selectedId) ?? null)
    : (visibleItems.find((i) => i.id === selectedId) ??
      visibleItems[0] ??
      null);
  const focus = dailyFocus(dashboard.items);
  const surface = accountOpen ? "account" : activeSurface;
  const sampleVisible = dashboard.items.some((i) => i.isSample);
  const canWrite = r2Available && !dataUnavailable && !loading;
  const onDirtyChange = useCallback((value: boolean) => {
    dirty.current = value;
  }, []);
  function canLeave() {
    if (!dirty.current) return true;
    if (window.confirm("有未保存的事项草稿。确定放弃并离开吗？")) {
      dirty.current = false;
      return true;
    }
    return false;
  }
  function navigateTo(s: DeliverySurface) {
    if (!canLeave()) return;
    if (onLeaveAccount?.() === false) return;
    setExpanded(false);
    setActiveSurface(s);
    setActiveBoard(null);
    setFocusFilter("all");
  }
  function selectBoard(board: string | null) {
    if (!canLeave()) return;
    if (onLeaveAccount?.() === false) return;
    setExpanded(false);
    setSelectedId(null);
    setActiveBoard(board);
    setActiveSurface(board ? "items" : "cockpit");
    setFocusFilter("all");
    setTaskFilter({ ...EMPTY_FILTER });
  }
  function selectFocus(value: string) {
    if (!canLeave()) return;
    if (onLeaveAccount?.() === false) return;
    setExpanded(false);
    setActiveSurface("items");
    setActiveBoard(null);
    setTaskFilter({ ...EMPTY_FILTER });
    setFocusFilter(value);
  }
  function openItem(id: string) {
    if (!canLeave()) return;
    if (!dashboard.items.some((i) => i.id === id)) {
      setError("关联对象不在当前可读事项集合中；请刷新或核对对象类型。");
      return;
    }
    if (onLeaveAccount?.() === false) return;
    setDetailStart("overview");
    setSelectedId(id);
    setExpanded(true);
    setActiveSurface("items");
  }
  function selectItem(id: string) {
    if (!canLeave()) return;
    setSelectedId(id);
    if (!split) {
      setDetailStart("overview");
      setExpanded(true);
    }
  }
  function expectedRevisionFor(id: string, snapshot?: number) {
    const revision =
      snapshot ?? dashboard.items.find((i) => i.id === id)?.revision;
    if (
      typeof revision !== "number" ||
      !Number.isInteger(revision) ||
      revision <= 0
    )
      throw new Error("当前事项缺少有效版本，请刷新后重试。");
    return revision;
  }
  async function mutate(operation: () => Promise<any>) {
    if (mutationLock.current) {
      setError("另一项提交尚未返回，请等待回执后重试。");
      return null;
    }
    mutationLock.current = true;
    setError(null);
    try {
      const result = await operation();
      await refreshAll();
      return result;
    } catch (cause) {
      onError(cause);
      return null;
    } finally {
      mutationLock.current = false;
    }
  }
  const handleContext = (
    id: string,
    input: Record<string, unknown>,
    rev?: number,
  ) => mutate(() => updateItemContext(id, expectedRevisionFor(id, rev), input));
  const handleUpdate = (
    id: string,
    input: Record<string, unknown>,
    rev?: number,
  ) => mutate(() => updateWorkItem(id, expectedRevisionFor(id, rev), input));
  const handleComment = (id: string, body: string, rev?: number) =>
    mutate(() => addComment(id, expectedRevisionFor(id, rev), body));
  const handleAdvance = (
    id: string,
    gate: string,
    evidence: any,
    rev?: number,
  ) =>
    mutate(() => advanceGate(id, expectedRevisionFor(id, rev), gate, evidence));
  const handleClose = (id: string, retrospective: string, rev?: number) =>
    mutate(() => closeItem(id, expectedRevisionFor(id, rev), retrospective));
  async function handleCreate(input: Record<string, unknown>) {
    const result = await mutate(() => createItem(input));
    if (result?.id) {
      setTaskFilter({ ...EMPTY_FILTER });
      setActiveBoard(null);
      setFocusFilter("all");
      setSelectedId(result.id);
      setDetailStart("overview");
      setActiveSurface("items");
      setExpanded(true);
    }
    return result;
  }
  async function handleWeek(member: string, start: string) {
    try {
      const result = await fetchMemberWeek(member, start);
      if (alive.current) setMemberWeek(result);
      return result;
    } catch (cause) {
      onError(cause);
      return null;
    }
  }
  function applyView(view: SavedView) {
    if (!canLeave()) return;
    setTaskFilter({ ...EMPTY_FILTER, ...view.filter });
    setActiveBoard(null);
    setFocusFilter("all");
    setFiltersOpen(false);
    setExpanded(false);
    setActiveSurface("items");
  }
  const commonDetail = {
    initialTab: detailStart,
    item: selectedItem,
    planning,
    projectName: projects.find((p) => p.id === selectedItem?.projectId)?.name,
    readOnly: !canWrite,
    onUpdateContext: handleContext,
    onUpdateItem: handleUpdate,
    onAddComment: handleComment,
    onAdvance: handleAdvance,
    onClose: handleClose,
    onDirtyChange,
    onExpand: (action?: string) => {
      setDetailStart(action || "overview");
      setExpanded(true);
      if (selectedItem) setSelectedId(selectedItem.id);
    },
    onBack: () => {
      if (canLeave()) setExpanded(false);
    },
  };
  const tools = (
    <ViewTools
      filter={taskFilter}
      onFilterChange={setTaskFilter}
      projects={projects}
      notifications={notifications}
      savedViews={savedViews}
      onSaveView={(input: Record<string, unknown>) =>
        mutate(() => saveView(input))
      }
      onApplyView={applyView}
      onLoadMemberWeek={handleWeek}
      week={memberWeek}
      onOpenItem={openItem}
      mode={surface === "weekly" ? "weekly" : "filters"}
    />
  );
  const legacy = (
    <Notice title="当前为旧后端对照模式">
      五板块驾驶舱和事项列表保持可读；项目、排期、通知和 MCP 对应的 R2
      工作流需要连接 Yunka 后端。
    </Notice>
  );
  return (
    <SidebarProvider
      className="app delivery-shell"
      style={
        {
          "--sidebar-width": "var(--layout-sidebar-primary-width)",
        } as CSSProperties
      }
    >
      <DeliverySidebar
        activeBoard={activeBoard}
        activeSurface={surface}
        boards={dashboard.boards}
        onNavigate={navigateTo}
        onSelectBoard={selectBoard}
        sampleVisible={sampleVisible}
        onCreate={() => {
          if (canLeave()) setCreateOpen(true);
        }}
        canCreate={canWrite}
        sessionName={sessionName}
        sessionDescription={sessionDescription}
        onAccount={
          onAccount
            ? () => {
                if (canLeave()) onAccount();
              }
            : undefined
        }
        onLogout={
          onLogout
            ? () => {
                if (canLeave()) onLogout();
              }
            : undefined
        }
        busy={sessionBusy}
      />
      <main className="workspace">
        <header className="topbar">
          <ListChecks className="icon" />
          <h1>
            {surface === "items" && activeBoard
              ? activeBoard
              : surfaceCopy[surface].title}
          </h1>
          {surface === "items" ? <Chip>{visibleItems.length} 项</Chip> : null}
          <div className="actions">
            {surface !== "account" ? (
              <>
                <Action
                  variant="ghost"
                  className="ghost"
                  busy={loading}
                  icon={RefreshCw}
                  onClick={() => {
                    setError(null);
                    void refreshAll();
                  }}
                >
                  刷新数据
                </Action>
                <Action
                  primary
                  disabled={!canWrite}
                  icon={Plus}
                  onClick={() => {
                    if (canLeave()) setCreateOpen(true);
                  }}
                >
                  新建交付事项
                </Action>
              </>
            ) : null}
          </div>
        </header>
        {notice ? <div className="workspace-feedback">{notice}</div> : null}
        {error ? (
          <div className="workspace-feedback">
            <Failure
              error={error}
              onRetry={() => {
                setError(null);
                void refreshAll();
              }}
            />
          </div>
        ) : null}
        <div className="surface">
          {surface === "account" ? (
            accountContent
          ) : surface === "cockpit" ? (
            <div className="page cockpit-page">
              <Heading
                title="每日交付总览"
                description="从五个板块查看风险，把关注项推进到可验证的交付。"
              />
              {!r2Available ? legacy : null}
              {dataUnavailable ? (
                <Notice title="数据暂不可用" tone="warning">
                  以下可能为上一次读取结果；不可据此判断当前健康状态。
                </Notice>
              ) : null}
              <ViewFocus
                focus={focus}
                total={dashboard.items.length}
                activeFilter={focusFilter}
                onSelect={selectFocus}
              />
              <ViewBoard
                boards={dashboard.boards}
                items={dashboard.items}
                activeBoard={activeBoard}
                onSelectBoard={selectBoard}
              />
              <div className="two-columns cockpit-bottom">
                <section>
                  <div className="section-title with-action">
                    <h3>需要关注的事项</h3>
                    <Action
                      className="ghost"
                      variant="ghost"
                      onClick={() => selectFocus("attention")}
                    >
                      查看全部
                    </Action>
                  </div>
                  {dashboard.items
                    .filter(
                      (i) =>
                        i.status === "blocked" ||
                        (i.dueDate &&
                          i.dueDate <
                            new Date().toLocaleDateString("sv-SE", {
                              timeZone: "Asia/Shanghai",
                            }) &&
                          i.status !== "closed"),
                    )
                    .slice(0, 4)
                    .map((i) => (
                      <div className="attention-item" key={i.id}>
                        <TriangleAlert className="icon" />
                        <div>
                          <button
                            className="row-open"
                            onClick={() => openItem(i.id)}
                          >
                            {i.title}
                          </button>
                          <p className="caption">
                            {i.id} · {i.owner || "未指定"}
                          </p>
                        </div>
                        <Status value={i.status} />
                      </div>
                    ))}
                  {!dashboard.items.length && !loading ? (
                    <p className="caption">
                      尚无交付事项。创建后可查看交付关注项。
                    </p>
                  ) : null}
                </section>
                <section>
                  <h3>交付关卡</h3>
                  <GateTrack vertical />
                  <p className="caption">
                    关卡推进必须附证据，生产验证后填写复盘才能关闭。
                  </p>
                </section>
              </div>
            </div>
          ) : surface === "items" ? (
            <>
              {!r2Available ? legacy : null}
              {expanded ? (
                <ViewItem {...commonDetail} />
              ) : (
                <div className={split ? "list-peek" : "items-full"}>
                  <section className="list-main">
                    <div className="list-toolbar">
                      <label className="search-box">
                        <Search className="icon" />
                        <Input
                          aria-label="搜索交付事项"
                          placeholder="搜索标题、设备、PR 或测试证据"
                          value={taskFilter.query}
                          onChange={(e) =>
                            setTaskFilter((f) => ({
                              ...f,
                              query: e.target.value,
                            }))
                          }
                        />
                      </label>
                      <Action
                        icon={Filter}
                        onClick={() => setFiltersOpen((v) => !v)}
                        disabled={!r2Available}
                      >
                        筛选
                      </Action>
                      <Action
                        icon={PanelRight}
                        onClick={() => setSplit((v) => !v)}
                        aria-pressed={split}
                      >
                        {split ? "列表" : "分栏"}
                      </Action>
                    </div>
                    {filtersOpen && r2Available ? (
                      <div className="filter-drawer">{tools}</div>
                    ) : null}
                    {focusFilter !== "all" ? (
                      <div className="active-filter">
                        <Chip>
                          {focusFilter === "blocked"
                            ? "受阻"
                            : focusFilter === "overdue"
                              ? "逾期"
                              : focusFilter === "verification"
                                ? "验证中"
                                : "需要关注"}
                        </Chip>
                        <Action
                          variant="ghost"
                          className="ghost"
                          onClick={() => setFocusFilter("all")}
                        >
                          清除关注筛选
                        </Action>
                      </div>
                    ) : null}
                    {loading && !dashboard.items.length ? (
                      <div
                        className="loading-list"
                        aria-label="正在同步交付数据"
                      >
                        {[0, 1, 2, 3].map((i) => (
                          <Skeleton key={i} className="h-12 w-full" />
                        ))}
                      </div>
                    ) : (
                      <ViewTable
                        items={visibleItems}
                        selectedId={selectedItem?.id}
                        compact={split}
                        onSelectItem={selectItem}
                        onOpenItem={openItem}
                      />
                    )}
                  </section>
                  {split ? <ViewItem {...commonDetail} compact /> : null}
                </div>
              )}
            </>
          ) : !r2Available ? (
            <div className="page">{legacy}</div>
          ) : surface === "projects" ? (
            <ViewProject
              activeProjectId={taskFilter.projectId}
              projects={projects}
              planning={planning}
              progress={projectProgress}
              schedule={projectSchedule}
              items={dashboard.items}
              onSelectProject={(projectId: string) =>
                setTaskFilter((f) => ({ ...f, projectId }))
              }
              onOpenItem={openItem}
              onCreateProject={(input: Record<string, unknown>) =>
                mutate(() => createProject(input))
              }
              onCreateRelease={(input: Record<string, unknown>) =>
                mutate(() => createRelease(input))
              }
              onCreateSprint={(input: Record<string, unknown>) =>
                mutate(() => createSprint(input))
              }
              onCreateMilestone={(input: Record<string, unknown>) =>
                mutate(() => createMilestone(input))
              }
            />
          ) : surface === "weekly" ? (
            tools
          ) : (
            <NotificationsView
              notifications={notifications}
              onOpenItem={openItem}
            />
          )}
        </div>
        <footer className="workbench-status">
          <span>
            {loading
              ? "正在读取…"
              : dataUnavailable
                ? "读取失败 · 保留上次结果"
                : dashboard.generatedAt
                  ? `数据快照 ${dashboard.generatedAt}`
                  : "交付状态以服务端为准"}
          </span>
          <span>
            {sampleVisible ? "含本地示例数据 · " : ""}SQLite 主数据 · Obsidian
            单向投影
          </span>
        </footer>
      </main>
      {isCreateOpen ? (
        <ViewCreate
          projects={projects}
          {...planning}
          onCreate={handleCreate}
          onClose={() => setCreateOpen(false)}
          onCheckSimilar={(input: any) =>
            findSimilar({
              title: input.title,
              board: input.board,
              projectId: input.projectId,
              kind: input.kind || "task",
            })
          }
        />
      ) : null}
    </SidebarProvider>
  );
}
