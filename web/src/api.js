const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

async function request(path, options = {}) {
  const method = String(options.method ?? "GET").toUpperCase();
  const headers = new Headers(options.headers ?? {});
  headers.set("Accept", "application/json");
  if (options.body) headers.set("Content-Type", "application/json");
  if (!SAFE_METHODS.has(method)) {
    headers.set("X-CSRF-Token", await currentCSRFToken());
  }
  const { headers: _ignoredHeaders, ...requestOptions } = options;
  const response = await fetch(path, {
    ...requestOptions,
    method,
    headers,
    credentials: "same-origin",
  });
  const payload = await responsePayload(response);
  if (!response.ok) throw requestError(response, payload);
  return payload;
}

async function currentCSRFToken() {
  const response = await fetch("/auth/session", {
    method: "GET",
    headers: { Accept: "application/json" },
    credentials: "same-origin",
    cache: "no-store",
  });
  const payload = await responsePayload(response);
  if (!response.ok) throw requestError(response, payload);
  const token = payload?.csrfToken;
  if (typeof token !== "string" || !/^[A-Za-z0-9_-]{32,}$/.test(token)) {
    const error = new Error("会话校验失败");
    error.status = 401;
    throw error;
  }
  return token;
}

async function responsePayload(response) {
  const contentType = response.headers.get("content-type") ?? "";
  return contentType.includes("application/json") ? response.json() : null;
}

function requestError(response, payload) {
  const error = new Error(payload?.error || `请求失败（${response.status}）`);
  error.status = response.status;
  return error;
}

export function fetchDashboard() {
  return request("/api/dashboard");
}

export function createItem(input) {
  return request("/api/items", { method: "POST", body: JSON.stringify(input) });
}

export function findSimilar(input) {
  return request("/api/items/similarity", { method: "POST", body: JSON.stringify(input) });
}

export function updateWorkItem(id, expectedRevision, input) {
  return request(`/api/items/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify({ ...input, expectedRevision }),
  });
}

export function addComment(id, expectedRevision, body) {
  return request(`/api/items/${encodeURIComponent(id)}/comments`, {
    method: "POST",
    body: JSON.stringify({ body, expectedRevision }),
  });
}

export function fetchProjects() {
  return request("/api/projects");
}

export function createProject(input) {
  return request("/api/projects", { method: "POST", body: JSON.stringify(input) });
}

export function fetchReleases(projectId = "") {
  return request(withQuery("/api/releases", { projectId }));
}

export function createRelease(input) {
  return request("/api/releases", { method: "POST", body: JSON.stringify(input) });
}

export function fetchSprints(projectId = "") {
  return request(withQuery("/api/sprints", { projectId }));
}

export function createSprint(input) {
  return request("/api/sprints", { method: "POST", body: JSON.stringify(input) });
}

export function fetchMilestones(projectId = "") {
  return request(withQuery("/api/milestones", { projectId }));
}

export function createMilestone(input) {
  return request("/api/milestones", { method: "POST", body: JSON.stringify(input) });
}

export function fetchSavedViews() {
  return request("/api/views");
}

export function saveView(input) {
  return request("/api/views", { method: "POST", body: JSON.stringify(input) });
}

export function fetchMemberWeek(member, weekStart = "") {
  return request(withQuery("/api/member-week", { member, weekStart }));
}

export function fetchProjectProgress(projectId) {
  return request(`/api/projects/${encodeURIComponent(projectId)}/progress`);
}

export function fetchProjectSchedule(projectId) {
  return request(`/api/projects/${encodeURIComponent(projectId)}/schedule`);
}

export function fetchNotifications(limit = 20) {
  return request(withQuery("/api/notifications", { limit }));
}

export function updateItemContext(id, expectedRevision, input) {
  return request(`/api/items/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify({ ...input, expectedRevision }),
  });
}

export function advanceGate(id, expectedRevision, gate, evidence) {
  const record = typeof evidence === "string" ? { title: evidence } : evidence;
  return request(`/api/items/${encodeURIComponent(id)}/gates/${encodeURIComponent(gate)}`, {
    method: "POST",
    body: JSON.stringify({
      expectedRevision,
      evidence: [{
        kind: "交付证据",
        title: record?.title ?? "",
        ...(record?.reference?.trim() ? { reference: record.reference.trim() } : {}),
      }],
    }),
  });
}

export function closeItem(id, expectedRevision, retrospective) {
  return request(`/api/items/${encodeURIComponent(id)}/close`, {
    method: "POST",
    body: JSON.stringify({ retrospective, expectedRevision }),
  });
}

function withQuery(path, values) {
  const query = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value !== undefined && value !== null && String(value).trim() !== "") query.set(key, String(value));
  });
  const serialized = query.toString();
  return serialized ? `${path}?${serialized}` : path;
}
