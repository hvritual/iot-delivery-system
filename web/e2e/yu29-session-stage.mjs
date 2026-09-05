import { assert, browserRequest, expectStatus, loginThroughUI } from "./browser-actions.mjs";

const resetPassword = "YU29-reset-password-01";
const selfPassword = "YU29-self-password-02";

export async function certifySessionLifecycleStage({ admin, member, fixture, webBase }) {
  const reset = expectStatus(await browserRequest(admin, `/auth/local/admin/members/${encodeURIComponent(fixture.memberUserId)}/reset-credential`, {
    method: "POST",
    body: {
      expectedUserRevision: fixture.memberUserRevision,
      expectedCredentialRevision: fixture.memberCredentialRevision,
      password: resetPassword,
    },
  }), 200, "administrator resets member credential").body;
  const resetUserRevision = reset?.UserRevision ?? reset?.userRevision;
  assert(Number.isInteger(resetUserRevision) && resetUserRevision > fixture.memberUserRevision, "credential reset must advance user revision");
  expectStatus(await browserRequest(member, "/auth/local/current", { csrf: false }), 401, "credential reset invalidates old member session");
  expectStatus(await browserRequest(admin, "/auth/local/current", { csrf: false }), 200, "credential reset leaves administrator valid");

  await loginThroughUI(member, webBase, fixture.organizationId, fixture.memberUserId, resetPassword, "YU-29 Ordinary Member");
  const changed = expectStatus(await browserRequest(member, "/auth/local/change-password", {
    method: "POST",
    body: { currentPassword: resetPassword, newPassword: selfPassword },
  }), 200, "member changes own password").body;
  const changedUserRevision = changed?.userRevision ?? changed?.UserRevision;
  assert(Number.isInteger(changedUserRevision) && changedUserRevision > resetUserRevision, "self password change must advance user revision");
  expectStatus(await browserRequest(member, "/auth/local/current", { csrf: false }), 401, "self password change invalidates old member session");
  expectStatus(await browserRequest(admin, "/auth/local/current", { csrf: false }), 200, "self password change leaves administrator valid");

  await loginThroughUI(member, webBase, fixture.organizationId, fixture.memberUserId, selfPassword, "YU-29 Ordinary Member");
  expectStatus(await browserRequest(admin, `/auth/local/admin/members/${encodeURIComponent(fixture.memberUserId)}/disable`, {
    method: "POST",
    body: { expectedRevision: changedUserRevision },
  }), 200, "administrator disables member");
  expectStatus(await browserRequest(member, "/auth/local/current", { csrf: false }), 401, "member disable invalidates existing browser session");
  expectStatus(await browserRequest(admin, "/auth/local/current", { csrf: false }), 200, "member disable leaves administrator valid");

  expectStatus(await browserRequest(member, "/auth/local/login", {
    method: "POST",
    body: { organizationId: fixture.organizationId, userId: fixture.memberUserId, password: selfPassword },
    csrf: false,
  }), 401, "disabled member cannot create a new session");
}
