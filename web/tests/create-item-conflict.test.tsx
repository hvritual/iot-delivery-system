// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CreateItemDialog } from "@/components/delivery/create-item";
import { Failure } from "@/components/delivery/ui";
import type { CreateCommand } from "@/components/delivery/model";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });
const duplicate = () => Object.assign(new Error("duplicate delivery work item: IOT-existing"), {
  status: 409, traceId: "trace-test-create-conflict",
});
const matches = [{ id: "IOT-existing", title: "连接回归", score: 0.85 }];

async function prepare(onCreate: CreateCommand, similar = vi.fn(async (_input: Record<string, unknown>) => matches)) {
  const onClose = vi.fn();
  const user = userEvent.setup();
  render(<CreateItemDialog onCreate={onCreate} onCheckSimilar={similar} onClose={onClose} />);
  fireEvent.change(screen.getByLabelText("事项名称"), { target: { value: "连接回归" } });
  fireEvent.change(screen.getByLabelText("负责人", { exact: true }), { target: { value: "工程师" } });
  fireEvent.submit(screen.getByRole("form", { name: "新建交付事项" }));
  fireEvent.change(screen.getByLabelText("规划", { exact: true }), { target: { value: "需要保留的规划" } });
  await user.click(screen.getByRole("button", { name: "检查相似事项并创建" }));
  await screen.findByText("发现 1 条相似事项");
  return { onClose, user, similar };
}

describe("creation follows exact-duplicate and similarity contracts", () => {
  it("shows the real 409 and trace inside the modal, retaining the draft without reporting success", async () => {
    const create = vi.fn(async () => { throw duplicate(); });
    const { user, onClose } = await prepare(create);
    await user.click(screen.getByRole("button", { name: "已核对，仍然创建" }));
    expect(await screen.findByText("事项重复，未创建")).toBeInTheDocument();
    expect(screen.getByText(/HTTP 409/)).toBeInTheDocument();
    expect(screen.getByText(/Trace ID: trace-test-create-conflict/)).toBeInTheDocument();
    expect(screen.queryByText("数据版本冲突")).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    expect(create).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "基本信息", exact: true }));
    expect(screen.getByLabelText("事项名称")).toHaveValue("连接回归");
    expect(screen.getByLabelText("负责人", { exact: true })).toHaveValue("工程师");
    fireEvent.submit(screen.getByRole("form", { name: "新建交付事项" }));
    expect(screen.getByLabelText("规划", { exact: true })).toHaveValue("需要保留的规划");
  });

  it("invalidates the rejected snapshot after editing and creates the newly checked payload only once", async () => {
    const create = vi.fn<CreateCommand>().mockRejectedValueOnce(duplicate()).mockResolvedValueOnce({ id: "IOT-new" });
    const { user, onClose, similar } = await prepare(create);
    await user.click(screen.getByRole("button", { name: "已核对，仍然创建" }));
    await screen.findByText("事项重复，未创建");
    await user.click(screen.getByRole("button", { name: "基本信息", exact: true }));
    fireEvent.change(screen.getByLabelText("事项名称"), { target: { value: "连接回归补充" } });
    expect(screen.queryByText("事项重复，未创建")).not.toBeInTheDocument();
    fireEvent.submit(screen.getByRole("form", { name: "新建交付事项" }));
    await user.click(screen.getByRole("button", { name: "检查相似事项并创建" }));
    await screen.findByText("发现 1 条相似事项");
    expect(similar).toHaveBeenCalledTimes(2);
    expect(create).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "已核对，仍然创建" }));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(create).toHaveBeenCalledTimes(2);
    expect(create.mock.calls[1][0]).toEqual(similar.mock.calls[1][0]);
    expect(create.mock.calls[1][0]).toMatchObject({ title: "连接回归补充", plan: "需要保留的规划" });
    expect(create.mock.calls[1][0]).not.toHaveProperty("force");
    expect(create.mock.calls[1][0]).not.toHaveProperty("confirmSimilar");
  });

  it("locks a pending confirmed submission and closes only after the returned receipt", async () => {
    let complete!: (value: unknown) => void;
    const create = vi.fn<CreateCommand>(() => new Promise((resolve) => { complete = resolve; }));
    const { user, onClose } = await prepare(create);
    const confirm = screen.getByRole("button", { name: "已核对，仍然创建" });
    await user.click(confirm);
    expect(confirm).toBeDisabled();
    fireEvent.click(confirm);
    expect(create).toHaveBeenCalledTimes(1);
    expect(onClose).not.toHaveBeenCalled();
    await act(async () => complete({ id: "IOT-created" }));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("form submission alone cannot bypass the explicit similarity confirmation", async () => {
    const create = vi.fn(async () => ({ id: "IOT-created" }));
    const { similar, onClose } = await prepare(create);
    fireEvent.submit(screen.getByRole("form", { name: "新建交付事项" }));
    await waitFor(() => expect(similar).toHaveBeenCalledTimes(2));
    expect(create).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "已核对，仍然创建" })).toBeInTheDocument();
  });

  it("keeps an unrelated revision conflict distinguishable from duplicate rejection", () => {
    render(<Failure error={Object.assign(new Error("work item revision conflict"), { status: 409 })} />);
    expect(screen.getByText("数据版本冲突")).toBeInTheDocument();
    expect(screen.queryByText("事项重复，未创建")).not.toBeInTheDocument();
  });

  it("does not interpret a non-409 failure as permission to retry a duplicate automatically", async () => {
    const create = vi.fn(async () => { throw Object.assign(new Error("identity unavailable"), { status: 503 }); });
    const { user, onClose } = await prepare(create);
    await user.click(screen.getByRole("button", { name: "已核对，仍然创建" }));
    expect(await screen.findByText(/HTTP 503/)).toBeInTheDocument();
    expect(screen.queryByText("事项重复，未创建")).not.toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    expect(create).toHaveBeenCalledTimes(1);
  });
});
