# S0-02-06 — BFF assertion, identity binding, and HTTP trace

## Scope and trust boundary

The browser calls only Next `/api/[...path]`. Every safe method requires the
existing server session; every unsafe method also requires the exact server
OIDC redirect origin and its session CSRF value. Rejected requests return
no-store `401` or `403` and never construct an upstream request.

Before forwarding, the BFF copies only the explicit business-header allowlist:
`Accept`, `Content-Type`, `If-Match`, and `Idempotency-Key`. It never copies
browser `Cookie`, `Origin`, CSRF, `Authorization`, API-key, assertion,
signature, trace, `Forwarded`, or `X-Forwarded-*` headers. It then adds the
configured local BFF-channel API key, a server-generated trace ID, and a newly
signed assertion. No provider token, session ID, CSRF token, client secret,
API key, or assertion is returned to the browser.

`backend-yunka` authenticates the existing local API key first. An absent
assertion is explicitly a legacy/bootstrap API-key call, not a person. A
present assertion must pass every check before any application operation runs.
The server, not the browser or assertion, selects the one configured
organization. That organization must already exist and be active. Identity
binding can provision a user only inside that pre-existing organization;
missing, disabled, cross-organization, or disabled identity records fail
closed.

## Assertion contract

`IOT_DELIVERY_BFF_ASSERTION_KEY` is base64url without padding and decodes to
at least 32 bytes. It is independent from `IOT_DELIVERY_LOCAL_API_KEY` and is
used as HMAC-SHA256 key over the base64url encoded strict JSON payload.

| Header | Value |
| --- | --- |
| `X-IoT-Delivery-Assertion` | base64url strict JSON assertion |
| `X-IoT-Delivery-Assertion-Signature` | base64url HMAC-SHA256 |
| `X-Trace-ID` | 32 lowercase hexadecimal characters |

The fixed v1 payload contains `v`, exact (not trimmed or normalized) `issuer`
and `subject`, optional presentation-normalized `email` and `displayName`,
unique `nonce`, `traceId`, upper-case `method`,
exact upstream `path` plus query, SHA-256 `bodySha256`, and epoch-second
`iat`/`exp`. Identity keys cannot have leading or trailing whitespace; all
identity fields reject Unicode `Cc` control characters, and the
encoded assertion is capped at 8192 bytes on both sides. Its lifetime is at
most 90 seconds. The backend requires exactly one bounded value for every
internal header, rejects unknown JSON fields and trailing JSON, validates HMAC
in constant time, binds every request field,
and consumes the nonce in a bounded expiry-pruned replay cache. Forgery,
expiry, body/method/path/trace tampering, malformed headers, and replay all
fail before the Executor and have no domain side effect.

## Principal and trace contract

After successful binding, Yunka receives a Principal with immutable internal
`UserID`, configured organization `TenantID`, stable non-email
`Subject=oidc-bff/<internal-user-id>`, `Authenticated=true`, and Yunka-native
`AuthMethod=jwt`. The JWT method describes the already verified OIDC person;
the HMAC assertion is only a server-to-server propagation channel, never a
second browser authentication method. Every generated operation declares both
`api-key` and `jwt` in the source proto and regenerated operation plan; every
handwritten extension plan does the same. During the S0-03 transition only,
roles are copied from the already authenticated local BFF channel; assertions
carry no role claim and cannot grant a role. gRPC and MCP remain non-browser
API-key/service-entry work for S0-02-07.

The backend adopts the assertion trace immediately after signature and request
binding validation, before identity resolution, so missing, disabled, and
cross-organization bindings return the signed trace too. It becomes Yunka
runtimecontext `TraceID` and `Metadata.RequestID` before the Executor runs.
The BFF ignores browser trace input and creates the trace that it signs.
Legacy API-key traffic receives a server-generated trace, and any partial
assertion headers fail closed. Every
Next and backend HTTP authentication, assertion, domain, and internal error has
the same `X-Trace-ID` and JSON `traceId`; 500 responses are normalized to
`internal_error` without raw errors, profiles, keys, assertions, or tokens.
Malformed, non-object, null, or truncated 4xx response bodies safely normalize
to `request_failed` and never return buffered raw content.

## Configuration and transition

The runtime reads `IOT_DELIVERY_BFF_ORGANIZATION_ID` and
`IOT_DELIVERY_BFF_ASSERTION_KEY` only at the server composition boundary.
They must appear together; invalid partial or invalid-key configuration fails
startup. When both are intentionally absent, the historical local API-key
route remains available solely as legacy/bootstrap compatibility and never
claims to identify a person. The Next BFF refuses missing or malformed
assertion/API-key configuration with stable `503 service_unavailable` and
does not forward.

S0-02-07 owns service identities for gRPC/MCP, S0-03 replaces the temporary
channel-role inheritance with RBAC, S0-05 owns full audit policy, and S0-08
owns production bootstrap. None is implemented by this slice.

## Test evidence

`web/tests/bff-api-route.test.ts` proves traced 401/403/502/503 responses,
session/CSRF rejection without upstream invocation, and replacement of hostile
internal headers. `web/tests/yunka-proxy.test.ts` proves with a real session
and CSRF token that all sensitive browser headers, including forwarding headers,
do not reach the upstream. `web/tests/bff-assertion-vector.test.ts` fixes a v1
TypeScript payload/signature accepted by
`backend-yunka/internal/bffassertion/verifier_test.go`; that verifier also
proves signature, expiry, method/path/body tampering and replay rejection.
`backend-yunka/internal/bffhttp/middleware_test.go` performs real
HTTP→identity binding→Yunka Executor→mutation calls for two external users,
asserts different internal `Activity.Actor` values and stable re-use for the
same external key, observes the bound JWT Principal and trace at the Executor,
tests a signed extension query, legacy API-key trace behavior, normalized 500,
and verifies trace/error and no-side-effect behavior. Bootstrap, generated
gRPC, and MCP API-key compatibility are covered by their integration tests.
