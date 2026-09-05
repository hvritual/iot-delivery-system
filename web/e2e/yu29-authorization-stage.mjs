import { assert, browserRequest, expectStatus } from "./browser-actions.mjs";

export async function certifyAuthorizationStage({ admin, member, fixture, adminCSRF }) {
  const bound = expectStatus(await browserRequest(admin, "/api/projects", {
    method: "POST",
    body: { name: "YU-29 Bound Project", board: "研发交付效能", owner: fixture.adminUserId, description: "two-browser authorization" },
  }), 201, "create bound project").body;
  const unbound = expectStatus(await browserRequest(admin, "/api/projects", {
    method: "POST",
    body: { name: "YU-29 Unbound Project", board: "产品与平台能力", owner: fixture.adminUserId, description: "outside member scope" },
  }), 201, "create unbound project").body;
  assert(bound?.id && unbound?.id && bound.id !== unbound.id, "two distinct projects are required");

  const assigned = expectStatus(await browserRequest(admin, "/auth/local/admin/project-role-bindings", {
    method: "POST",
    body: { projectId: bound.id, userId: fixture.memberUserId, roleId: "release-approver" },
  }), 201, "assign project reviewer role").body;
  const bindingId = assigned?.BindingID ?? assigned?.bindingId;
  const bindingRevision = assigned?.Revision ?? assigned?.revision;
  assert(bindingId && bindingRevision === 1, "new RoleBinding must start at revision 1");

  const list = expectStatus(await browserRequest(member, "/api/projects"), 200, "member lists authorized projects").body;
  assert(Array.isArray(list), "project list must be an array");
  assert(list.some((project) => project.id === bound.id), "bound project must be visible");
  assert(!list.some((project) => project.id === unbound.id), "unbound project must remain invisible");

  const denialPath = `/auth/local/admin/members/${encodeURIComponent(fixture.adminUserId)}/disable`;
  expectStatus(await browserRequest(member, denialPath, { method: "POST", body: { expectedRevision: 1 } }), 403, "ordinary member cannot perform member administration");

  await certifySOD(admin, member, fixture.adminUserId, bound.id, adminCSRF);

  expectStatus(await browserRequest(admin, `/auth/local/admin/project-role-bindings/${encodeURIComponent(bindingId)}/revoke`, {
    method: "POST",
    body: { expectedRevision: bindingRevision },
  }), 200, "revoke project reviewer role");
  expectStatus(await browserRequest(member, "/auth/local/current", { csrf: false }), 200, "role revoke keeps authentication valid");
  expectStatus(await browserRequest(member, "/api/projects"), 403, "role revoke removes authorization on next protected request");
}

async function certifySOD(admin, reviewer, adminUserId, projectId, adminCSRF) {
  const implementerIdentity = expectStatus(await browserRequest(admin, "/auth/local/current"), 200, "verify implementer identity").body;
  const reviewerIdentity = expectStatus(await browserRequest(reviewer, "/auth/local/current"), 200, "verify reviewer identity").body;
  assert(implementerIdentity.userId === adminUserId && reviewerIdentity.userId && reviewerIdentity.userId !== adminUserId && implementerIdentity.organizationId === reviewerIdentity.organizationId, "SOD must use different server-verified users in the same organization");
  const created = expectStatus(await browserRequest(admin, "/api/items", {
    method: "POST",
    body: { title: "YU-29 independent production validation", board: "研发交付效能", projectId, kind: "task", type: "task", owner: adminUserId, priority: "P1", progressPercent: 0 },
  }), 201, "create SOD work item").body;
  let revision = created.revision;
  for (const gate of ["solution_reviewed", "development_completed", "test_passed"]) {
    const advanced = expectStatus(await browserRequest(admin, `/api/items/${encodeURIComponent(created.id)}/gates/${gate}`, {
      method: "POST",
      body: { expectedRevision: revision, evidence: [{ kind: "e2e", title: `YU-29 ${gate}` }] },
    }), 200, `advance to ${gate}`).body;
    revision = advanced.revision;
  }

  const rejected = await browserRequest(admin, `/api/items/${encodeURIComponent(created.id)}/gates/production_validated`, {
    method: "POST",
    body: { expectedRevision: revision, evidence: [{ kind: "e2e", title: "same implementer" }] },
  });
  expectStatus(rejected, 403, "implementer cannot validate own change");
  assert(rejected.phase === "request" && rejected.body?.error === "permission_denied", "SOD denial must use the runtime authorization contract, not CSRF or availability failure");
  const unchanged = expectStatus(await browserRequest(admin, `/api/items/${encodeURIComponent(created.id)}`), 200, "read rejected SOD item").body;
  assert(unchanged.gate === "test_passed" && unchanged.revision === revision, "rejected validation must have zero business mutation");

  // Hold identity, grant, path, body and revision constant; vary only CSRF.
  const reviewPath = `/api/items/${encodeURIComponent(created.id)}/gates/production_validated`;
  const reviewBody = { expectedRevision: revision, evidence: [{ kind: "e2e", title: "independent reviewer" }] };
  const wrongCSRF = expectStatus(await browserRequest(reviewer, reviewPath, { method: "POST", body: reviewBody, csrfToken: adminCSRF }), 403, "reject other context CSRF on an otherwise authorized operation");
  assert(wrongCSRF.body?.error === "forbidden", "CSRF must be rejected at the browser boundary");
  const afterCSRF = expectStatus(await browserRequest(admin, `/api/items/${encodeURIComponent(created.id)}`), 200, "read after CSRF rejection").body;
  assert(afterCSRF.revision === revision && afterCSRF.gate === "test_passed", "CSRF rejection must not mutate work item");

  const validated = expectStatus(await browserRequest(reviewer, `/api/items/${encodeURIComponent(created.id)}/gates/production_validated`, {
    method: "POST",
    body: { expectedRevision: revision, evidence: [{ kind: "e2e", title: "independent reviewer" }] },
  }), 200, "independent reviewer validates production").body;
  assert(validated.gate === "production_validated" && validated.revision === revision + 1, "different browser identity must complete exactly one production review");
}
