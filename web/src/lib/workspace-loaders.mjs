import { loadR2Workspace } from "./r2-capability.mjs";

// Planning list contracts are project-scoped. An empty projectId is not an
// organization-wide query, even for an administrator. Resolve the authorized
// catalog first and never manufacture a project or retry with broader scope.
export async function loadAuthorizedWorkspace(loaders) {
  const catalog = await loadR2Workspace({
    projects: { load: loaders.projects, fallback: [] },
  });
  const projects = catalog.values.projects;
  if (!Array.isArray(projects)) throw new TypeError("项目列表响应格式不正确。");
  const forProjects = (load) => async () => {
    const batches = await Promise.all(projects.map((project) => load(project.id)));
    if (batches.some((items) => !Array.isArray(items))) {
      throw new TypeError("项目排期响应格式不正确。");
    }
    return batches.flat();
  };
  const result = await loadR2Workspace({
    releases: { load: forProjects(loaders.releases), fallback: [] },
    sprints: { load: forProjects(loaders.sprints), fallback: [] },
    milestones: { load: forProjects(loaders.milestones), fallback: [] },
    views: { load: loaders.views, fallback: [] },
    inbox: { load: loaders.inbox, fallback: [] },
  });
  return {
    available: catalog.available && result.available,
    values: {
      projects,
      releases: result.values.releases,
      sprints: result.values.sprints,
      milestones: result.values.milestones,
      views: result.values.views,
      inbox: result.values.inbox,
    },
  };
}

// A project member may read assigned work without permission to read the
// organization cockpit. Both requests retain server authorization; this is a
// separately labelled projection, not a fabricated successful dashboard.
export async function loadAuthorizedDashboard(loaders) {
  try {
    return { scope: "organization", value: await loaders.dashboard() };
  } catch (error) {
    if (error?.status !== 403) throw error;
    const items = await loaders.items();
    if (!Array.isArray(items)) throw new TypeError("事项列表响应格式不正确。");
    return {
      scope: "project",
      value: {
        items,
        boards: loaders.boards.map((board) => ({
          board,
          total: items.filter((item) => item.board === board).length,
        })),
        generatedAt: null,
      },
    };
  }
}
