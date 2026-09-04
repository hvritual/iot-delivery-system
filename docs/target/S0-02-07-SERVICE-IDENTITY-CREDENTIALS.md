# S0-02-07 — Service identity credentials

## Trust boundary and principal shape

People continue to enter only through the OIDC+BFF assertion path. Service
callers use Yunka's independent gRPC metadata key
`x-yunka-service-authorization` with the value
`Bearer svc.<credential-id>.<base64url-secret>`. A service credential is never
accepted as a BFF assertion, browser header, local API key, user ID, email, or
display name.

The `svc.` namespace is reserved for service credentials: local API-key
construction, environment loading, and runtime comparison reject it before any
legacy role principal can be created. Existing ordinary local API keys remain
unchanged.

After validation, the adapter creates a Yunka principal with:

| Field | Value |
| --- | --- |
| `Subject` | `service-account/<stable-service-account-id>` |
| `TenantID` | owning active organization ID |
| `UserID` | empty — a service account is not a human user |
| `AuthMethod` | `service-token` |
| roles | empty — S0-03 authorization bindings remain unimplemented |

The gRPC adapter uses Yunka's `CredentialVerifier` and
`AuthenticatedUnaryServerInterceptor`, including standard W3C trace-context
extraction; it does not trust a custom client trace header. Duplicate credential
metadata, malformed values, unknown credentials, expired credentials, revoked
credentials, disabled service accounts, and disabled organizations all return
the same generic unauthenticated result. Invalid service metadata never falls
back to the legacy API-key interceptor.

## Durable credential model and lifecycle

Migration `S0-02-07_service_credentials_v1` adds
`service_account_credentials`, referencing the existing separate
`service_accounts` table. The durable record contains only the credential ID,
owning service account, a 32-byte SHA-256 digest of a random 32-byte secret,
expiry, optional revocation timestamp, and creation time. It has no plaintext
secret/token/credential column.

`serviceauth.Manager` is the in-process management port. `Issue` and `Rotate`
return the plaintext credential only once after committing its digest. `Revoke`
marks one credential revoked without deleting its metadata, and
`DisableServiceAccount` makes every credential for that account fail on the
next authentication. Digest comparison is constant-time.

Yunka's service-token envelope bounds are applied before lookup: total token
length is 32–4096 visible ASCII bytes; credential ID is 1–128 ASCII characters
from `[A-Za-z0-9_-]`; the base64url secret is bounded before decoding and
decodes to 32–64 bytes. Control
characters, surrounding whitespace, malformed Base64url, and duplicate Bearer
metadata fail closed.

Rotation runs in one SQLite transaction: it validates the current active
credential, inserts the new digest, and marks the old credential revoked. The
commit completes before the new plaintext value is returned, so successful
rotation invalidates the old credential immediately; a failed insert rolls back
both changes.

## Generated-operation boundary

The 12 existing canonical delivery operations declare `api-key`, `jwt`, and
`service-token` authentication in the source Protobuf and regenerated
Operation Plans. A service account starts with no roles, so it reaches the
standard Yunka authentication chain but is denied by the existing permission
plans until S0-03 supplies explicit authorization bindings. No service account
is converted into a shared role API key.

The verifier requires gRPC privacy and integrity (TLS, mTLS, ALTS, or an
equivalent channel) by default. `AllowInsecureServiceCredentialsForDevelopment`
is false by default and can only be enabled for a loopback listener in an
explicit development/test configuration.

S0-02-07 intentionally exposes no remote credential-management RPC, HTTP
route, or MCP tool. Its write entrypoint is the process-local manager only.
Were such a write transport added, it would require a canonical
Protobuf/DSL-generated Operation Plan and the Executor/authorization/
transaction/Outbox path; the structure tests prohibit silently adding a
management transport in this slice.

## Non-goals

This slice does not provision service accounts, grant project or role bindings,
change the legacy local API-key compatibility boundary, add a production
bootstrap switch, contact Vault or a production database, or modify Yunka.
