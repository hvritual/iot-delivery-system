import { afterEach, describe, expect, it, vi } from "vitest";

import {
  addComment,
  advanceGate,
  closeItem,
  fetchProjects,
  updateItemContext,
  updateWorkItem,
} from "@/src/api.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("delivery API client", () => {
  it("preserves an HTTP status on request errors so capability fallbacks are explicit", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => ({
      ok: false,
      status: 404,
      headers: { get: () => "application/json" },
      json: async () => ({ error: "R2 endpoint not available" }),
    })));

    await expect(fetchProjects()).rejects.toMatchObject({
      message: "R2 endpoint not available",
      status: 404,
    });
  });

  it("sends the current expected revision for every work item mutation", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: () => "application/json" },
      json: async () => ({}),
    }));
    vi.stubGlobal("fetch", fetchMock);

    await updateWorkItem("delivery-1", 7, { title: "更新后的标题" });
    await updateItemContext("delivery-1", 8, { plan: "更新后的计划" });
    await addComment("delivery-1", 9, "补充评论");
    await advanceGate("delivery-1", 10, "solution_reviewed", "评审通过");
    await closeItem("delivery-1", 11, "复盘完成");

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/api/items/delivery-1", expect.objectContaining({
      body: JSON.stringify({ title: "更新后的标题", expectedRevision: 7 }),
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/items/delivery-1", expect.objectContaining({
      body: JSON.stringify({ plan: "更新后的计划", expectedRevision: 8 }),
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/api/items/delivery-1/comments", expect.objectContaining({
      body: JSON.stringify({ body: "补充评论", expectedRevision: 9 }),
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(4, "/api/items/delivery-1/gates/solution_reviewed", expect.objectContaining({
      body: JSON.stringify({
        expectedRevision: 10,
        evidence: [{ kind: "交付证据", title: "评审通过" }],
      }),
    }));
    expect(fetchMock).toHaveBeenNthCalledWith(5, "/api/items/delivery-1/close", expect.objectContaining({
      body: JSON.stringify({ retrospective: "复盘完成", expectedRevision: 11 }),
    }));
  });
});
