# S0-02-05 Server Session and CSRF

## Scope and status

Implemented for the Next.js BFF. This slice accepts the normalized `VerifiedLogin` produced by S0-02-04 only after the OIDC callback is bound to the browser that started login. Browser state contains opaque values only; OIDC access, ID, and refresh tokens, client secrets, PKCE verifiers, and full claims are never placed in a cookie or response.

## Login and callback state machine

1. `GET /auth/login` creates the existing server-side OIDC transaction and redirects to the configured provider.
2. It also sets `__Host-iotd_login` to that transaction's random `state`, with a ten-minute lifetime.
3. `GET /auth/callback` accepts exactly one query `state` only when it securely equals the single login-binding Cookie value. Missing, duplicate, malformed, or mismatched bindings return `400 {"error":"invalid_state"}` and do not consume the legitimate transaction.
4. Once bound, the callback consumes the OIDC transaction. Provider errors, missing code, token exchange or verification failures clear the login-binding Cookie and create no session.
5. On verified login, a new server session is created and the callback clears the login-binding Cookie. If the same request carries a live current `__Host-iotd_session`, that old local session is revoked only after creation succeeds, then the new Cookie is returned. Failed binding, provider, or token paths do not revoke it; capacity exhaustion leaves it intact and returns stable `503 {"error":"session_unavailable"}`.

All auth responses are `Cache-Control: no-store, max-age=0`; errors are stable codes and do not reflect provider data or secrets.

## Server session contract

The browser receives only `__Host-iotd_session`, a cryptographically random opaque session ID. The local server store contains the normalized `VerifiedLogin` (`issuer`, `subject`, optional `email` and `displayName`) plus created, last-access, and absolute-expiry metadata and a separate random CSRF token. Provider tokens are not stored.

The included store is deliberately bounded and in-memory: capacity is 10,000 sessions, absolute TTL is eight hours, and idle TTL is 30 minutes. Successful authorized requests renew idle access only up to the absolute deadline. Rejected Origin or CSRF checks inspect without renewing, so failed cross-site or forged requests cannot keep a session alive. Unknown, expired, revoked, duplicate, malformed, and capacity-exhausted values fail closed. Revocation removes one session ID.

This implementation is for one local process only. It is not distributed, survives neither process restart nor deployment replacement, and has no cross-instance session coordination.

## Cookie and CSRF contract

| Cookie | Value | Max-Age | Common attributes |
| --- | --- | ---: | --- |
| `__Host-iotd_login` | OIDC random state browser binding | 600 seconds | `Secure; HttpOnly; SameSite=Lax; Path=/`; no `Domain` |
| `__Host-iotd_session` | opaque random session ID | 28,800 seconds | `Secure; HttpOnly; SameSite=Lax; Path=/`; no `Domain` |

These attributes apply in every environment, including local HTTP OIDC test configurations. Cookie parsing ignores unrelated browser Cookies (including empty, bare, or base64-padded values), while rejecting a missing, duplicate, empty, bare, or malformed target Cookie.

`GET /auth/session` reads only the session Cookie. A live session returns exactly `{ "authenticated": true, "csrfToken": "…" }`; it never returns identity data, the session ID, or provider material. It sends `Vary: Cookie` and clears an invalid session Cookie with stable `401 {"error":"unauthenticated"}`.

`POST /auth/logout` has no GET counterpart. It requires a live session, an exact `Origin` equal to the origin of trusted `OIDC_REDIRECT_URI`, and a timing-safe `X-CSRF-Token` comparison. Failed origin/CSRF requests do not revoke the session. Success atomically revokes the current local session, clears the Cookie, and returns no-store `204`; the old Cookie then fails.

`guardSessionRequest` is the reusable connection point for S0-02-06: safe methods require a valid session; unsafe methods additionally require the trusted origin and CSRF token. This task intentionally does not attach it to `/api`, transport, Executor, RBAC, auditing, identity binding, or UI login gates.

## Logout boundary and remaining work

Logout revokes only this BFF's local session. No provider end-session action is attempted because provider tokens are intentionally not retained. S0-02-06 should apply the reusable guard at the API transport boundary while preserving this task's no-token browser contract. Persistent/distributed session storage, server-side authorization/RBAC, audit records, service identity, and provider logout are outside this slice.
