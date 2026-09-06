// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import { DeliveryWorkspace } from "@/components/delivery-workspace";
import {
	addComment,
	advanceGate,
	closeItem,
	fetchMilestones,
  fetchNotifications,
  fetchProjects,
  fetchReleases,
  fetchSavedViews,
	fetchSprints,
	updateItemContext,
	updateWorkItem,
} from "@/src/api.js";

afterEach(() => {
  cleanup();
});

const dashboard = {
  boards: [
    { board: "设备质量与连接", total: 1 },
    { board: "产品与平台能力", total: 0 },
    { board: "研发交付效能", total: 2 },
    { board: "运营保障与安全", total: 0 },
    { board: "客户与业务价值", total: 0 },
  ],
  items: [{ id: "delivery-1", board: "研发交付效能", title: "交付证据联通", revision: 7, isSample: false }],
};

vi.mock("@/src/api.js", () => ({
  addComment: vi.fn(),
  advanceGate: vi.fn(),
  closeItem: vi.fn(),
  createItem: vi.fn(),
  createMilestone: vi.fn(),
  createProject: vi.fn(),
  createRelease: vi.fn(),
  createSprint: vi.fn(),
  fetchDashboard: vi.fn(async () => dashboard),
  fetchItems: vi.fn(async () => dashboard.items),
  fetchMemberWeek: vi.fn(),
  fetchMilestones: vi.fn(async () => []),
  fetchNotifications: vi.fn(async () => []),
  fetchProjectProgress: vi.fn(async () => null),
	fetchProjectSchedule: vi.fn(async () => ({ totalItems: 1, scheduledItems: 1 })),
  fetchProjects: vi.fn(async () => []),
  fetchReleases: vi.fn(async () => []),
  fetchSavedViews: vi.fn(async () => []),
  fetchSprints: vi.fn(async () => []),
  findSimilar: vi.fn(),
  saveView: vi.fn(),
  updateItemContext: vi.fn(),
  updateWorkItem: vi.fn(),
}));

vi.mock("@/src/lib/presentation.mjs", () => ({
  normalizeDashboard: (payload: typeof dashboard = dashboard) => payload,
  dailyFocus: () => ({ dueToday: [], overdue: [], blockers: [] }),
  filterItems: (items: typeof dashboard.items) => items,
  gateLabel: (gate: string) => gate,
}));

vi.mock("@/src/lib/r2-presentation.mjs", () => ({
  filterDeliveryItems: (items: typeof dashboard.items) => items,
}));

vi.mock("@/components/delivery-sidebar", () => ({
  DeliverySidebar: ({ onNavigate, onSelectBoard }: { onNavigate: (surface: string) => void; onSelectBoard: (board: string | null) => void }) => (
    <aside data-testid="delivery-sidebar">
      <button onClick={() => onNavigate("projects")} type="button">项目与排期</button>
      <button onClick={() => onNavigate("items")} type="button">交付事项</button>
      <button onClick={() => onSelectBoard("研发交付效能")} type="button">研发交付效能</button>
    </aside>
  ),
}));

vi.mock("@/src/components/ProjectWorkspace.jsx", () => ({
	ProjectWorkspace: ({ onSelectProject, schedule }: { onSelectProject: (projectId: string) => void; schedule: { totalItems?: number; scheduledItems?: number } | null }) => (
		<div data-testid="project-workspace">
			<button onClick={() => onSelectProject("PRJ-1")} type="button">选择项目</button>
			<span>{schedule ? `${schedule.scheduledItems}/${schedule.totalItems}` : "无排期"}</span>
		</div>
	),
}));
vi.mock("@/src/components/TaskOperationsPanel.jsx", () => ({ TaskOperationsPanel: () => <div data-testid="task-operations" /> }));
vi.mock("@/src/components/DailyFocus.jsx", () => ({ DailyFocus: () => <div data-testid="daily-focus" /> }));
vi.mock("@/src/components/BoardGrid.jsx", () => ({
  BoardGrid: () => <div data-testid="board-grid" />,
  StatusLegend: () => <div data-testid="status-legend" />,
}));
vi.mock("@/src/components/DeliveryTable.jsx", () => ({
  DeliveryTable: ({ items, onSelectItem }: { items: typeof dashboard.items; onSelectItem: (id: string) => void }) => <div data-testid="delivery-table">{items.length} 项<button onClick={() => onSelectItem("delivery-1")} type="button">选择交付事项</button></div>,
}));
vi.mock("@/src/components/ItemPanel.jsx", () => ({
  ItemPanel: ({ onAddComment, onAdvance, onClose, onUpdateContext, onUpdateItem }: {
    onAddComment: (id: string, body: string) => void;
    onAdvance: (id: string, gate: string, evidence: string) => void;
    onClose: (id: string, retrospective: string) => void;
    onUpdateContext: (id: string, input: object) => void;
    onUpdateItem: (id: string, input: object) => void;
  }) => <aside data-testid="item-panel">
    <button onClick={() => onUpdateItem("delivery-1", { title: "已更新" })} type="button">更新事项</button>
    <button onClick={() => onUpdateContext("delivery-1", { plan: "已更新计划" })} type="button">更新上下文</button>
    <button onClick={() => onAddComment("delivery-1", "补充评论")} type="button">添加评论</button>
    <button onClick={() => onAdvance("delivery-1", "solution_reviewed", "评审通过")} type="button">推进关卡</button>
    <button onClick={() => onClose("delivery-1", "复盘完成")} type="button">关闭事项</button>
  </aside>,
}));
vi.mock("@/src/components/CreateItemDialog.jsx", () => ({
  CreateItemDialog: () => <div data-testid="create-item-dialog">新建事项表单</div>,
}));

