# YU-28 local-auth, account and governed administration UI evidence

> Document class: **EVIDENCE**  
> Task: `YU-28`  
> Fixed consumer parent: `924a560533b638dd1596a69e122f256c18229320`  
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`  
> Scope stop: before `YU-29`

## Result

YU-28 adds the human interaction layer on top of the existing YU-20/YU-24/YU-25/YU-26/YU-27 identity and authorization truth without introducing another identity, role or session store.

The resulting local browser path is:

```text
login screen
    -> POST /auth/local/login on same-origin Next route
    -> YU-26 runtime local login
    -> host-only local session + CSRF cookies
    -> GET /auth/session
    -> GET /auth/local/current
    -> current durable member/session truth
    -> DeliveryWorkspace

unsafe account/admin action
    -> YU-27 GET /auth/session CSRF preflight
    -> X-CSRF-Token
    -> same-origin Next local-auth forwarder
    -> YU-26 local BFF route
    -> YU-20 or YU-24 manager
    -> durable GrantResolver / OperationGuard / CAS / audit / Outbox
```

No password, opaque session, access JWT, role list, permission list or RoleBinding snapshot is written to `localStorage`, `sessionStorage` or another browser persistence layer.

## Fixed-parent RED review

### Confirmed RED A — the application had no usable local-auth UI

At the fixed parent `web/app/page.tsx` rendered only:

```text
DeliveryWorkspace
```

The workspace had no local login, current-member, logout, self-password-change, member-administration or project-role-administration interaction surface.

Therefore the YU-26/YU-27 browser/auth capabilities existed below the UI but could not be operated by a normal user through the current Next application.

### Confirmed RED B — the current Next application had no same-origin `/auth/local/*` route

YU-26 deliberately exposed the governed local BFF namespace in the Yunka runtime:

```text
/auth/local/*
```

The Next application, however, had routes for the existing OIDC family and `/auth/session`, plus `/api/*`, but no `web/app/auth/local/...` route.

The repository's web development entry serves Next on port 5173 while the Yunka runtime default is port 8281. Without a same-origin application forwarding boundary, merely drawing a login form would not make the local BFF usable through the current web entry.

YU-28 therefore adds a consumer Next forwarding route. This is web/BFF composition work, not a Yunka framework modification.

### Candidate C — UI should decide whether the current local member is an administrator

Falsified as a safe design.

YU-26 `GET /auth/local/current` returns current durable user/session facts but does not return a durable role/capability decision that the UI can treat as authorization authority.

YU-28 therefore does **not** infer administrator status from UserID, display name, token contents or a copied role list. The management UI explicitly states that it does not grant authority; the existing YU-20/YU-24 server OperationGuards remain final.

An ordinary authenticated member may see the protected management entry, but an attempted administrative mutation still receives the server's `403 forbidden`. The UI never labels that member as an administrator.

## Same-origin local-auth browser forwarding

New consumer module:

```text
web/lib/server/local-auth-forwarder.ts
web/app/auth/local/[...path]/route.ts
```

Only the YU-26 route shapes are accepted:

```text
POST /auth/local/login
GET  /auth/local/current
POST /auth/local/logout
POST /auth/local/change-password
POST /auth/local/admin/members
POST /auth/local/admin/members/{userID}/disable
POST /auth/local/admin/members/{userID}/reset-credential
POST /auth/local/admin/project-role-bindings
POST /auth/local/admin/project-role-bindings/{bindingID}/revoke
```

Unknown route shapes do not become an open proxy.

### Browser Origin

Every forwarded unsafe request first validates the browser request using the YU-27 application-Origin policy:

```text
browser Origin == trusted current application Origin
```

A cross-origin request is rejected before the runtime is contacted.

After that outer browser check, the server-to-server request uses the runtime target's own canonical Origin. This is necessary because the YU-26 adapter intentionally binds its Origin check to the HTTP request Host when no explicit trusted runtime origin is configured.

The browser cannot choose the server-to-server Host/Origin pairing.

### Credential isolation

The forwarder never copies browser-controlled:

```text
Authorization
X-API-Key
BFF assertion headers
OIDC session cookie
Forwarded / X-Forwarded-* identity hints
```

Only exact local browser cookies are eligible for the local runtime hop:

```text
__Host-iotd_local_session
__Host-iotd_local_csrf
```

For protected local writes the current `X-CSRF-Token` is forwarded and YU-26 performs its existing exact cookie/header CSRF check.

### Access JWT remains server-side

YU-26 login/current may return the current short access JWT because that is part of its runtime transport contract.

The new Next browser boundary removes from successful login/current JSON before returning to UI code:

```text
accessToken
accessExpiresAt
csrfToken
```

The opaque session remains cookie-only. Frontend CSRF continues to come only from YU-27 `GET /auth/session`.

This keeps the application UI from becoming a second token cache while leaving the YU-26 runtime contract unchanged.

## Explicit local/OIDC login selection

`web/components/local-auth-shell.tsx` now owns the application-level session entry.

Unauthenticated state presents two explicit choices:

```text
local member login
OIDC login -> /auth/login
```

The local login path does not read OIDC configuration.

The shell first reads the existing narrow `/auth/session` endpoint. When a session exists it probes local current-member truth:

```text
local current succeeds -> local mode
local current 401      -> retained OIDC session mode
```

It does not inspect cookie contents or token claims in browser code to guess the authentication family.

OIDC mode retains the existing OIDC BFF session and logout path. Local account/admin controls are shown only for a verified local current-member response.

## Current member UI

The local account panel displays only current server-derived profile/session facts:

- OrganizationID;
- UserID;
- display name;
- email;
- User revision;
- Session revision.

It does not display or persist the access JWT, opaque session, CSRF value, role list or permissions.

The profile is rendered with semantic definition-list markup and the account dialog uses the existing Base UI/shadcn focus-managed dialog primitive.

## Self password change

The password form submits only:

```text
currentPassword
newPassword
```

Passwords are read from the form at submission time and are not saved to browser persistence.

The request reuses the YU-27 automatic CSRF path. On success YU-26/YU-22 semantics revoke the old session; the UI therefore immediately returns to unauthenticated state and asks the member to sign in again.

A `401` during a protected account action also returns the UI to login rather than keeping a stale authenticated shell visible.

## Member administration UI

The member section exposes the existing YU-20 operations only:

```text
create member
    displayName + optional email + initial password

disable member
    UserID + expected user revision

reset credential
    UserID + expected user revision
    + expected credential revision + new password
```

The UI does not create a member list or identity cache because YU-26 does not expose such a read endpoint in this task.

Successful mutation results are kept only in React memory long enough to show canonical UserID and the returned revisions for the next CAS operation.

## Project RoleBinding administration UI

The project-role section exposes the existing YU-24 operations only:

```text
assign
    ProjectID + UserID + RoleID

revoke
    BindingID + expected binding revision
```

RoleID is treated as a canonical identifier submitted to the server. The UI does not copy the permission dictionary into an authorization decision and does not assume that typing `contributor`, `viewer` or another value means the grant exists.

Successful server results can display BindingID/status/revision in transient component memory for the next CAS action.

## Stable user-visible states

The shell has explicit states for:

```text
401 -> session expired / sign in again
403 -> authorization or Origin/CSRF denial
404 -> target member/project/RoleBinding not found
409 -> stale revision or durable state conflict
503 -> identity/authorization service unavailable
```

A session-truth `503` fails closed and does not render the business workspace.

The management dialog does not duplicate the same error/status announcement at both page and modal scope, avoiding duplicate screen-reader announcements.

## Authored regression inventory

### API client

- local login is the sole unsafe local call that does not require an existing CSRF session;
- self-password and administrative writes reuse YU-27 `/auth/session` CSRF preflight;
- protected local writes carry the current `X-CSRF-Token`;
- existing work-item mutation CSRF behavior remains unchanged.

### same-origin local-auth forwarder

- browser same-origin is checked before unsafe runtime forwarding;
- runtime-hop Origin is server selected;
- browser Authorization/API-key/OIDC cookies are not forwarded;
- only exact local session/CSRF cookies are forwarded;
- current `X-CSRF-Token` reaches YU-26 protected routes;
- cross-origin requests stop before runtime access;
- login/current browser payloads do not expose access JWT or CSRF;
- runtime transport failure becomes non-cacheable `503`.

### interaction/accessibility

- unauthenticated UI exposes explicit local and OIDC choices;
- local login reaches verified current member and workspace without OIDC configuration;
- current member facts are visible from server response;
- ordinary-member `403` is shown as permission denial without UI administrator claims;
- project RoleBinding `409` is shown as a revision/state conflict;
- password change returns to login after old-session invalidation;
- session-truth `503` fails closed before workspace render;
- form controls have explicit labels;
- password inputs use password types/autocomplete semantics;
- account management uses a focus-managed dialog and semantic status/error regions.

## Execution status

The execution environment was retried with:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
```

and returned:

```text
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Therefore executable verification is recorded exactly as:

```text
YU-28 web tests       AUTHORED, NOT EXECUTED
npm test              NOT RUN
npm run typecheck     NOT RUN
npm run build         NOT RUN
```

The environment failure is not RED and no PASS is fabricated.

## Generation and dependency drift

YU-28 does not modify:

- `backend-yunka` identity/authentication/authorization source;
- `third_party/yunka` or Yunka framework source;
- protobuf or generated files;
- Go dependencies;
- package dependencies or lock files;
- YU-20 member persistence semantics;
- YU-24 RoleBinding semantics;
- YU-25 centralized validity semantics;
- YU-26 cookie/session/CSRF runtime semantics;
- YU-29 two-browser E2E scenarios.

No generator input changes in this task.

## Framework disposition

No Yunka framework defect was reproduced.

The fixed framework/runtime provides the durable identity, authorization, operation guard, transaction, audit and Outbox behavior required by the UI. The confirmed gaps were consumer web interaction and same-origin BFF composition gaps.

No framework Issue is created and Yunka remains unmodified at:

```text
057ebcf88a87303eb633eb6e604d306f633dfac0
```

## Next boundary

YU-29 owns the two-real-account, two-independent-browser-context E2E certification covering permission independence, disable/reset/password-change/role-revoke invalidation, CSRF and segregation of duties.

YU-28 does not begin those browser-context E2E scenarios.
