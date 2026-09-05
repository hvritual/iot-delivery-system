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
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 404,
      headers: { get: () => "application/json" },
      json: async () => ({ error: "R2 endpoint not available" }),
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchProjects()).rejects.toMatchObject({
      message: "R2 endpoint not available",
      status: 404,
    });
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/projects");
  });

  it("obtains the current verified-session CSRF token before every unsafe mutation", async () => {
    const csrfToken = "csrf_current_session_token_1234567890ABCDEFG";
    const fetchMock = vi.fn(async (path: string) => {
      if (path === "/auth/session") {
        return {
          ok: true,
          status: 200,
          headers: { get: () => "application/json" },
          json: async () => ({ authenticated: true, csrfToken }),
        };
      }
      return {
        ok: true,
        status: 200,
        headers: { get: () => "application/json" },
        json: async () => ({}),
      };
    });
    vi.stubGlobal("fetch", fetchMock);

    await updateWorkItem("delivery-1", 7, { title: "更新后的标题" });
    await updateItemContext("delivery-1", 8, { plan: "更新后的计划" });
    await addComment("delivery-1", 9, "补充评论");
    await advanceGate("delivery-1", 10, "solution_reviewed", "评审通过");
    await closeItem("delivery-1", 11, "复盘完成");

    const csrfCalls = fetchMock.mock.calls.filter(([path]) => path === "/auth/session");
    expect(csrfCalls).toHaveLength(5);
    for (const [, options] of csrfCalls) {
      expect(options).toMatchObject({ method: "GET", credentials: "same-origin", cache: "no-store" });
    }

    const apiCalls = fetchMock.mock.calls.filter(([path]) => String(path).startsWith("/api/"));
    expect(apiCalls).toHaveLength(5);
    expect(apiCalls[0]?.[0]).toBe("/api/items/delivery-1");
    expect(apiCalls[0]?.[1]).toEqual(expect.objectContaining({
      body: JSON.stringify({ title: "更新后的标题", expectedRevision: 7 }),
    }));
    expect(apiCalls[1]?.[1]).toEqual(expect.objectContaining({
      body: JSON.stringify({ plan: "更新后的计划", expectedRevision: 8 }),
    }));
    expect(apiCalls[2]?.[1]).toEqual(expect.objectContaining({
      body: JSON.stringify({ body: "补充评论", expectedRevision: 9 }),
    }));
    expect(apiCalls[3]?.[1]).toEqual(expect.objectContaining({
      body: JSON.stringify({
        expectedRevision: 10,
        evidence: [{ kind: "交付证据", title: "评审通过" }],
      }),
    }));
    expect(apiCalls[4]?.[1]).toEqual(expect.objectContaining({
      body: JSON.stringify({ retrospective: "复盘完成", expectedRevision: 11 }),
    }));
    for (const [, options] of apiCalls) {
      const headers = options?.headers as Headers;
      expect(headers.get("x-csrf-token")).toBe(csrfToken);
      expect(options).toEqual(expect.objectContaining({ credentials: "same-origin" }));
    }
  });

  it("does not fetch or require a CSRF token for safe API methods", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: () => "application/json" },
      json: async () => ([]),
    }));
    vi.stubGlobal("fetch", fetchMock);

    await fetchProjects();

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/projects");
    const headers = fetchMock.mock.calls[0]?.[1]?.headers as Headers;
    expect(headers.get("x-csrf-token")).toBeNull();
  });

  it("fails before the mutation when the current session cannot provide CSRF", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 401,
      headers: { get: () => "application/json" },
      json: async () => ({ error: "unauthenticated" }),
    }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(updateWorkItem("delivery-1", 7, { title: "must not send" })).rejects.toMatchObject({
      message: "unauthenticated",
      status: 401,
    });
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBe("/auth/session");
  });
});
