import { useCallback, useEffect, useMemo, useState } from "react";

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
} from "./api.js";
import { BoardGrid, StatusLegend } from "./components/BoardGrid.jsx";
import { CreateItemDialog } from "./components/CreateItemDialog.jsx";
import { DailyFocus } from "./components/DailyFocus.jsx";
import { DeliveryTable } from "./components/DeliveryTable.jsx";
import { ItemPanel } from "./components/ItemPanel.jsx";
import { ProjectWorkspace } from "./components/ProjectWorkspace.jsx";
import { ProjectScheduleHealth } from "./components/ProjectScheduleHealth.jsx";
import { Sidebar } from "./components/Sidebar.jsx";
import { TaskOperationsPanel } from "./components/TaskOperationsPanel.jsx";
import { dailyFocus, filterItems, gateLabel, normalizeDashboard } from "./lib/presentation.mjs";
import { filterDeliveryItems } from "./lib/r2-presentation.mjs";

export default function App() {
  const [dashboard, setDashboard] = useState(() => normalizeDashboard());
  const [projects, setProjects] = useState([]);
  const [planning, setPlanning] = useState({ releases: [], sprints: [], milestones: [] });
  const [savedViews, setSavedViews] = useState([]);
  const [notifications, setNotifications] = useState([]);
  const [taskFilter, setTaskFilter] = useState({ projectId: "", owner: "", status: "", kind: "", query: "" });
  const [projectProgress, setProjectProgress] = useState(null);
  const [projectSchedule, setProjectSchedule] = useState(null);
  const [memberWeek, setMemberWeek] = useState(null);
  const [activeBoard, setActiveBoard] = useState(null);
  const [focusFilter, setFocusFilter] = useState("all");
  const [selectedId, setSelectedId] = useState(null);
  const [isCreateOpen, setCreateOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const refreshDashboard = useCallback(async () => {
    setLoading(true);
    try {
      const next = normalizeDashboard(await fetchDashboard());
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
      const [nextProjects, releases, sprints, milestones, views, inbox] = await Promise.all([
        fetchProjects(), fetchReleases(), fetchSprints(), fetchMilestones(), fetchSavedViews(), fetchNotifications(),
      ]);
      setProjects(nextProjects);
      setPlanning({ releases, sprints, milestones });
      setSavedViews(views);
      setNotifications(inbox);
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
      if (next?.items?.[0]) setSelectedId((current) => current ?? next.items[0].id);
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
			.catch((cause) => { if (active) setError(cause instanceof Error ? cause.message : "暂时无法计算项目排期健康。"); });
    return () => { active = false; };
  }, [taskFilter.projectId]);

  const boardItems = activeBoard ? dashboard.items.filter((item) => item.board === activeBoard) : dashboard.items;
  const focusItems = filterItems(boardItems, focusFilter);
  const visibleItems = useMemo(() => filterDeliveryItems(focusItems, taskFilter), [focusItems, taskFilter]);
  const selectedItem = visibleItems.find((item) => item.id === selectedId) ?? visibleItems[0] ?? null;
  const focus = dailyFocus(dashboard.items);
  const sampleVisible = dashboard.items.some((item) => item.isSample);

  function selectBoard(board) {
    setActiveBoard(board);
    setSelectedId(null);
  }

  function selectFocus(filter) {
    setFocusFilter((current) => filter === current ? "all" : filter);
    setSelectedId(null);
  }

  async function runMutation(operation) {
    try {
      const result = await operation();
      await refreshAll();
      if (result?.id) setSelectedId(result.id);
      return result;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作未完成，请稍后重试。");
      return null;
    }
  }

  async function runWorkspaceMutation(operation) {
    try {
      const result = await operation();
      await refreshAll();
      return result;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作未完成，请稍后重试。");
      return null;
    }
  }

  function handleCreate(input) { return runMutation(() => createItem(input)); }
  function expectedRevisionFor(id) {
    const revision = dashboard.items.find((item) => item.id === id)?.revision;
    if (!Number.isInteger(revision) || revision <= 0) throw new Error("当前事项缺少有效版本，请刷新后重试。");
    return revision;
  }
  function handleContext(id, input) { return runMutation(() => updateItemContext(id, expectedRevisionFor(id), input)); }
  function handleUpdateItem(id, input) { return runMutation(() => updateWorkItem(id, expectedRevisionFor(id), input)); }
  function handleAddComment(id, body) { return runMutation(() => addComment(id, expectedRevisionFor(id), body)); }
  function handleAdvance(id, gate, evidence) { return runMutation(() => advanceGate(id, expectedRevisionFor(id), gate, evidence)); }
  function handleClose(id, retrospective) { return runMutation(() => closeItem(id, expectedRevisionFor(id), retrospective)); }
  function handleCreateProject(input) { return runWorkspaceMutation(() => createProject(input)); }
  function handleCreateRelease(input) { return runWorkspaceMutation(() => createRelease(input)); }
  function handleCreateSprint(input) { return runWorkspaceMutation(() => createSprint(input)); }
  function handleCreateMilestone(input) { return runWorkspaceMutation(() => createMilestone(input)); }
  function handleSaveView(input) { return runWorkspaceMutation(() => saveView(input)); }

  async function handleCheckSimilar(input) {
    try {
      return await findSimilar({ title: input.title, board: input.board, projectId: input.projectId, kind: input.kind || "task" });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法检查相似事项。");
      throw cause;
    }
  }

  async function handleLoadMemberWeek(member, weekStart) {
    try {
      const next = await fetchMemberWeek(member, weekStart);
      setMemberWeek(next);
      return next;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法读取成员周任务。");
      return null;
    }
  }

  function selectProject(projectId) {
    setTaskFilter((current) => ({ ...current, projectId }));
    setSelectedId(null);
  }

  function applySavedView(view) {
    setTaskFilter({ projectId: "", owner: "", status: "", kind: "", query: "", ...(view.filter ?? {}) });
    setSelectedId(null);
  }

  return (
    <div className="app-shell">
      <Sidebar activeBoard={activeBoard} boards={dashboard.boards} onSelectBoard={selectBoard} sampleVisible={sampleVisible} />
      <main className="workspace">
        <header className="workspace-header">
          <div>
            <span className="eyebrow">IoT 研发交付管理</span>
            <h1>{activeBoard ?? "交付驾驶舱"}</h1>
            <p>用同一条交付链路管理规划、排期、方案、决策、设备范围、验证与复盘。</p>
          </div>
          <div className="header-actions">
            <button className="quiet-button" onClick={() => void refreshAll()} type="button">↻ 刷新</button>
            <button className="primary-button" onClick={() => setCreateOpen(true)} type="button">＋ 新建交付事项</button>
          </div>
        </header>

        {error ? <div className="error-banner" role="alert">{error}<button onClick={() => setError("")} type="button">知道了</button></div> : null}
        {sampleVisible ? <div className="sample-banner">这是可操作的本地示例事项。创建真实事项后，示例不会影响你的交付数据。</div> : null}

        <ProjectWorkspace activeProjectId={taskFilter.projectId} onCreateMilestone={handleCreateMilestone} onCreateProject={handleCreateProject} onCreateRelease={handleCreateRelease} onCreateSprint={handleCreateSprint} onSelectProject={selectProject} planning={planning} progress={projectProgress} projects={projects} schedule={projectSchedule} />
		<ProjectScheduleHealth schedule={projectSchedule} />
        <TaskOperationsPanel filter={taskFilter} notifications={notifications} onApplyView={applySavedView} onFilterChange={setTaskFilter} onLoadMemberWeek={handleLoadMemberWeek} onSaveView={handleSaveView} savedViews={savedViews} week={memberWeek} />

        <DailyFocus activeFilter={focusFilter} focus={focus} onSelect={selectFocus} />
        <BoardGrid activeBoard={activeBoard} boards={dashboard.boards} onSelectBoard={selectBoard} />

        <section className="release-strip" aria-label="交付关卡总览">
          <div><span className="eyebrow">发布路径</span><h2>从规划到复盘，每一关都有证据</h2></div>
          <div className="release-gates">{["planning", "solution_reviewed", "development_completed", "test_passed", "production_validated"].map((gate, index) => <div key={gate}><span>{index + 1}</span><small>{gateLabel(gate)}</small></div>)}</div>
        </section>

        <section className="content-grid">
          <div className="delivery-list-card">
            <div className="section-header"><div><span className="eyebrow">{activeBoard ?? "全部板块"}</span><h2>交付事项</h2></div><span className="item-count">{visibleItems.length} 项</span></div>
            <StatusLegend />
            {loading ? <div className="loading-state">正在同步交付数据…</div> : <DeliveryTable items={visibleItems} onSelectItem={setSelectedId} selectedId={selectedItem?.id} />}
          </div>
          <ItemPanel item={selectedItem} onAddComment={handleAddComment} onAdvance={handleAdvance} onClose={handleClose} onUpdateContext={handleContext} onUpdateItem={handleUpdateItem} planning={planning} />
        </section>
      </main>
      {isCreateOpen ? <CreateItemDialog milestones={planning.milestones} onCheckSimilar={handleCheckSimilar} onClose={() => setCreateOpen(false)} onCreate={handleCreate} projects={projects} releases={planning.releases} sprints={planning.sprints} /> : null}
    </div>
  );
}
