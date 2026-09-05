# YU-21 local password login, opaque session and internal JWT evidence

> Document class: **EVIDENCE**
> Task: `YU-21`
> Fixed consumer parent: `801144169d788720112e4dbcfa1884c39e12771c`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-22`

## Result

YU-21 adds a consumer-owned, transportless local human-authentication capability on top of YU-18 credentials and YU-20 User lifecycle state.

The successful login path is:

```text
canonical organization ID + canonical User ID + password
-> require active Organization + active tenant-bound User
-> verify YU-18 Argon2id credential
-> optional YU-18 NeedsRehash CAS upgrade
-> generate 256-bit opaque session bearer
-> persist only SHA-256(session bearer)
-> persist active server-side session
-> sign short-lived internal HS256 JWT
-> record successful authentication audit
-> one Yunka root local transaction commit
```

No HTTP/gRPC/MCP/BFF auth route or UI is added in this task.

YU-21 also provides internal verification seams:

```text
opaque session bearer
-> SHA-256 digest lookup
-> active session + active Organization + active User
-> verified SessionIdentity
-> short-lived internal JWT

internal JWT
-> strict syntax + HMAC signature + header + claims
-> exact issuer/audience/kid/version/TTL
-> live server-side session
-> active Organization + active User
-> authenticated JWT Principal
```

An unverified token never becomes a Principal.

## Fixed-parent RED

### Expected

After YU-20, local members had durable independent User records and Argon2id credentials, but local human login needed these invariants:

1. only an active User in the requested tenant plus a correct local credential may establish a session;
2. User identity must not be inferred from email/display name or OIDC issuer/subject fields;
3. unknown User, disabled User, cross-tenant User, absent credential and wrong password must expose one credential-failure category;
4. cheap negative account-existence paths must not bypass the configured slow password work factor;
5. the opaque session bearer must be cryptographically random and never persisted in plaintext;
6. the stored session must retain its durable User/tenant association and credential revision;
7. internal JWTs must have an explicit algorithm, issuer, audience, key ID, version and bounded lifetime;
8. signature/header/claims/session verification must complete before an authenticated Principal is created;
9. a disabled User must not create a Principal from a previously valid internal JWT;
10. successful login-time credential rehash, session persistence and success audit must be atomic;
11. failed authentication must not leak User/tenant identity or password/session/JWT/signing-key material through audit;
12. failure-audit unavailability must not turn a rejected credential into a different public credential result.

### Observed at the fixed parent

The fixed parent contained:

- YU-18 Argon2id credential verification and `NeedsRehash` semantics;
- YU-20 active User state, User revision and administrator credential lifecycle operations;
- Yunka root local transaction support;
- durable audit persistence and redaction rules.

It did **not** contain:

- a local login application boundary;
- a server-side local-session schema;
- an opaque session bearer/digest contract;
- an internal JWT signer/verifier contract;
- a verified local JWT -> Principal boundary;
- a session -> short-lived JWT bridge.

This was a consumer capability gap. No Yunka framework defect was required to implement it.

## Login identity contract

`LoginInput` is intentionally:

```text
OrganizationID
UserID
Password
```

It does not accept email or display name as an identity key. YU-20 explicitly allows multiple independent Users to share the same profile fields, so profile data cannot safely identify a local account.

The login operation is:

```text
identity.local-login.authenticate
```

Its internal operation plan declares:

```text
transaction = local
idempotency = none
boundary    = local
authentication = []
permissions    = []
HTTP/RPC       = none
```

This is a pre-authentication boundary: requiring an existing Principal would be circular. The fixed Yunka Executor supports an unprotected plan with no Security phase while still creating the root execution scope and local transaction. It also writes the canonical operation ID into runtime metadata before the application invocation.

YU-21 deliberately uses a plain Yunka `operation.Executor` for login rather than `audit.RecordingExecutor`. A bad password occurs before authorization and must not be classified as an already-authorized application rollback.

## Uniform negative authentication

The public credential sentinel is:

```text
ErrAuthenticationFailed
```

The following valid-looking credential failures map to that same error:

- unknown User;
- disabled User;
- cross-tenant User;
- User without a local credential;
- wrong password;
- unsupported/corrupt local credential record;
- rehash CAS conflict after an otherwise successful verification.

Failure audit is always anonymous/system-scoped and does not contain the attempted tenant or User ID:

```text
category = authentication
actor    = anonymous
scope    = system
result   = failure
reason   = authentication.local_login_failed
```

Failure-audit persistence is best effort. If the audit store is unavailable, `Login` still returns exactly `ErrAuthenticationFailed` and creates no session.

### Synthetic password work

YU-21 adds:

```text
localcredential.VerifyPasswordAgainstSyntheticCredential(password)
```

for valid-looking negative paths that do not have a real stored hash to verify. It consumes the current YU-18 policy's Argon2id work factor and discards/zeroes the generated hash material. It does not read or write a real credential.

Wrong-password paths already consume the real stored Argon2id work factor. Oversized/invalid passwords retain YU-18's common validation behavior.

This reduces the obvious timing gap between “account absent/disabled/no credential” and “credential exists but password is wrong”. It is not represented as a formal constant-time guarantee.

## Opaque server-side session

YU-21 introduces:

```text
iotd_local_sessions
```

with:

```text
id
organization_id
user_id
secret_digest
status
credential_revision
created_at
expires_at
revoked_at
```

The physical contract requires:

- singleton session ID primary key;
- `secret_digest` BLOB, unique, exactly 32 bytes;
- status only `active|revoked`;
- credential revision >= 1;
- expiry after creation;
- active session has no revoked timestamp;
- revoked session has a revoked timestamp;
- composite `(organization_id,user_id)` foreign key to canonical User with `ON DELETE RESTRICT`;
- active-user lookup index.

The migration validates exact columns, physical table SQL, the composite FK identity, index definition and migration ledger. It does not trust a forged ledger entry over a malformed physical table.

### Session bearer

The raw session bearer is:

```text
32 random bytes from crypto/rand
-> raw base64url
-> 43-character opaque token
```

Only:

```text
SHA-256(raw 32-byte secret)
```

is stored in SQLite. A SHA-256 digest is appropriate here because the input is a uniformly random 256-bit secret rather than a human password.

The raw session token is returned only to the caller and is not placed in audit, Outbox or the session table.

### Session verification

`VerifySessionToken`:

1. requires the exact 43-character canonical raw-base64url shape;
2. decodes exactly 32 bytes;
3. hashes the bearer before the database lookup;
4. never sends the raw bearer to SQLite;
5. requires an active, unexpired session;
6. requires the owning Organization and User to still be active;
7. returns only durable session identity facts.

`IssueAccessTokenFromSession` derives tenant/User/session IDs from that verified session rather than caller input.

A session with less than a full AccessTTL remaining cannot mint a new access token. This prevents issuance of a JWT that would already extend past its server-side session expiry.

Logout/revocation/session-version semantics are intentionally deferred to YU-22.

## Internal JWT contract

YU-21 uses Go standard-library HMAC-SHA256 and adds no JWT dependency or `go.mod/go.sum` change.

Default contract:

```text
alg          = HS256
typ          = JWT
kid          = local-auth-v1
iss          = iot-delivery.local
aud          = iot-delivery.internal
ver          = 1
access TTL   = 5 minutes
session TTL  = 12 hours
signing key  >= 32 bytes
future iat skew <= 30 seconds
max token size = 8192 bytes
```

Claims are deliberately minimal:

```text
iss
aud
sub = canonical UserID
tid = canonical OrganizationID
sid = server-side session ID
iat
exp
ver
```

There are no role or permission claims. Durable authorization remains a database responsibility after Principal construction.

The parser is strict:

- exactly three JWT segments;
- canonical unpadded base64url;
- HMAC signature verification;
- exact `HS256/JWT/kid` header;
- strict canonical JSON with unknown fields rejected;
- exact issuer and audience;
- exact token version;
- canonical subject/tenant/session identifiers;
- positive `iat` and `exp`;
- exact configured TTL expressed in integer seconds;
- expiry enforced;
- bounded future `iat` skew.

Tests include valid-HMAC tokens with an invalid `kid`, `alg=none`, unknown claims, wrong issuer/audience/version and tampered/wrong-key signatures. These are rejected before Principal construction.

## JWT -> Principal boundary

`VerifyAccessToken` performs these steps in order:

```text
JWT syntax
-> signature
-> exact header
-> exact claims
-> expiry
-> server-side session ID/tenant/User match
-> session active + unexpired
-> Organization active
-> User active
-> Principal
```

Only after every check succeeds is this Principal returned:

```text
Subject       = local-user/<canonical UserID>
UserID        = canonical UserID
TenantID      = canonical OrganizationID
AuthMethod    = jwt
Authenticated = true
Roles         = []
```

This is intentionally not an OIDC Principal and does not invent an OIDC issuer or subject.

A User disabled after login cannot continue to create an authenticated Principal from an otherwise correctly signed, unexpired JWT.

Credential revision, password-reset invalidation and broader centralized session-version/revocation policy remain YU-22/YU-25 responsibilities. YU-21 stores the session credential revision so those tasks have a durable comparison fact.

## Login-time password rehash

When YU-18 verification returns:

```text
Match = true
NeedsRehash = true
```

YU-21 calls `SetPassword` with the exact verified credential revision inside the same root transaction.

The resulting credential revision is persisted on the new session.

Therefore:

```text
password rehash
+ session row
+ success audit
= one root commit
```

A forced success-audit failure regression is authored to prove that the session insert and credential rehash roll back together.

## Authentication audit

Successful local login records:

```text
category = authentication
actor    = human/<verified UserID>
scope    = organization/<verified tenant>
target   = identity.session/<session ID>
result   = success
reason   = authentication.local_login_accepted
```

Success metadata contains only stable non-secret facts:

```text
credential_revision
credential_rehashed
jwt_version
key_id
access_ttl_seconds
session_ttl_seconds
```

It contains no password, password hash, salt, session bearer, JWT, HMAC key, email or display name.

Success audit participates in the login root transaction. If it cannot persist, no session and no login-time rehash commit.

## Signing-key configuration

YU-21 adds a dedicated environment/config input:

```text
IOT_DELIVERY_LOCAL_AUTH_JWT_KEY
```

The value must be canonical raw base64url and decode to at least 32 bytes.

It is deliberately separate from:

```text
IOT_DELIVERY_BFF_ASSERTION_KEY
```

so two different trust boundaries do not silently reuse one secret.

Because YU-21 exposes no local-auth route yet, an absent local-auth signing key leaves `Application.LocalAuthentication()` disabled (`nil`) while the session schema is still migrated. A configured valid key enables only the in-process capability.

YU-26 must require/configure this capability before exposing local-auth BFF routes.

## Runtime wiring

Startup migration order is now:

```text
identitycore
-> localcredential
-> localbootstrap
-> localmemberadmin
-> locallogin
-> configrevision
-> audit
```

The local-login Manager receives:

- the existing application SQLite database;
- the YU-18 local credential repository;
- the existing SQLite audit store;
- the same Yunka SQLite transaction factory;
- a dedicated local-auth signing key contract.

`Application.LocalAuthentication()` is in-process only. No HTTP/gRPC/MCP/BFF route is registered in YU-21.

## Executable regressions authored

`internal/locallogin/locallogin_test.go` covers:

- successful active User/password login;
- 256-bit opaque session token shape;
- database digest equals SHA-256(raw bearer), not the raw bearer;
- valid JWT -> real tenant/User JWT Principal;
- tampered signature rejection;
- wrong issuer rejection;
- wrong audience rejection;
- wrong key rejection;
- wrong JWT version rejection;
- missing server session rejection;
- unknown/disabled/cross-tenant/no-credential/wrong-password uniform `ErrAuthenticationFailed`;
- no session on failed authentication;
- synthetic password work on absent-state credential failures;
- generic anonymous failure audits;
- old-policy successful-login rehash;
- rehashed credential revision captured by the session;
- forced success-audit failure rolls back session and rehash;
- expired access token rejection;
- password/session/JWT/HMAC-key/runtime-secret sentinels absent from durable audit/session text surfaces.

Additional regressions cover:

- disabled User cannot produce a Principal from a previously valid JWT;
- failure-audit outage does not change credential rejection;
- wrong `kid`, `alg=none` and unknown claims are rejected;
- opaque session lookup and session-derived access-token issuance;
- deleted/malformed session bearer rejection;
- near-expiry session cannot mint a JWT beyond session lifetime;
- migration repeatability and plaintext-field absence;
- forged physical session schema rejection;
- forged migration-ledger rejection;
- internal pre-auth operation-plan validation;
- dedicated local-auth signing-key configuration and no key leakage through startup errors.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent ancestry | PASS by GitHub compare against `801144169d788720112e4dbcfa1884c39e12771c` |
| Framework boundary | PASS by branch diff: Yunka framework/gitlink unchanged |
| Protobuf/generated boundary | PASS by branch diff: no protobuf/generated artifact changed |
| Transport/UI boundary | PASS by branch diff/source review: no local-auth HTTP/gRPC/MCP/BFF route or UI added |
| Dependency boundary | PASS by branch diff: no `go.mod` or `go.sum` change |
| Opaque bearer persistence | PASS by source/schema review: SQL receives only a 32-byte SHA-256 digest |
| JWT verification order | PASS by source review: Principal constructed only after strict JWT + live session + active User/tenant checks |
| Negative auth classification | PASS by source review: one `ErrAuthenticationFailed` category and anonymous generic audit |
| Login-time rehash atomicity | PASS by source review: rehash/session/success audit use one Yunka root local transaction |
| Signing-key separation | PASS by source/config review: dedicated local-auth key separate from BFF assertion key |
| YU-21 executable tests | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: execution container cannot resolve/materialize `github.com` repository/module paths |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | to be checked at final task head; no CI PASS is assumed |
| Canonical generation/full check | NOT RUN; protobuf/generated inputs were not changed |

Environment/tooling absence is neither behavioral RED nor executable PASS.

## Framework issue disposition

No Yunka compiler, generator, Executor, execution-scope, transaction or authorization defect was reproduced.

The fixed Yunka Executor already supports the required pre-authentication shape:

- unprotected plan may execute with no Security phase;
- canonical operation metadata is established before application invocation;
- local root UnitOfWork is opened;
- invoker error rolls the root transaction back;
- success commits once.

YU-21 therefore uses existing public framework seams and leaves Yunka source untouched.

No new Yunka framework Issue is required.

## Residual boundary and next task

YU-21 intentionally does **not** implement:

- current-member operation;
- logout/session revocation operation;
- self-password-change;
- session version/revocation CAS;
- password-reset-driven invalidation of existing sessions;
- project-role-revocation-driven session policy;
- full HTTP/gRPC/MCP durable local-human authentication integration;
- BFF local-auth routes;
- local-auth UI.

The next independent task is `YU-22`: current member, logout, own password change and session version/revocation. It must build on the YU-21 verified opaque session/JWT facts rather than trusting unverified claims.