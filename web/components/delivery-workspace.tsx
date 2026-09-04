"use client";

import { type ComponentType, useCallback, useEffect, useMemo, useState } from "react";
import {
  CircleAlertIcon,
  FlaskConicalIcon,
  PlusIcon,
  RefreshCwIcon,
  RouteIcon,
} from "lucide-react";

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
import { dailyFocus, filterItems, gateLabel, normalizeDashboard } from "@/src/lib/presentation.mjs";
import { loadR2Workspace } from "@/src/lib/r2-capability.mjs";
import { filterDeliveryItems } from "@/src/lib/r2-presentation.mjs";
import { DeliverySidebar, type DeliverySurface } from "@/components/delivery-sidebar";
import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";

type WorkItem = {
  id: string;
  board: string;
  revision?: number;
  isSample?: boolean;
  [key: string]: unknown;
};

type Dashboard = {
  boards: Array<{ board: string; total?: number }>;
  items: WorkItem[];
};

type TaskFilter = {
  projectId: string;
  owner: string;
  status: string;
  kind: string;
  query: string;
};

type R2WorkspaceData = {
  projects: unknown[];
  releases: unknown[];
  sprints: unknown[];
  milestones: unknown[];
  views: unknown[];
  inbox: unknown[];
};

const emptyTaskFilter: TaskFilter = { projectId: "", owner: "", status: "", kind: "", query: "" };
const deliveryGates = ["planning", "solution_reviewed", "development_completed", "test_passed", "production_validated"];

const surfaceCopy: Record<DeliverySurface, { description: string; title: string }> = {
  cockpit: { title: "交付驾驶舱", description: "跨项目查看交付健康、风险与节奏。" },
  items: { title: "交付事项", description: "筛选、推进并沉淀可核验的交付证据。" },
  projects: { title: "项目与排期", description: "管理项目、版本、Sprint、里程碑与容量风险。" },
  weekly: { title: "成员周视图", description: "按成员查看当周工作与未排期事项。" },
  notifications: { title: "通知收件箱", description: "查看本地可靠投递，并为外部通道留出接入点。" },
};

// The migration deliberately retains these feature-complete JavaScript flows while
// their presentation is hosted by the new Next/shadcn workspace. The cast confines
// JS default-prop inference to this adapter boundary.
const LegacyBoardGrid = BoardGrid as ComponentType<any>;
const LegacyCreateItemDialog = CreateItemDialog as ComponentType<any>;
const LegacyDailyFocus = DailyFocus as ComponentType<any>;
const LegacyDeliveryTable = DeliveryTable as ComponentType<any>;
const LegacyItemPanel = ItemPanel as ComponentType<any>;
const LegacyProjectWorkspace = ProjectWorkspace as ComponentType<any>;
const LegacyStatusLegend = StatusLegend as ComponentType<any>;
const LegacyTaskOperationsPanel = TaskOperationsPanel as ComponentType<any>;