beforeAll(() => {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
  class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  vi.stubGlobal("ResizeObserver", ResizeObserver);
});

describe("DeliveryWorkspace", () => {
	it("moves from the desktop cockpit into independent project and item workspaces", async () => {
    const user = userEvent.setup();
    render(<DeliveryWorkspace />);

    expect(screen.getByTestId("delivery-sidebar")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "交付驾驶舱" })).toBeInTheDocument();
    expect(screen.getByTestId("board-grid")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "项目与排期" }));
    expect(screen.getByRole("heading", { name: "项目与排期" })).toBeInTheDocument();
    expect(screen.getByTestId("project-workspace")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "选择项目" }));
    await waitFor(() => expect(screen.getByTestId("project-workspace")).toHaveTextContent("1/1"));

    await user.click(screen.getByRole("button", { name: "交付事项" }));
    expect(screen.getByRole("heading", { level: 1, name: "交付事项" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId("delivery-table")).toHaveTextContent("1 项"));

    await user.click(screen.getByRole("button", { name: /新建交付事项/ }));
    expect(screen.getByTestId("create-item-dialog")).toBeInTheDocument();
  });

	 it("degrades to the five-board read-only cockpit when the old comparison backend lacks R2 endpoints", async () => {
		const unsupported = Object.assign(new Error("endpoint not found"), { status: 404 });
		vi.mocked(fetchProjects).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchSavedViews).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchNotifications).mockRejectedValueOnce(unsupported);

		render(<DeliveryWorkspace />);

		await waitFor(() => expect(screen.getByText("当前为旧后端对照模式")).toBeInTheDocument());
		expect(screen.getByTestId("board-grid")).toBeInTheDocument();
		expect(screen.getByText("当前为旧后端对照模式")).toBeInTheDocument();
		expect(screen.queryByTestId("project-workspace")).not.toBeInTheDocument();
	});

	it("uses the selected work item's current revision for every mutation", async () => {
		const user = userEvent.setup();
		render(<DeliveryWorkspace />);

		await user.click(screen.getByRole("button", { name: "交付事项" }));
		await user.click(screen.getByRole("button", { name: "选择交付事项" }));
		await user.click(screen.getByRole("button", { name: "更新事项" }));
		await user.click(screen.getByRole("button", { name: "更新上下文" }));
		await user.click(screen.getByRole("button", { name: "添加评论" }));
		await user.click(screen.getByRole("button", { name: "推进关卡" }));
		await user.click(screen.getByRole("button", { name: "关闭事项" }));

		await waitFor(() => expect(updateWorkItem).toHaveBeenCalledWith("delivery-1", 7, { title: "已更新" }));
		expect(updateItemContext).toHaveBeenCalledWith("delivery-1", 7, { plan: "已更新计划" });
		expect(addComment).toHaveBeenCalledWith("delivery-1", 7, "补充评论");
		expect(advanceGate).toHaveBeenCalledWith("delivery-1", 7, "solution_reviewed", "评审通过");
		expect(closeItem).toHaveBeenCalledWith("delivery-1", 7, "复盘完成");
	});
});
