import { describe, it, expect, vi } from "vitest";
import { loadAuthorizedWorkspace, loadAuthorizedDashboard } from "../src/lib/workspace-loaders.mjs";

const forbidden = Object.assign(new Error("permission_denied"), { status: 403 });
function loaders(projects = [{ id: "project-a" }, { id: "project-b" }]) {
  return {
    projects: vi.fn(async () => projects),
    releases: vi.fn(async (id: string) => [{ projectId: id, id: `rel-${id}` }]),
    sprints: vi.fn(async (id: string) => [{ projectId: id, id: `sprint-${id}` }]),
    milestones: vi.fn(async (id: string) => [{ projectId: id, id: `ms-${id}` }]),
    views: vi.fn(async () => []), inbox: vi.fn(async () => []),
  };
}
describe("server-scoped workspace reads", () => {
  it("uses only returned project IDs for every planning read", async () => {
    const source = loaders();
    const result = await loadAuthorizedWorkspace(source);
    expect(result.available).toBe(true);
    for (const load of [source.releases, source.sprints, source.milestones]) {
      expect(load.mock.calls).toEqual([["project-a"], ["project-b"]]);
    }
    expect(result.values.releases).toHaveLength(2);
  });
  it("never sends an empty projectId for an empty authorized catalog", async () => {
    const source = loaders([]);
    expect((await loadAuthorizedWorkspace(source)).values.releases).toEqual([]);
    expect(source.releases).not.toHaveBeenCalled();
    expect(source.sprints).not.toHaveBeenCalled();
  });
  it("retains old-backend detection but never downgrades authorization failures", async () => {
    const source = loaders();
    source.projects.mockRejectedValueOnce(Object.assign(new Error("missing"), { status: 404 }));
    expect((await loadAuthorizedWorkspace(source)).available).toBe(false);
    source.projects.mockRejectedValueOnce(forbidden);
    await expect(loadAuthorizedWorkspace(source)).rejects.toBe(forbidden);
  });
  it("separately labels a server-filtered item projection, never an organization dashboard", async () => {
    const source = { dashboard: vi.fn().mockRejectedValue(forbidden), items: vi.fn(async () => [{ id: "allowed", board: "设备质量与连接" }]), boards: ["设备质量与连接"] };
    const result = await loadAuthorizedDashboard(source);
    expect(result.scope).toBe("project");
    expect(result.value.boards).toEqual([{ board: "设备质量与连接", total: 1 }]);
    expect(result.value.generatedAt).toBeNull();
  });
  it("does not downgrade 401, 503, or denied item reads into mock/empty success", async () => {
    for (const status of [401, 503]) {
      const cause = Object.assign(new Error("unavailable"), { status });
      const source = { dashboard: vi.fn().mockRejectedValue(cause), items: vi.fn(), boards: [] };
      await expect(loadAuthorizedDashboard(source)).rejects.toBe(cause);
      expect(source.items).not.toHaveBeenCalled();
    }
    await expect(loadAuthorizedDashboard({ dashboard: vi.fn().mockRejectedValue(forbidden), items: vi.fn().mockRejectedValue(forbidden), boards: [] })).rejects.toBe(forbidden);
  });
});
