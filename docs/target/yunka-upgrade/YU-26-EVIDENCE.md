# YU-26 local-auth BFF route evidence

> Document class: **EVIDENCE**  
> Task: `YU-26`  
> Fixed consumer parent: `15c1335417c0dce778973127a08178e5338189cf`  
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`  
> Scope stop: before `YU-27`

## Result

YU-26 exposes the existing local identity capabilities through one browser-facing consumer HTTP namespace without copying their business rules:

```text
/auth/local/*
    -> local BFF transport checks
    -> YU-21/22/25 local session/login capability
    -> YU-20 member administration capability
    -> YU-24 project RoleBinding administration capability
    -> existing durable authorization / OperationGuard / CAS / audit / Outbox
```

The new adapter is `backend-yunka/internal/localbffhttp` and is registered by the existing runtime capability binder. It has no `database/sql` dependency and does not issue SQL writes.

The existing OIDC BFF route family remains separate. YU-26 does not replace or silently fall back from `/auth/login`; local authentication is selected explicitly through `/auth/local/...`.

No UI or frontend automatic CSRF injection is added. Those remain YU-27/YU-28 work.

## Fixed-parent review

YU-26 reviewed the dispatch candidates against the fixed parent rather than assuming every listed defect existed.

### Confirmed RED A — no local-auth BFF route existed

At the fixed parent the local capability was deliberately in-process:

- `Application.LocalAuthentication()` exposed YU-21/YU-22/YU-25 login/session/JWT logic;
- `Application.MemberAdministration()` exposed YU-20 member management;
- `Application.ProjectRoleAdministration()` exposed YU-24 RoleBinding management;
- none of those capabilities had a browser route.

Therefore a browser could not perform local login/current/logout/password-change or the local administrative writes through a governed HTTP adapter.

### Confirmed RED B — the YU-23 protected HTTP middleware would reject a cookie-only local login surface

YU-23's HTTP authentication middleware owned the protected application transport. A request with no local JWT delegated to the existing BFF/development fallback or failed authentication.

A new cookie-backed login/current route placed behind that same requirement would be unreachable: login cannot already possess the access JWT it is supposed to create.

YU-26 therefore carves out exactly one transport namespace:

```text
/auth/local/
```

from bearer/BFF-assertion authentication. The namespace is not an authorization bypass: its own handlers require Origin, CSRF and YU-25 session verification as appropriate, while administrative operations still pass through their real YU-20/YU-24 Executor security and OperationGuards.

All non-`/auth/local/` paths keep the YU-23 protected middleware unchanged.

### Candidate C — local and OIDC credentials were inherently ambiguous

The existing OIDC BFF routes are `/auth/login`, `/auth/callback`, `/auth/session` and `/auth/logout` in the web BFF. The new local routes use `/auth/local/...`.

The route families are therefore explicitly selected rather than guessed from credential contents.

YU-26 additionally rejects bearer, development API-key, or BFF assertion credentials on the local BFF namespace instead of silently ignoring a second credential family.

### Candidate D — administrator BFF writes would need direct SQLite mutations

Falsified as a necessary design. The existing YU-20/YU-24 managers are transport-neutral application ports and can be called directly from an HTTP adapter while retaining their shared Yunka Executor, durable GrantResolver, dedicated OperationGuard, transaction, audit and Outbox semantics.

The final BFF handler has no `database/sql` import and no `Exec`, `Query` or `QueryRow` calls.

## Route contract

| Method | Route | Authentication / guard | Application capability |
| --- | --- | --- | --- |
| POST | `/auth/local/login` | exact same-origin `Origin`; no preexisting session required | `locallogin.Login` |
| GET | `/auth/local/current` | verified opaque session | `CurrentMemberFromSessionToken` + `IssueAccessTokenFromSession` |
| POST | `/auth/local/logout` | verified opaque session + Origin + CSRF | `locallogin.Logout` |
| POST | `/auth/local/change-password` | verified opaque session + Origin + CSRF | `locallogin.ChangePassword` |
| POST | `/auth/local/admin/members` | verified opaque session -> JWT human Principal + Origin + CSRF | YU-20 `Create` |
| POST | `/auth/local/admin/members/{userID}/disable` | same + YU-20 durable guard | YU-20 `Disable` |
| POST | `/auth/local/admin/members/{userID}/reset-credential` | same + YU-20 durable guard | YU-20 `ResetCredential` |
| POST | `/auth/local/admin/project-role-bindings` | same + YU-24 durable guard | YU-24 `Assign` |
| POST | `/auth/local/admin/project-role-bindings/{bindingID}/revoke` | same + YU-24 durable guard | YU-24 `Revoke` |

The adapter never accepts role names or permissions as authentication facts. Administrative Principal construction uses `localtransportauth.Verifier.VerifySessionToken`, whose result is a verified `AuthMethodJWT` human with `Roles` empty.

## Login and session delivery

A successful login delegates to YU-21/YU-25:

```text
organizationId + userId + password
    -> locallogin.Manager.Login
    -> Argon2id credential verification
    -> opaque server session
    -> short local access JWT
```

The browser receives the opaque session only as:

```text
__Host-iotd_local_session
```

The session bearer is not returned in JSON.

A separate browser-readable CSRF cookie is created:

```text
__Host-iotd_local_csrf
```

The response may return the short access token and its expiry, as allowed by the YU-26 task contract. Renewal is performed only through `IssueAccessTokenFromSession`, so the YU-25 centralized User/credential/session validity fence remains authoritative.

If a server session is created but the BFF cannot safely construct the browser cookie response, the adapter best-effort revokes the newly created session instead of intentionally leaving an undelivered bearer active.

## Cookie contract

### Session cookie

```text
Name     = __Host-iotd_local_session
Secure   = true
HttpOnly = true
SameSite = Strict
Path     = /
Domain   = absent
Max-Age  = bounded by persisted local-session expiry
Expires  = persisted local-session expiry
```

### CSRF cookie

```text
Name     = __Host-iotd_local_csrf
Secure   = true
HttpOnly = false
SameSite = Strict
Path     = /
Domain   = absent
Max-Age  = bounded by persisted local-session expiry
```

Both values are canonical unpadded base64url encodings of 32 random bytes. Duplicate, malformed or non-canonical target cookies fail closed.

`__Host-` plus `Secure`, root Path and no Domain keeps the browser credential host-only and prevents a narrower path/domain cookie from replacing the canonical value.

## Origin and CSRF

All unsafe local routes require one exact Origin and an exact CSRF double-submit value after login.

The local-auth origin contract does not read OIDC configuration.

When an explicit trusted origin is supplied to the adapter, it must be a canonical HTTP(S) origin and match exactly.

When the runtime does not provide a separate origin setting, the adapter derives the allowed same-origin authority from the request Host:

- `https://<request-host>` is accepted;
- plaintext `http://` is accepted only for localhost/loopback development hosts;
- cross-host, duplicate, malformed, path-bearing or otherwise non-canonical Origin values fail closed.

CSRF requires exactly one:

```text
__Host-iotd_local_csrf cookie
X-CSRF-Token header
```

and compares the two values with a timing-safe comparison.

A failed Origin or CSRF check does not call `Logout`, `ChangePassword`, YU-20 or YU-24 mutation logic.

YU-27 remains responsible for automatically obtaining/sending the CSRF token from the frontend API client.

## Current-member and access-token renewal

`GET /auth/local/current` does not trust user/profile values stored in a browser token.

It performs:

```text
session cookie
    -> CurrentMemberFromSessionToken
    -> YU-25 centralized session validity
    -> current durable User profile
    -> IssueAccessTokenFromSession
    -> YU-25 centralized validity again before minting short access JWT
```

The response contains current User/session revisions and a short access token; it never contains the opaque session bearer.

A stale credential revision, disabled User, revoked session or expired session therefore cannot use the current endpoint to mint a fresh access JWT.

## Logout

Logout derives the expected session revision from the verified server session, not from caller JSON, and calls the existing YU-22 CAS operation:

```text
verified session
    -> Logout(sessionToken, persisted session revision)
    -> active -> revoked
    -> revision + 1
    -> transactional authentication audit
```

Success clears both browser cookies.

Missing/wrong Origin or CSRF is rejected before the logout mutation and leaves the session live.

## Self password change

The browser supplies only current and new password values.

The BFF derives:

- expected session revision from the verified session;
- expected credential revision from the verified session;
- expected User revision from `CurrentMemberFromSessionToken`.

It then calls YU-22 `ChangePassword`.

The caller therefore cannot choose stale/forged revision facts to weaken the CAS contract. A successful password change retains YU-22 semantics and revokes the old sessions, after which YU-26 clears its browser cookies.

Passwords are converted to private byte slices for the application call and zeroed before the HTTP handler returns. They are not put into audit metadata or error text.

## Administrator member routes

YU-26 does not implement member persistence inside the transport.

The path is:

```text
opaque session cookie
    -> YU-25 VerifySessionToken
    -> localtransportauth verified no-Roles JWT Principal
    -> Principal attached to Yunka context
    -> YU-20 Manager
    -> GrantAuthorizer / humanauthz
    -> YU-20 system-administrator OperationGuard
    -> User/credential CAS
    -> audit + Outbox in root transaction
```

The real-path regression logs in a durable `system-administrator` account and exercises create -> reset credential -> disable. The resulting User/credential state, audit rows and Outbox records are read back from SQLite.

A second ordinary durable User authenticates successfully but receives HTTP 403 when attempting the member-create route. This proves the route does not convert a valid session into administrator authority and does not authorize from `Principal.Roles`.

## Project RoleBinding routes

The project-role adapter follows the same verified Principal boundary and calls the existing YU-24 manager.

The real-path regression assigns `contributor` to a real project/member, reads back the active binding at revision 1, then revokes through the BFF route and reads back:

```text
status   = disabled
revision = 2
```

The YU-24 durable role dictionary validation, project ownership check, system-administrator guard, CAS, audit, Outbox and immutable history rules remain unchanged.

YU-26 does not update `role_bindings` directly.

## Error and cache contract

Every local-auth handler sets:

```text
Cache-Control: no-store, max-age=0
Pragma: no-cache
Vary: Cookie, Origin
X-Trace-ID: <request trace>
```

Stable transport error classes are used:

```text
400 invalid_request
401 unauthenticated
403 forbidden
404 not_found
409 conflict
503 service_unavailable
```

Unknown user and wrong password login attempts both map to `401 unauthenticated`. The response does not echo UserID, password, credential details or internal persistence errors.

Authorization denial is recognized through Yunka `authz.IsDenied` and mapped to 403 without exposing grant/guard internals.

## Credential-family separation

`localtransportauth.HTTPMiddleware` now has an explicit `/auth/local/` branch.

For this namespace:

- no Authorization bearer is required;
- no BFF assertion is required;
- no development API-key is required by the route;
- if any of those competing credential headers are supplied, the request is rejected instead of selecting an identity by precedence.

For every other path, the existing YU-23 middleware behavior remains unchanged.

An authored regression proves `/auth/local/login` reaches the local handler without bearer authentication while `/api/items` without authentication still goes to the protected fallback. It also proves bearer/API-key mixing on the local namespace is rejected.

## Runtime composition

The new route registration is performed exactly once inside `applicationRuntimeBinder.Bind`, after the real YU-20/YU-24 managers have been constructed with the shared runtime capabilities and before the HTTP server begins application operation handling.

The BFF gets:

- the existing `Application.localLogin` manager;
- the runtime YU-20 member manager;
- the runtime YU-24 project-role manager;
- the existing security audit recorder.

There is no second member manager, role manager, authorization resolver or transaction factory.

An AST regression locks the single `registerLocalAuthBFF` call inside the runtime binder.

## OIDC boundary

YU-26 does not modify:

```text
web/app/auth/login
web/app/auth/callback
web/app/auth/session
web/app/auth/logout
```

Those remain the existing OIDC BFF family.

The local route family is explicitly:

```text
/auth/local/...
```

and its handler imports no OIDC library or OIDC configuration. A local login therefore does not perform provider discovery, authorization-code exchange, issuer/audience/nonce verification or any OIDC fallback.

This task does not delete the existing OIDC path; it separates the two authentication choices.

## Authored regression inventory

### local BFF

- real local login creates session + access JWT through YU-21/YU-25;
- opaque session is cookie-only and absent from JSON;
- Secure/HttpOnly/Path/Domain/SameSite/expiry cookie attributes;
- current-member rereads server session/User and renews only a short access token;
- missing CSRF does not revoke a session;
- wrong Origin does not revoke a session;
- valid logout revokes through YU-22 and clears cookies;
- password change derives all CAS revisions from verified server facts and clears the old session;
- wrong password and missing User share stable `unauthenticated` response classification;
- real YU-20 create/reset/disable through durable system-administrator authorization;
- ordinary authenticated User cannot perform YU-20 administrator route;
- real YU-24 project-role assign/revoke through durable authorization/CAS;
- audit and Outbox evidence exists after administrator mutations;
- AST gate proves no direct SQLite calls from the BFF adapter.

### transport / bootstrap

- only `/auth/local/` bypasses bearer/BFF-assertion middleware;
- ordinary protected `/api` routes retain the existing fallback;
- competing Authorization/API-key credentials on local BFF namespace are rejected;
- local BFF registration occurs exactly once inside the shared runtime binder.

## Execution status

The cloud execution environment was retried with:

```text
git clone --depth 1 --branch codex/yu-26-local-auth-bff-routes \
  https://github.com/hvritual/iot-delivery-system.git /tmp/yu26-check
```

and returned:

```text
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Therefore executable verification is recorded exactly as:

```text
YU-26 tests          AUTHORED, NOT EXECUTED
go test ./...        NOT RUN
go test -race ./...  NOT RUN
go vet ./...         NOT RUN
```

Environment/tool failure is not RED and no PASS is fabricated.

## Generation and dependency drift

YU-26 does not modify:

- `go.mod` / `go.sum`;
- protobuf contracts;
- generated assembly/contracts/client/OpenAPI files;
- authorization dictionary;
- `third_party/yunka`;
- Yunka framework source;
- frontend API client automatic CSRF behavior;
- login/member/project-role UI.

No generator input is changed and no generation run is required by this diff.

## Framework disposition

No Yunka framework defect was reproduced.

The fixed framework already provides the Executor, Principal context, GrantResolver/GrantAuthorizer, OperationGuard, root transaction and Outbox seams required by the YU-20/YU-24 application ports. The missing capability was a consumer BFF adapter and a consumer HTTP middleware namespace composition rule.

No framework Issue is created and the framework remains unmodified.

## Next boundary

YU-27 owns frontend/API-proxy CSRF automation and removing the current Origin dependency on OIDC configuration.

YU-26 stops after its branch is reviewed and fast-forwarded to `main`; it does not begin YU-27.
