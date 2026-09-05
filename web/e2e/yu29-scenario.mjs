import { launchBrowser } from "./cdp-browser.mjs";
import { assert, browserRequest, expectStatus, localCookies, loginThroughUI } from "./browser-actions.mjs";
import { certifyAuthorizationStage } from "./yu29-authorization-stage.mjs";
import { certifySessionLifecycleStage } from "./yu29-session-stage.mjs";

export async function runYU29Scenario({ fixture, webBase }) {
  const browser = await launchBrowser(process.env);
  const admin = await browser.createContext();
  const member = await browser.createContext();
  try {
    await loginThroughUI(admin, webBase, fixture.organizationId, fixture.adminUserId, fixture.adminPassword, "YU-29 System Administrator");
    await loginThroughUI(member, webBase, fixture.organizationId, fixture.memberUserId, fixture.memberPassword, "YU-29 Ordinary Member");

    const adminCookies = await localCookies(admin);
    const memberCookies = await localCookies(member);
    assert(adminCookies.session && memberCookies.session && adminCookies.session !== memberCookies.session, "independent browser contexts must not share session cookies");
    assert(adminCookies.csrf && memberCookies.csrf && adminCookies.csrf !== memberCookies.csrf, "independent browser contexts must not share CSRF cookies");

    await certifyAuthorizationStage({ admin, member, fixture, adminCSRF: adminCookies.csrf });
    await certifySessionLifecycleStage({ admin, member, fixture, webBase });

    expectStatus(await browserRequest(admin, "/auth/local/current", { csrf: false }), 200, "unaffected administrator remains authenticated");
  } finally {
    await member.close();
    await admin.close();
    await browser.close();
  }
}
