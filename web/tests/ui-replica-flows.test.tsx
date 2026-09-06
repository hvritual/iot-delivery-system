// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ItemPanel } from "@/components/delivery/item-workspace";
import { CreateItemDialog } from "@/components/delivery/create-item";
import { PolicyReference } from "@/components/delivery/policy-reference";
import { ProjectWorkspace } from "@/components/delivery/project-workspace";
import { safeReference, type WorkItem } from "@/components/delivery/model";
const item: WorkItem = {
  id: "IOT-test-1",
  title: "连接回归",
  board: "设备质量与连接",
  owner: "工程师",
  kind: "task",
  status: "in_progress",
  gate: "solution_reviewed",
  revision: 7,
  plan: "原规划",
  projectId: "P1",
  dependencies: [{ itemId: "other", relation: "related" }],
};
function commands() {
  return {
    onUpdateContext: vi.fn(async () => ({ id: item.id })),
    onUpdateItem: vi.fn(async () => ({ id: item.id })),
    onAddComment: vi.fn(async () => ({ id: "comment" })),
    onAdvance: vi.fn(async () => ({ id: item.id })),
    onClose: vi.fn(async () => ({ id: item.id })),
  };
}
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("accepted design implemented with existing contracts", () => {
  it("retains a dirty context across tabs and retains its original expectedRevision after refresh", async () => {
    const user = userEvent.setup();
    const c = commands();
    const { rerender } = render(<ItemPanel item={item} {...c} />);
    await user.click(
      screen.getByRole("button", { name: "规划与方案", exact: true }),
    );
    const plan = screen.getByLabelText("规划", { exact: true });
    await user.clear(plan);
    await user.type(plan, "本地草稿");
    await user.click(
      screen.getByRole("button", { name: "IoT 范围", exact: true }),
    );
    await user.click(
      screen.getByRole("button", { name: "规划与方案", exact: true }),
    );
    expect(plan).toHaveValue("本地草稿");
    rerender(
      <ItemPanel item={{ ...item, revision: 8, plan: "并发更新" }} {...c} />,
    );
    expect(await screen.findByText(/草稿仍基于/)).toBeInTheDocument();
    expect(plan).toHaveValue("本地草稿");
    await user.click(screen.getByRole("button", { name: "保存交付上下文" }));
    expect(c.onUpdateContext).toHaveBeenCalledWith(
      item.id,
      expect.objectContaining({ plan: "本地草稿" }),
      7,
    );
  });
  it("submits only the current section and preserves non-depends_on relations", async () => {
    const c = commands();
    const user = userEvent.setup();
    render(<ItemPanel item={item} {...c} />);
    await user.click(
      screen.getByRole("button", { name: "排期与依赖", exact: true }),
    );
    await user.type(
      screen.getByLabelText("依赖事项 ID（逗号分隔）"),
      "IOT-test-2",
    );
    await user.click(screen.getByRole("button", { name: "保存排期与关联" }));
    expect(c.onUpdateItem).toHaveBeenCalledWith(
      item.id,
      expect.objectContaining({
        dependencies: [
          { itemId: "other", relation: "related" },
          { itemId: "IOT-test-2", relation: "depends_on" },
        ],
      }),
      7,
    );
    expect(c.onUpdateItem.mock.calls[0]?.[1]).not.toHaveProperty("iotBindings");
    expect(c.onUpdateItem.mock.calls[0]?.[1]).not.toHaveProperty("traceLinks");
  });
  it("rejects self dependencies without sending a mutation", async () => {
    const c = commands();
    const user = userEvent.setup();
    render(<ItemPanel item={item} {...c} />);
    await user.click(
      screen.getByRole("button", { name: "排期与依赖", exact: true }),
    );
    await user.type(screen.getByLabelText("依赖事项 ID（逗号分隔）"), item.id);
    await user.click(screen.getByRole("button", { name: "保存排期与关联" }));
    expect(
      await screen.findByText(/依赖不能包含当前事项自身/),
    ).toBeInTheDocument();
    expect(c.onUpdateItem).not.toHaveBeenCalled();
  });
  it("does not discard an incomplete IoT row silently", async () => {
    const c = commands();
    const user = userEvent.setup();
    render(<ItemPanel item={item} {...c} />);
    await user.click(
      screen.getByRole("button", { name: "IoT 范围", exact: true }),
    );
    fireEvent.change(
      screen.getByLabelText("每行：类型 | 引用 | 标签 | JSON 属性"),
      { target: { value: "device |" } },
    );
    await user.click(screen.getByRole("button", { name: "保存 IoT 范围" }));
    expect(
      await screen.findByText(/第 1 行缺少类型或引用/),
    ).toBeInTheDocument();
    expect(c.onUpdateItem).not.toHaveBeenCalled();
  });
  it("blocks gate advancement for a blocked item and keeps comparison mode read-only", async () => {
    const c = commands();
    const user = userEvent.setup();
    render(
      <ItemPanel
        item={{ ...item, status: "blocked", blocker: "测试机未就绪" }}
        {...c}
        readOnly
      />,
    );
    expect(
      screen.getByRole("button", { name: "受阻：不可推进" }),
    ).toBeDisabled();
    await user.click(
      screen.getByRole("button", { name: "规划与方案", exact: true }),
    );
    expect(screen.getByLabelText("规划", { exact: true })).toBeDisabled();
    expect(c.onAdvance).not.toHaveBeenCalled();
  });
  it("passes a peek gate action into the expanded view rather than losing the destination", async () => {
    const c = commands();
    const open = vi.fn();
    const user = userEvent.setup();
    render(<ItemPanel item={item} {...c} compact onExpand={open} />);
    await user.click(screen.getByRole("button", { name: "提交研发完成证据" }));
    expect(open).toHaveBeenCalledWith("gate");
  });
  it("preserves input on a failed mutation rather than clearing the form", async () => {
    const c = commands();
    c.onUpdateContext = vi.fn(async () => {
      throw new Error("server conflict");
    });
    const user = userEvent.setup();
    render(<ItemPanel item={item} {...c} />);
    await user.click(
      screen.getByRole("button", { name: "规划与方案", exact: true }),
    );
    await user.type(
      screen.getByLabelText("规划", { exact: true }),
      " 本地修订",
    );
    await user.click(screen.getByRole("button", { name: "保存交付上下文" }));
    expect(await screen.findByText("server conflict")).toBeInTheDocument();
    expect(screen.getByLabelText("规划", { exact: true })).toHaveValue(
      "原规划 本地修订",
    );
  });
  it("requires explicit similarity confirmation and submits the inspected payload", async () => {
    const user = userEvent.setup();
    const create = vi.fn(async () => ({ id: "created" }));
    const similar = vi.fn(async () => [
      { id: "existing", title: "已有连接回归", score: 0.87 },
    ]);
    const close = vi.fn();
    render(
      <CreateItemDialog
        onCreate={create}
        onCheckSimilar={similar}
        onClose={close}
      />,
    );
    await user.type(screen.getByLabelText("事项名称"), "连接回归");
    await user.type(screen.getByLabelText("负责人", { exact: true }), "工程师");
    const form = screen.getByRole("form", { name: "新建交付事项" });
    fireEvent.submit(form);
    await user.click(
      screen.getByRole("button", { name: "检查相似事项并创建" }),
    );
    expect(await screen.findByText("发现 1 条相似事项")).toBeInTheDocument();
    expect(create).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "已核对，仍然创建" }));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(create.mock.calls[0]?.[0]).toEqual(similar.mock.calls[0]?.[0]);
  });
  it("keeps all four proposed settings informational without write controls", () => {
    for (const view of [
      "rules",
      "obsidian",
      "notifications",
      "runtime",
    ] as const) {
      const { unmount } = render(<PolicyReference view={view} />);
      expect(screen.queryByRole("switch")).not.toBeInTheDocument();
      expect(screen.queryByRole("button")).not.toBeInTheDocument();
      expect(screen.getByText("能力说明，不是实时配置")).toBeInTheDocument();
      unmount();
    }
  });
  it("renders backend project progress separately from task completion and capacity thresholds", async () => {
    const user = userEvent.setup();
    render(
      <ProjectWorkspace
        activeProjectId="P1"
        projects={[
          { id: "P1", name: "连接项目", owner: "工程师", board: item.board },
        ]}
        items={[item]}
        planning={{ releases: [], sprints: [], milestones: [] }}
        progress={{ progressPercent: 44, totalItems: 6, completedItems: 2 }}
        schedule={{
          scheduledItems: 4,
          totalItems: 6,
          overdueItems: 1,
          blockedItems: 1,
          capacity: [],
          risks: [],
        }}
        onSelectProject={vi.fn()}
        onCreateProject={vi.fn()}
        onCreateRelease={vi.fn()}
        onCreateSprint={vi.fn()}
        onCreateMilestone={vi.fn()}
      />,
    );
    await user.click(
      screen.getByRole("button", { name: "交付健康", exact: true }),
    );
    expect(screen.getByText("44%")).toBeInTheDocument();
    expect(screen.getByText("4 / 6")).toBeInTheDocument();
    expect(screen.getByText("2 / 6 项完成")).toBeInTheDocument();
    expect(screen.getByText(/未配置容量阈值/)).toBeInTheDocument();
  });
  it("does not convert javascript or relative references into executable links", () => {
    expect(safeReference("javascript:alert(1)")).toBeUndefined();
    expect(safeReference("/admin")).toBeUndefined();
    expect(safeReference("https://example.test/report")).toBe(
      "https://example.test/report",
    );
  });
});
