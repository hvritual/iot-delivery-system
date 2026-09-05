# YU-29 two-real-account / two-browser-context E2E evidence

> Document class: **EVIDENCE**  
> Task: `YU-29`  
> Fixed consumer parent: `d9aa3e4b19d9bd3ff3ea569c040096d8b5eeb515`  
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`  
> Scope stop: before `YU-30`

## Result

YU-29 adds a real-browser E2E harness that is capable of starting the existing consumer stack with:

```text
real temporary SQLite
    -> one-time local administrator bootstrap
    -> YU-20 real ordinary-member creation
    -> real Yunka runtime
    -> real Next Web
    -> real Chromium-family browser
    -> BrowserContext A + BrowserContext B
```

The harness is intentionally dependency-free on Playwright/Puppeteer. It uses Chrome DevTools Protocol directly and therefore does not add a browser automation package or lockfile drift.

The canonical entry point is:

```text
cd web
npm run e2e:yu29
```

This round **does not claim the E2E passed**. The current execution container has Node, Go and Chromium, but has no repository checkout and cannot resolve `github.com`, so the canonical source could not be materialized for execution. The scenario is therefore **AUTHORED, NOT EXECUTED**.

## Fixed-parent RED review

### Confirmed RED A — no two-browser real E2E harness existed

At the fixed parent the Web package exposed:

```text
dev
build
start
test
test:watch
typecheck
```

but no E2E command, browser harness or `web/e2e` test surface.

Existing YU-28 tests are component/interaction tests with mocked `fetch`. They are useful UI regressions, but by contract they cannot prove two independent browser cookie jars against real Next + runtime + SQLite.

This is the actual YU-29 RED. Missing browser tooling in an execution environment is not treated as RED.

### Confirmed RED B — no existing test could prove cross-context invalidation/isolation

The fixed parent had no executable path that simultaneously proved:

- account A and B use separate browser contexts;
- session/CSRF cookies are not shared;
- A's CSRF cannot authorize B's context;
- B's project RoleBinding affects only the bound project;
- RoleBinding revoke changes B's next authorization decision while preserving authentication;
- admin credential reset, self password change and User disable invalidate only the affected account/session;
- the unaffected administrator context remains valid;
- real segregation of duties is enforced between two different browser identities.

No unit/component mock is reclassified as E2E evidence.

## Real fixture construction

New fixture command:

```text
backend-yunka/cmd/yu29-fixture
```

It creates a fresh temporary database and starts the real consumer `bootstrap.Application` so all existing identity/auth/audit/outbox migrations and runtime composition are present.

The only direct fixture SQL is the minimum tenant prerequisite:

```text
INSERT INTO organizations ...
```

Users, credentials and RoleBindings are **not** inserted directly.

Account A is created through:

```text
Application.AdministratorBootstrap().Initialize
```

which retains the YU-19 one-time bootstrap latch and creates the durable `system-administrator` binding.

Account B is created by:

```text
A local login
-> VerifyAccessToken
-> identity.WithPrincipal
-> Application.MemberAdministration().Create
-> YU-20 Executor / durable authorization / guard / transaction / audit / Outbox
```

This prevents the fixture from manufacturing an ordinary member by bypassing the production application boundary.

### Temporary secrets

The fixture generates random:

- local JWT HMAC key;
- BFF assertion key;
- administrator password;
- ordinary-member password.

They are written only to a temporary manifest with mode `0600`, consumed by the E2E process, and removed with the temporary workspace unless the explicit troubleshooting flag is set.

No real credential or reusable secret is committed.

## Real process ownership

`web/scripts/yu29-e2e.mjs` owns the test lifecycle.

It:

1. creates the temporary SQLite fixture;
2. builds the actual `cmd/yunka-bootstrap` runtime binary;
3. starts that binary against the same SQLite file;
4. starts Next directly through its Node CLI;
5. waits for runtime/Web readiness;
6. runs the browser scenario;
7. terminates owned services;
8. removes the temporary workspace.

The runtime is built to a temporary binary instead of leaving `go run` as a long-lived parent process. Next is started as the actual Node process rather than through a long-lived `npm run` parent. This avoids treating a killed wrapper process as proof that the real server stopped.

The CDP browser owner also waits for Chrome to exit before deleting its user-data directory, including a bounded cleanup path for Windows file locking.

## Two independent browser contexts

`web/e2e/cdp-browser.mjs` starts an installed Chromium-family browser and calls real CDP:

```text
Target.createBrowserContext   # A
Target.createBrowserContext   # B
```

Each context receives its own target, session and storage partition.

The scenario verifies both contexts have non-empty but different:

```text
__Host-iotd_local_session
__Host-iotd_local_csrf
```

values.

The harness never copies a session cookie from one context to another.

## Login path

Both accounts login through the actual YU-28 local login form in the real rendered Next page.

The browser fills:

```text
organizationId
userId
password
```

and submits the DOM form. Success is confirmed only after the local member display name is rendered and the unauthenticated login form disappears.

No OIDC configuration is needed for this local-auth path.

## Project-scope authorization

Account A creates two real projects through `/api/projects`.

A then uses the YU-28/YU-26 protected project-role route to assign only:

```text
role = release-approver
scope = project:<bound-project>
user = B
```

B lists projects through the normal browser `/api/projects` path.

The expected real authorization observation is:

```text
bound project   -> visible
unbound project -> absent
```

This proves the test does not merely check that B has "a role"; it checks the durable project scope.

## Ordinary account cannot administer identities

Before any destructive member mutation, B sends a valid same-context request to a YU-20 administrative route targeting A with a valid expected revision.

Expected result:

```text
403 forbidden
```

The chosen operation has no intended business side effect on denial, and the route still relies on the YU-20 `system-administrator` OperationGuard.

The UI is not treated as the authorization authority.

## Cross-context CSRF isolation

The harness reads A's real browser CSRF cookie and deliberately supplies it as the request header from B's browser context.

B's own session/CSRF cookies remain in B's cookie jar.

Expected result:

```text
B session cookie + B CSRF cookie + A CSRF header
    -> 403
