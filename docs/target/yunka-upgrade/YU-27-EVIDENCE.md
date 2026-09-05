# YU-27 frontend CSRF and application-Origin evidence

> Document class: **EVIDENCE**  
> Task: `YU-27`  
> Fixed consumer parent: `11b32cec898166603f498d4cea8bd5f16fe45566`  
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`  
> Scope stop: before `YU-28`

## Result

YU-27 closes the web-side gap between the YU-26 local-auth BFF and the existing browser API proxy without changing YU-25/YU-26 server identity semantics.

The resulting browser mutation chain is:

```text
frontend unsafe API call
    -> GET /auth/session for current verified CSRF
    -> X-CSRF-Token on POST/PUT/PATCH/DELETE
    -> explicit browser session family selection
       -> OIDC server session OR YU-26 local opaque session
    -> application Origin derived independently of OIDC
    -> current CSRF comparison
    -> server-generated upstream credential
       -> BFF assertion for OIDC session
       -> YU-26 current access JWT for local session
    -> Yunka runtime
```

No session bearer, access JWT, role list or permission snapshot is persisted in localStorage/sessionStorage or another browser persistence layer.

## Fixed-parent RED review

### Confirmed RED A — unsafe API Origin depended on OIDC configuration

At the fixed parent, `web/app/api/[...path]/route.ts` used:

```text
readOidcConfiguration().redirectUri.origin
```

for every unsafe `/api/*` request.

Therefore an otherwise valid local-auth browser flow could not perform an unsafe API call when OIDC configuration was absent. The API proxy returned service-unavailable before the existing Origin/CSRF guard could accept the local application origin.

This was a consumer web/BFF composition defect, not a Yunka framework defect.

### Confirmed RED B — frontend API client did not send CSRF automatically

At the fixed parent, `web/src/api.js` called `/api/*` directly. Mutation helpers set method/body/content-type, but did not retrieve the current BFF session CSRF token and did not send `X-CSRF-Token`.

The server guard already required CSRF for unsafe requests, so normal frontend mutation helpers could not satisfy the browser-side contract on their own.

### Confirmed RED C — `/api/*` had only the older OIDC in-memory session source

The fixed-parent catch-all API route called `guardSessionRequest(request, serverSessions)`. That store contains the existing OIDC `VerifiedLogin` browser session.

YU-26 introduced a separate durable local opaque session and `/auth/local/current`, but the web API proxy had no explicit local-session branch and no way to obtain the YU-25-verified local access JWT server-side.

### Candidate D — safe methods were being forced through CSRF

Falsified at the server guard.

The existing `guardSessionRequest` already treats `GET`, `HEAD` and `OPTIONS` as safe and does not require Origin/CSRF for them. YU-27 preserves that behavior and adds a frontend regression proving safe API helpers do not perform a CSRF preflight.

## Independent application Origin

New consumer module:

```text
web/lib/server/application-origin.ts
```

`resolveTrustedApplicationOrigin` never imports or reads OIDC configuration.

The policy is:

```text
IOT_DELIVERY_WEB_ORIGIN present
    -> must be canonical HTTP(S)
    -> must exactly match request origin

IOT_DELIVERY_WEB_ORIGIN absent
    -> derive from canonical request URL origin

HTTPS
    -> accepted

HTTP
    -> accepted only for localhost / loopback development origin

non-loopback HTTP, malformed origin, userinfo/path/query/fragment,
configured/request mismatch
    -> fail closed
```

The existing OIDC session logout route now uses the same application-Origin policy instead of `OIDC_REDIRECT_URI`, so Origin authorization is no longer an accidental side effect of identity-provider callback configuration.

OIDC discovery, callback and token validation remain unchanged and still use their OIDC-specific configuration where that configuration is semantically required.

## Explicit browser session-family selection

The API proxy now selects the browser authentication source using distinct session-cookie families:

```text
__Host-iotd_session
    -> existing OIDC server session

__Host-iotd_local_session
    -> YU-26 local opaque session

both present
    -> 401 unauthenticated
```

It does not attempt one source and silently fall back to the other.

### OIDC branch

The OIDC branch retains:

```text
serverSessions
-> guardSessionRequest
-> current VerifiedLogin
-> server-generated BFF assertion
-> existing BFF channel credential
```

The only semantic change is that unsafe Origin comes from the independent application-Origin policy rather than `OIDC_REDIRECT_URI`.

### Local branch

The local branch performs:

```text
exact __Host-iotd_local_session
    -> server-to-server GET /auth/local/current
    -> YU-25 centralized validity
    -> current YU-26 CSRF token
    -> current short local access JWT
```

The access JWT remains inside the server proxy. It is not accepted from a browser Authorization header and is not returned by `/auth/session`.

The resulting upstream `/api/*` request is created with:

```text
Authorization: Bearer <current YU-26 access JWT>
```

and deliberately omits:

```text
browser Authorization
browser X-API-Key
browser BFF assertion headers
browser Cookie
browser X-CSRF-Token
server development X-API-Key
```

This avoids mixing local JWT and development API-key credential families at the YU-23 runtime boundary.

## CSRF continuity across YU-26 current

Static adversarial review found one issue in the first YU-27 draft before merge.

If the Next proxy forwarded only the local session cookie to `/auth/local/current`, YU-26 would see the local CSRF cookie as absent and generate a replacement CSRF value. A frontend preflight and the following API request could therefore observe different CSRF values and fail every mutation.

The final implementation forwards the existing canonical:

```text
__Host-iotd_local_csrf
```

alongside the local session cookie when calling YU-26 current.

If the browser legitimately lost the CSRF cookie, `/auth/session` propagates the replacement `Set-Cookie` emitted by YU-26 current. This preserves YU-26 as the CSRF-cookie semantic owner rather than inventing a second CSRF store in Next.

## `/auth/session` as the frontend CSRF source

The existing safe `GET /auth/session` endpoint now supports both browser session families explicitly.

OIDC session:

```text
serverSessions -> csrfToken
```

Local session:

```text
YU-26 /auth/local/current -> current csrfToken
```

The browser response remains intentionally narrow:

```json
{
  "authenticated": true,
  "csrfToken": "..."
}
```

It does not expose the local opaque session, the YU-26 access JWT, roles or permissions.

Mixed OIDC/local session cookies fail closed instead of selecting a source by precedence.

## Frontend automatic CSRF

`web/src/api.js` now classifies methods as:

```text
safe   = GET / HEAD / OPTIONS
unsafe = everything else used by the client, including POST / PUT / PATCH / DELETE
```

Before every unsafe API call it performs a no-store same-origin request to:

```text
GET /auth/session
```

and uses only the returned current verified-session `csrfToken` for:

```text
X-CSRF-Token
```

The token is not written to localStorage, sessionStorage or another persistent cache. A session lookup failure prevents the mutation request from being sent.

Safe API methods do not perform this CSRF preflight and do not add `X-CSRF-Token` as a required header.

## Unsafe local request check

For the local-session API branch an unsafe request must satisfy both:

```text
Origin == trusted application Origin
X-CSRF-Token == current csrfToken returned by YU-26 current
```

The comparison uses the existing timing-safe `secureEqual` helper. Missing, stale, malformed or mismatched CSRF is mapped to stable:

```text
403 forbidden
```

before the business mutation request is forwarded upstream.

Safe local requests still revalidate the local session through YU-26 current for authentication, but do not require Origin or CSRF.

## Authored regression inventory

### frontend client

- safe `fetchProjects` performs no `/auth/session` CSRF preflight;
- every work-item mutation obtains the current `/auth/session` CSRF first;
- every mutation sends that value in `X-CSRF-Token`;
- failed current-session lookup prevents the mutation request;
- existing expected-revision payloads remain unchanged.

### application Origin

- canonical HTTPS request origin derives without OIDC configuration;
- localhost/loopback HTTP development origin is accepted;
- non-loopback plaintext HTTP is rejected;
- explicit `IOT_DELIVERY_WEB_ORIGIN` must exactly match request origin.

### API proxy

- OIDC-session unsafe API request succeeds with OIDC environment values absent when BFF assertion/channel configuration itself is present;
- local opaque session is reread through YU-26 current;
- local mutation forwards only the server-obtained access JWT;
- development API-key is not mixed into the local-JWT upstream request;
- stale local CSRF returns 403 before the mutation reaches upstream;
- safe local GET requires no Origin/CSRF header;
- simultaneous OIDC and local session cookies fail closed before upstream access;
- `/auth/session` returns local CSRF without returning the server-side access JWT;
- local CSRF cookie continuity is preserved;
- lost CSRF cookie is replaced only by propagating YU-26 current's `Set-Cookie`.

## Execution status

The cloud execution environment was retried with:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
```

and returned:

```text
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Therefore executable verification is recorded exactly as:

```text
YU-27 web tests       AUTHORED, NOT EXECUTED
npm test              NOT RUN
npm run typecheck     NOT RUN
npm run build         NOT RUN
```

Environment failure is not RED and no PASS is fabricated.

## Generation and dependency drift

YU-27 does not modify:

- `backend-yunka` business/authentication source;
- `third_party/yunka` or Yunka framework source;
- protobuf contracts;
- generated assembly/contracts/client/OpenAPI files;
- `package.json` / lock files;
- Go dependencies;
- YU-26 local session/credential/RoleBinding semantics;
- login/member/project-role UI;
- browser E2E scenarios.

No generator input changes in this task.

## Framework disposition

No Yunka framework defect was reproduced.

The fixed framework/runtime continues to accept the verified local access JWT and resolve durable authorization correctly. The confirmed defects are in the consumer web client and Next BFF composition: missing automatic CSRF, OIDC-coupled application Origin, and lack of an explicit local-session proxy branch.

No framework Issue is created and Yunka remains unmodified at `057ebcf88a87303eb633eb6e604d306f633dfac0`.

## Next boundary

YU-28 owns the actual login/current/logout/password-change/member/project-role user interface and its accessibility/error/permission-state behavior.

YU-27 does not add those screens and does not begin YU-28.
