// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";

import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import { DeliveryWorkspace } from "@/components/delivery-workspace";
import {
  fetchMilestones,
  fetchNotifications,
  fetchProjects,
  fetchReleases,
  fetchSavedViews,
  fetchSprints,
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
  items: [{ id: "delivery-1", board: "研发交付效能", title: "交付证据联通", isSample: false }],
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
  DeliveryTable: ({ items }: { items: typeof dashboard.items }) => <div data-testid="delivery-table">{items.length} 项</div>,
}));
vi.mock("@/src/components/ItemPanel.jsx", () => ({ ItemPanel: () => <aside data-testid="item-panel" /> }));
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
		vi.mocked(fetchReleases).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchSprints).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchMilestones).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchSavedViews).mockRejectedValueOnce(unsupported);
		vi.mocked(fetchNotifications).mockRejectedValueOnce(unsupported);

		render(<DeliveryWorkspace />);

		await waitFor(() => expect(screen.getByText("当前为旧后端对照模式")).toBeInTheDocument());
		expect(screen.getByTestId("board-grid")).toBeInTheDocument();
		expect(screen.getByText("当前为旧后端对照模式")).toBeInTheDocument();
		expect(screen.queryByTestId("project-workspace")).not.toBeInTheDocument();
	});
});