```

This proves one context's CSRF token cannot authorize another context even though both accounts are valid users in the same organization.

## Real segregation of duties

The E2E creates a real project work item as A and advances it through:

```text
solution_reviewed
-> development_completed
-> test_passed
```

A then attempts `production_validated` on its own implemented change.

The scenario requires both:

```text
HTTP rejection
error == "implementer cannot production-verify or close their own change"
```

and a read-back proving gate/revision are unchanged.

This prevents an unrelated transport failure from being counted as a segregation-of-duties PASS.

B, holding the project-scoped `release-approver` binding, then performs the same production validation at the current revision and must succeed.

Thus the E2E is designed to certify the real persisted implementation principal and an independent second JWT identity, not display-name ownership.

## Role revoke semantics

A revokes B's exact project RoleBinding using the returned binding ID and revision.

The next observations are intentionally separated:

```text
GET /auth/local/current
    -> 200
    -> authentication remains valid

GET /api/projects
    -> 403
    -> project authorization has disappeared
```

This locks the YU-24/YU-25 boundary: role revoke is immediate authorization revocation, not global session logout.

## Credential reset, self password change and disable

The E2E then exercises three YU-25 validity cases against B while repeatedly checking A remains valid.

### Administrator credential reset

A calls the real YU-20 reset route using the fixture's exact user/credential revisions.

Expected:

```text
B old /auth/local/current -> 401
A /auth/local/current     -> 200
```

B then logs in again through the real UI with the reset password.

### Self password change

B calls the real change-password route with current/new password.

The returned user revision is captured for the next CAS operation.

Expected:

```text
B old /auth/local/current -> 401
A /auth/local/current     -> 200
```

B logs in again through the real UI with the new password.

### Member disable

A disables B with the exact user revision returned by the previous self password change.

Expected:

```text
B existing /auth/local/current -> 401
A /auth/local/current          -> 200
B new login                    -> 401
```

This distinguishes affected-account invalidation from tenant-wide authentication failure.

## Browser persistence boundary

The E2E code does not write identity data to:

```text
localStorage
sessionStorage
IndexedDB role snapshots
browser JWT caches
```

Browser identity continues to be carried by the YU-26 host cookies and server-side current/session truth.

The harness contract test explicitly guards against introducing `localStorage`, `sessionStorage`, Playwright or Puppeteer into the YU-29 scenario.

## Execution status

The current cloud execution environment was probed directly.

Available locally:

```text
Node    v22.16.0
npm     10.9.2
Go      go1.23.2 linux/amd64
Chromium /usr/bin/chromium
```

However no `iot-delivery-system` checkout exists in the accessible workspace, and the canonical repository cannot be cloned because:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Therefore the exact status is:

```text
npm run e2e:yu29       AUTHORED, NOT EXECUTED
YU-29 browser E2E      NOT CERTIFIED IN THIS ENVIRONMENT
```

The presence of Chromium/Node/Go is not reported as a PASS because the canonical source could not be executed.

YU-30 still owns the phase-wide `npm test`, typecheck, build, Go test/race/vet, generator and full-check regression. YU-29 does not pre-claim those gates.

## Generation and dependency drift

YU-29 does not modify:

- Yunka framework source or `third_party/yunka`;
- protobuf or generated artifacts;
- identity/session/RoleBinding/permission/Guard implementation;
- YU-28 application UI behavior;
- Go module dependencies;
- npm dependencies or package lock.

`web/package.json` changes only by adding the `e2e:yu29` script.

## Static review corrections before merge

Adversarial source review caught and corrected harness-level issues before merge:

1. long-lived runtime initially used `go run`, which could leave its compiled child after the wrapper was killed;
2. Next initially used an npm wrapper process, which could similarly leave the real Node server;
3. browser cleanup initially removed its profile before confirming Chrome had exited, creating a Windows file-lock race;
4. the harness contract test initially assumed the current working directory was exactly `web/`;
5. the initial SOD assertion accepted any HTTP error, which could falsely count an unrelated failure as SOD enforcement; it now requires the real domain rejection reason and zero mutation.

These are consumer test-harness corrections, not framework defects.

## Framework disposition

No Yunka framework defect was reproduced during YU-29 source analysis.

The fixed Yunka runtime already exposes the durable authorization, execution security, transaction and identity seams that the E2E harness targets.

Because the full browser E2E could not execute in this environment, YU-29 also does not manufacture a framework problem from an unexecuted hypothesis.

No framework Issue is created. Yunka remains fixed at:

```text
057ebcf88a87303eb633eb6e604d306f633dfac0
```

## Next boundary

YU-30 owns the full phase regression: double generation, `yunka check --full`, Go tests/race/vet, frontend tests/typecheck/build, and execution of the YU-29 E2E when the canonical source is available to the toolchain.

YU-29 does not begin YU-30.