export function DeliveryWorkspace() {
  const [dashboard, setDashboard] = useState<Dashboard>(() => normalizeDashboard() as Dashboard);
  const [projects, setProjects] = useState<unknown[]>([]);
  const [planning, setPlanning] = useState({ releases: [] as unknown[], sprints: [] as unknown[], milestones: [] as unknown[] });
  const [savedViews, setSavedViews] = useState<unknown[]>([]);
  const [notifications, setNotifications] = useState<unknown[]>([]);
  const [taskFilter, setTaskFilter] = useState<TaskFilter>(emptyTaskFilter);
  const [projectProgress, setProjectProgress] = useState<unknown>(null);
  const [projectSchedule, setProjectSchedule] = useState<unknown>(null);
  const [r2Available, setR2Available] = useState(true);
  const [memberWeek, setMemberWeek] = useState<unknown>(null);
  const [activeBoard, setActiveBoard] = useState<string | null>(null);
  const [activeSurface, setActiveSurface] = useState<DeliverySurface>("cockpit");
  const [focusFilter, setFocusFilter] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [isCreateOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const refreshDashboard = useCallback(async () => {
    setLoading(true);
    try {
      const next = normalizeDashboard(await fetchDashboard()) as Dashboard;
      setDashboard(next);
      return next;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法读取交付数据。");
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshWorkspace = useCallback(async () => {
    try {
      const result = await loadR2Workspace({
        projects: { load: fetchProjects, fallback: [] },
        releases: { load: fetchReleases, fallback: [] },
        sprints: { load: fetchSprints, fallback: [] },
        milestones: { load: fetchMilestones, fallback: [] },
        views: { load: fetchSavedViews, fallback: [] },
        inbox: { load: fetchNotifications, fallback: [] },
      }) as { available: boolean; values: R2WorkspaceData };
      setProjects(result.values.projects);
      setPlanning({ releases: result.values.releases, sprints: result.values.sprints, milestones: result.values.milestones });
      setSavedViews(result.values.views);
      setNotifications(result.values.inbox);
      setR2Available(result.available);
      return true;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法读取项目与任务协作数据。");
      return false;
    }
  }, []);

  const refreshAll = useCallback(async () => {
    const [next] = await Promise.all([refreshDashboard(), refreshWorkspace()]);
    return next;
  }, [refreshDashboard, refreshWorkspace]);

  useEffect(() => {
    void refreshAll().then((next) => {
      if (next?.items?.[0]) {
        setSelectedId((current) => current ?? next.items[0].id);
      }
    });
  }, [refreshAll]);

  useEffect(() => {
    if (!taskFilter.projectId) {
      setProjectProgress(null);
      setProjectSchedule(null);
      return;
    }
    let active = true;
    void Promise.all([fetchProjectProgress(taskFilter.projectId), fetchProjectSchedule(taskFilter.projectId)])
      .then(([nextProgress, nextSchedule]) => {
        if (!active) return;
        setProjectProgress(nextProgress);
        setProjectSchedule(nextSchedule);
      })
      .catch((cause) => {
        if (active) setError(cause instanceof Error ? cause.message : "暂时无法计算项目排期健康。");
      });
    return () => {
      active = false;
    };
  }, [taskFilter.projectId]);

  const boardItems = activeBoard ? dashboard.items.filter((item) => item.board === activeBoard) : dashboard.items;
  const focusItems = filterItems(boardItems, focusFilter) as WorkItem[];
  const visibleItems = useMemo(
    () => filterDeliveryItems(focusItems, taskFilter) as WorkItem[],
    [focusItems, taskFilter],
  );
  const selectedItem = visibleItems.find((item) => item.id === selectedId) ?? visibleItems[0] ?? null;
  const focus = dailyFocus(dashboard.items);
  const sampleVisible = dashboard.items.some((item) => item.isSample);
  const page = activeSurface === "items" && activeBoard
    ? { ...surfaceCopy.items, title: activeBoard }
    : surfaceCopy[activeSurface];

  function navigateTo(surface: DeliverySurface) {
    setActiveSurface(surface);
    if (surface === "cockpit") setActiveBoard(null);
  }

  function selectBoard(board: string | null) {
    setActiveBoard(board);
    setSelectedId(null);
    setActiveSurface(board ? "items" : "cockpit");
  }

  function selectFocus(filter: string) {
    setFocusFilter((current) => (filter === current ? "all" : filter));
    setSelectedId(null);
  }

  async function runMutation(operation: () => Promise<any>) {
    try {
      const result = await operation();
      await refreshAll();
      if (typeof result?.id === "string") setSelectedId(result.id);
      return result;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作未完成，请稍后重试。");
      return null;
    }
  }

  async function runWorkspaceMutation(operation: () => Promise<any>) {
    try {
      const result = await operation();
      await refreshAll();
      return result;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作未完成，请稍后重试。");
      return null;
    }
  }

  async function handleCreate(input: any) {
    const result = await runMutation(() => createItem(input));
    if (result) setActiveSurface("items");
    return result;
  }
  function expectedRevisionFor(id: string) {
    const revision = dashboard.items.find((item) => item.id === id)?.revision;
    if (typeof revision !== "number" || !Number.isInteger(revision) || revision <= 0) throw new Error("当前事项缺少有效版本，请刷新后重试。");
    return revision;
  }
  function handleContext(id: string, input: any) { return runMutation(() => updateItemContext(id, expectedRevisionFor(id), input)); }
  function handleUpdateItem(id: string, input: any) { return runMutation(() => updateWorkItem(id, expectedRevisionFor(id), input)); }
  function handleAddComment(id: string, body: string) { return runMutation(() => addComment(id, expectedRevisionFor(id), body)); }
  function handleAdvance(id: string, gate: string, evidence: string) { return runMutation(() => advanceGate(id, expectedRevisionFor(id), gate, evidence)); }
  function handleClose(id: string, retrospective: string) { return runMutation(() => closeItem(id, expectedRevisionFor(id), retrospective)); }
  function handleCreateProject(input: any) { return runWorkspaceMutation(() => createProject(input)); }
  function handleCreateRelease(input: any) { return runWorkspaceMutation(() => createRelease(input)); }
  function handleCreateSprint(input: any) { return runWorkspaceMutation(() => createSprint(input)); }
  function handleCreateMilestone(input: any) { return runWorkspaceMutation(() => createMilestone(input)); }
  function handleSaveView(input: any) { return runWorkspaceMutation(() => saveView(input)); }

  async function handleCheckSimilar(input: any) {
    try {
      return await findSimilar({
        title: input.title,
        board: input.board,
        projectId: input.projectId,
        kind: input.kind || "task",
      });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法检查相似事项。");
      throw cause;
    }
  }

  async function handleLoadMemberWeek(member: string, weekStart: string) {
    try {
      const next = await fetchMemberWeek(member, weekStart);
      setMemberWeek(next);
      return next;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法读取成员周任务。");
      return null;
    }
  }

  function selectProject(projectId: string) {
    setTaskFilter((current) => ({ ...current, projectId }));
    setSelectedId(null);
  }

  function applySavedView(view: { filter?: Partial<TaskFilter> }) {
    setTaskFilter({ ...emptyTaskFilter, ...(view.filter ?? {}) });
    setSelectedId(null);
    setActiveSurface("items");
  }

  const taskOperations = (
    <LegacyTaskOperationsPanel
      filter={taskFilter}
      notifications={notifications}
      onApplyView={applySavedView}
      onFilterChange={setTaskFilter}
      onLoadMemberWeek={handleLoadMemberWeek}
      onSaveView={handleSaveView}
      savedViews={savedViews}
      week={memberWeek}
    />
  );

  const detailWorkspace = (
    <section className="delivery-detail-workspace" aria-label="交付事项与详情">
      <ResizablePanelGroup className="delivery-split-panel" orientation="horizontal">
        <ResizablePanel defaultSize="64%" id="delivery-list" minSize="500px">
          <Card className="delivery-list-card">
            <CardContent className="delivery-list-content">
              <div className="collection-heading">
                <div>
                  <p className="eyebrow">{activeBoard ?? "全部板块"}</p>
                  <h2>交付事项</h2>
                </div>
                <span className="item-count">{visibleItems.length} 项</span>
              </div>
              <LegacyStatusLegend />
              {loading ? (
                <div className="delivery-loading" aria-label="正在同步交付数据">
                  <Skeleton className="h-9 w-full" />
                  <Skeleton className="h-9 w-full" />
                  <Skeleton className="h-9 w-full" />
                </div>
              ) : <LegacyDeliveryTable items={visibleItems} onSelectItem={setSelectedId} selectedId={selectedItem?.id} />}
            </CardContent>
          </Card>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize="36%" id="delivery-detail" minSize="320px">
          <Card className="delivery-item-card">
            <div className="detail-panel-heading">
              <RouteIcon />
              <span>事项详情与交付证据</span>
            </div>
            <LegacyItemPanel
              item={selectedItem}
              onAddComment={handleAddComment}
              onAdvance={handleAdvance}
              onClose={handleClose}
              onUpdateContext={handleContext}
              onUpdateItem={handleUpdateItem}
              planning={planning}
            />
          </Card>
        </ResizablePanel>
      </ResizablePanelGroup>
    </section>
  );

  const r2Fallback = (
    <Alert className="legacy-comparison-notice">
      <FlaskConicalIcon />
      <AlertTitle>当前为旧后端对照模式</AlertTitle>
      <AlertDescription>五板块驾驶舱和事项列表保持可读；项目、排期、通知和 MCP 对应的 R2 工作流需要连接 Yunka 后端。</AlertDescription>
    </Alert>
  );

  return (
    <SidebarProvider className="delivery-shell" defaultOpen>
      <DeliverySidebar
        activeBoard={activeBoard}
        activeSurface={activeSurface}
        boards={dashboard.boards}
        onNavigate={navigateTo}
        onSelectBoard={selectBoard}
        sampleVisible={sampleVisible}
      />
      <SidebarInset className="delivery-inset">
        <div className="delivery-page">
          <header className="delivery-page-header">
            <div className="delivery-page-title">
              <SidebarTrigger className="delivery-sidebar-trigger" />
              <div>
                <div className="delivery-title-line">
                  <h1>{page.title}</h1>
                  {activeSurface === "items" ? <span className="page-count">{visibleItems.length} 项</span> : null}
                </div>
                <p>{page.description}</p>
              </div>
            </div>
            <div className="delivery-toolbar-actions">
              <Button onClick={() => void refreshAll()} size="sm" type="button" variant="outline">
                <RefreshCwIcon data-icon="inline-start" />
                刷新数据
              </Button>
              <Button disabled={!r2Available} onClick={() => setCreateOpen(true)} size="sm" type="button">
                <PlusIcon data-icon="inline-start" />
                新建交付事项
              </Button>
            </div>
          </header>

          {error ? (
            <Alert variant="destructive">
              <CircleAlertIcon />
              <AlertTitle>交付数据暂不可用</AlertTitle>
              <AlertDescription>{error}</AlertDescription>
              <AlertAction>
                <Button onClick={() => setError("")} size="xs" type="button" variant="outline">知道了</Button>
              </AlertAction>
            </Alert>
          ) : null}

          {sampleVisible ? (
            <Alert className="sample-notice">
              <FlaskConicalIcon />
              <AlertTitle>本地示例数据</AlertTitle>
              <AlertDescription>创建真实事项后，示例不会影响你的交付数据。</AlertDescription>
            </Alert>
          ) : null}

          {activeSurface === "cockpit" ? (
            <section className="delivery-cockpit" aria-label="每日交付概况">
              {!r2Available ? r2Fallback : null}
              <LegacyDailyFocus activeFilter={focusFilter} focus={focus} onSelect={selectFocus} />
              <Card className="delivery-board-card" size="sm">
                <CardContent>
                  <div className="collection-heading board-collection-heading">
                    <div>
                      <p className="eyebrow">交付板块</p>
                      <h2>五个板块的当前状态</h2>
                    </div>
                    <span className="collection-description">点击任一板块进入交付事项。</span>
                  </div>
                  <LegacyBoardGrid activeBoard={activeBoard} boards={dashboard.boards} onSelectBoard={selectBoard} />
                </CardContent>
              </Card>
              <section className="release-strip" aria-label="交付关卡总览">
                <div>
                  <p className="eyebrow">发布路径</p>
                  <h2>从规划到复盘，每一关都有可核验的证据</h2>
                </div>
                <div className="release-gates">
                  {deliveryGates.map((gate, index) => (
                    <div key={gate}>
                      <span>{index + 1}</span>
                      <small>{gateLabel(gate)}</small>
                    </div>
                  ))}
                </div>
              </section>
            </section>
          ) : null}

          {activeSurface === "items" ? (
            <section className="delivery-items-surface" aria-label="交付事项工作区">
              {r2Available ? (
                <details className="workspace-tools">
                  <summary>筛选与协作工具 <span>搜索、保存视图、成员周任务与通知</span></summary>
                  {taskOperations}
                </details>
              ) : r2Fallback}
              {detailWorkspace}
            </section>
          ) : null}

          {activeSurface === "projects" ? (
            <section className="workspace-stack" aria-label="项目与排期工作区">
              {r2Available ? (
                <>
                  <LegacyProjectWorkspace
                    activeProjectId={taskFilter.projectId}
                    onCreateMilestone={handleCreateMilestone}
                    onCreateProject={handleCreateProject}
                    onCreateRelease={handleCreateRelease}
                    onCreateSprint={handleCreateSprint}
                    onSelectProject={selectProject}
                    planning={planning}
                    progress={projectProgress}
                    projects={projects}
                    schedule={projectSchedule}
                  />
                  <ProjectScheduleHealth schedule={projectSchedule} />
                </>
              ) : r2Fallback}
            </section>
          ) : null}

          {activeSurface === "weekly" ? (
            <section className="workspace-stack weekly-workspace" aria-label="成员周视图工作区">
              {r2Available ? taskOperations : r2Fallback}
            </section>
          ) : null}

          {activeSurface === "notifications" ? (
            <section className="notification-workspace" aria-label="通知收件箱工作区">
              {r2Available ? (
                <>
                  <div className="collection-heading">
                    <div>
                      <p className="eyebrow">可靠事件投递</p>
                      <h2>{notifications.length} 条最近投递</h2>
                    </div>
                    <span className="collection-description">本地优先，可扩展企业微信、邮件或 Webhook。</span>
                  </div>
                  {notifications.length ? (
                    <ul className="notification-list">
                      {notifications.slice(0, 12).map((notification: any) => (
                        <li key={String(notification.deliveryId) + "-" + String(notification.channel)}>
                          <strong>{notification.title || notification.eventType}</strong>
                          <span>{notification.subject || "未关联事项"}</span>
                          <small>{notification.channel || "local"}</small>
                        </li>
                      ))}
                    </ul>
                  ) : <p className="notification-empty">尚无投递记录。任务状态、截止提醒与发布事件会先写入本地收件箱。</p>}
                </>
              ) : r2Fallback}
            </section>
          ) : null}
        </div>
      </SidebarInset>
      {isCreateOpen ? (
        <LegacyCreateItemDialog
          milestones={planning.milestones}
          onCheckSimilar={handleCheckSimilar}
          onClose={() => setCreateOpen(false)}
          onCreate={handleCreate}
          projects={projects}
          releases={planning.releases}
          sprints={planning.sprints}
        />
      ) : null}
    </SidebarProvider>
  );
}
