import { describe, expect, it } from "vitest";

import { loadR2Workspace } from "@/src/lib/r2-capability.mjs";

describe("R2 workspace capability loading", () => {
  it("falls back to empty R2 data only when a comparison backend does not expose the R2 endpoints", async () => {
    const unavailable = Object.assign(new Error("endpoint not found"), { status: 404 });
    const result = await loadR2Workspace({
      projects: { load: async () => { throw unavailable; }, fallback: [] },
      notifications: { load: async () => [{ deliveryId: "event-001" }], fallback: [] },
    });

    expect(result.available).toBe(false);
    expect(result.values).toEqual({ projects: [], notifications: [{ deliveryId: "event-001" }] });
  });

  it("keeps operational failures visible instead of hiding them as an unsupported capability", async () => {
    const operationalFailure = Object.assign(new Error("database unavailable"), { status: 500 });

    await expect(loadR2Workspace({
      projects: { load: async () => { throw operationalFailure; }, fallback: [] },
    })).rejects.toBe(operationalFailure);
  });
});
