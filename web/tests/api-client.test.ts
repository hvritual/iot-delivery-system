import { afterEach, describe, expect, it, vi } from "vitest";

import { fetchProjects } from "@/src/api.js";

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
});
