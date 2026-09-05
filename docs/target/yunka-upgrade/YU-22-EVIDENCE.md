# YU-22 current member, logout, self password change, and session revocation evidence

> Document class: **EVIDENCE**
> Task: `YU-22`
> Fixed consumer parent: `d693ab34ef5609e678b573ae11c4e6a4273dff72`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-23`

## Result

YU-22 extends the YU-21 local-auth capability with consumer-owned self-service operations for:

- resolving the current member from a strictly verified internal JWT or opaque server session;
- logging out by atomically revoking the current opaque session;
- changing the current member password with explicit session/User/credential optimistic CAS;
- invalidating every pre-change session/JWT after a successful self password change;
- persisting and verifying per-session revision/revocation state.

No HTTP/gRPC/MCP/BFF auth route is added. The capability remains an in-process consumer boundary. YU-23 owns integration of these verified local-auth facts into the shared transport security chain, and YU-26 owns BFF routes.

## Fixed-parent RED

### Expected

After YU-21 established opaque sessions and strict internal JWT verification, self-service identity required these additional invariants:

1. current-member facts must come only from a YU-21 verified JWT/session, never from unverified JWT claims or caller identity fields;
2. logout must revoke the exact current session with optimistic CAS and invalidate already-issued JWTs bound to it;
3. self password change must share the same User revision and credential revision CAS used by administrator credential reset;
4. self password change must atomically invalidate every old session/JWT for that User;
5. stale session/User/credential revisions must never commit partial state;
6. session revision/revocation state must be durable and fail closed even when a compatibility/direct-SQL path tries to bypass the application method;
7. session identity, credential snapshot, bearer digest, lifetime and final revocation timestamp must not be mutable after creation except for the one legal active-to-revoked transition;
8. success audit must commit atomically with logout/password change and must not contain passwords, opaque session bearers, JWTs or signing keys.

### Observed at the fixed parent

The fixed parent had:

- YU-21 local password verification;
- opaque sessions with one-way SHA-256 bearer digests;
- captured credential revision;
- strict internal JWT signature/issuer/audience/key/version/expiry verification;
- live active session + active Organization/User checks before creating a Principal;
- YU-20 User and credential CAS.

It did **not** have:

- persisted session revision;
- a JWT claim bound to exact session revision;
- logout/revocation operation;
- self password change operation;
- current-member application operation;
- application/SQLite invariants preventing session resurrection, identity rebinding, credential snapshot mutation or lifetime extension;
- atomic self password change plus all-session revocation.

These are consumer capability/consistency gaps. No Yunka framework defect was required to reproduce them.

## Session-control migration

YU-22 adds additive migration:

```text
YU-22_local_session_controls_v1
```

Existing YU-21 sessions are upgraded with:

```text
revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
```

The original YU-21 physical-schema verifier was evolved to accept either:

- the original nine-column session schema before the YU-22 additive migration; or
- that schema plus the trailing `revision` column after migration.

This keeps `locallogin.ApplyMigrations` repeatable across both fresh databases and databases upgraded from YU-21.

### Persistent revocation/CAS invariants

YU-22 installs physically verified SQLite triggers for:

1. **revocation CAS**
   - `active -> revoked` requires `NEW.revision = OLD.revision + 1`;
2. **no resurrection**
   - a revoked session can never become non-revoked;
3. **revision discipline**
   - session revision cannot change while status is unchanged;
4. **current credential snapshot on insert**
   - a newly created session's `credential_revision` must still equal the current local credential revision;
5. **immutable session identity/lifetime**
   - `id`, `organization_id`, `user_id`, `secret_digest`, `credential_revision`, `created_at`, and `expires_at` cannot be changed after creation;
6. **immutable final revocation time**
   - after a session is revoked, `revoked_at` cannot be rewritten.

Migration verification reads each trigger SQL definition from `sqlite_master`. A same-name no-op trigger is rejected; a forged migration ledger therefore cannot hide a malformed physical session-control schema.

The credential-snapshot insert trigger closes a real composition race: if login verifies credential revision N but a concurrent password reset advances it before the session insert, the stale session cannot be created.

## JWT contract evolution

YU-22 binds each JWT to the exact persisted session revision with mandatory claim:

```text
sv = session revision
```

Because this is a required new claim, the internal JWT schema is explicitly advanced from:

```text
ver = 1  (YU-21)
```

to:

```text
ver = 2  (YU-22)
```

The signing key identifier remains:

```text
kid = local-auth-v1
```

because `kid` identifies the cryptographic key, not the JWT claim-schema version.

A correctly signed legacy `ver=1` token is rejected after YU-22. YU-21 access tokens have a five-minute TTL, so the compatibility boundary is intentionally short and fail closed.

New and renewed JWTs are signed with the actual server-side session revision. Access verification requires all previous YU-21 checks plus:

```text
claims.sv == persisted session.revision
```

Therefore successful logout changes both status and revision and invalidates already-issued JWTs immediately rather than waiting for token expiry.

## Current-member boundary

Internal operation:

```text
identity.local-session.current
```

The operation is transport-free in YU-22.

`CurrentMember` accepts an access token, then:

1. performs strict YU-22 JWT v2 signature/claim verification;
2. verifies exact session ID, User, tenant and session revision against the live server-side row;
3. requires active session, active Organization and active User;
4. reads current profile/User revision from SQLite;
5. returns only server-owned facts.

Current-member never accepts caller-supplied organization, User, email, display name, roles or JWT claims as trusted member identity.

An equivalent in-process `CurrentMemberFromSessionToken` path first verifies the opaque session digest/server row and then reads the current User. It exists for later BFF composition and has no remote binding in YU-22.

Revoked sessions and disabled Users fail closed for both JWT and opaque-session current-member paths.

## Logout semantics

Internal operation:

```text
identity.local-session.logout
```

Input:

```text
SessionToken
ExpectedSessionRevision >= 1
```

The raw opaque bearer is decoded and SHA-256 hashed before lookup; raw bearer material is never sent to SQLite.

Inside one Yunka root local transaction, logout:

1. reads the exact active server-side session by bearer digest;
2. compares the persisted revision with `ExpectedSessionRevision`;
3. executes CAS update:

```text
status     = revoked
revision   = revision + 1
revoked_at = now
WHERE status = active AND revision = expected
```

4. appends the success audit in the same `*sql.Tx`;
5. commits once.

After commit:

- the opaque bearer no longer verifies;
- every JWT carrying the old session revision fails exact session-revision verification;
- other independent sessions for the same User remain active.

A success-audit failure rolls the revocation back, leaving both opaque session and bound JWT valid. This preserves the fail-closed atomicity contract instead of creating un-audited state changes.

## Self password-change semantics

Internal operation:

```text
identity.local-session.change-password
```

Input requires:

```text
SessionToken
ExpectedSessionRevision
ExpectedUserRevision
ExpectedCredentialRevision
CurrentPassword
NewPassword
```

Passwords are copied into private byte slices and zeroed on return. They are never placed into audit metadata, session rows or JWT claims.

Inside one Yunka root local transaction the operation:

1. verifies the exact active current session and expected session revision;
2. verifies the session's captured credential revision equals the caller's expected credential revision;
3. advances the active User revision with explicit CAS;
4. verifies the current Argon2id password against the credential row;
5. replaces the credential with the YU-18 `SetPassword` expected-revision CAS;
6. revokes **all** active sessions for the User, incrementing each session revision;
7. appends one success audit in the same transaction;
8. commits once.

Any wrong current password, stale session revision, stale User revision, stale credential revision, credential write failure, session revocation failure or audit failure rolls the entire transaction back.

The operation intentionally does not create a replacement session. After successful password change the member must authenticate again with the new password. This gives the operation a simple security invariant: no pre-change session survives.

## Administrator reset versus self change

YU-20 administrator reset and YU-22 self change both use the same:

```text
users.revision CAS
+ localcredential revision CAS
```

YU-22 authors deterministic interleaving regressions using the **real YU-20 `localmemberadmin.Manager`**, durable `humanauthz`, member `OperationGuard`, and a real YU-21 verified administrator JWT Principal.

Two orderings are locked:

```text
admin reset wins first
-> User revision 2 / credential revision 2
-> stale self change expecting 1/1 fails User CAS
```

and:

```text
self change wins first
-> User revision 2 / credential revision 2
-> stale administrator reset expecting 1/1 fails member User CAS
```

Thus the two password-write paths cannot both overwrite the same stale state successfully.

## Boundary intentionally deferred to YU-25

YU-22 invalidates all sessions after **self password change** and invalidates the exact session after logout.

It does **not** claim that YU-20 administrator password reset already invalidates every existing session immediately. Centralized validity checks for:

- administrator reset;
- User disable;
- project-role revocation;

are explicitly assigned to YU-25.

The existing session `credential_revision` snapshot remains available for that centralized policy. YU-22 does not silently broaden its scope and does not claim YU-25's GREEN condition early.

## Internal operation contracts

YU-22 registers three transport-free internal plans:

```text
identity.local-session.current
identity.local-session.logout
identity.local-session.change-password
```

Transactions:

```text
current-member   = none
logout           = local
change-password  = local
```

All use `idempotency=none`, `boundary=local`, and have no HTTP/RPC binding.

They intentionally have no Yunka SecurityPhase authentication declaration in YU-22. The consumer Manager itself is the local-auth verification boundary and directly verifies the opaque session/JWT facts. YU-23 is the task that wires these same verified facts into the shared HTTP/gRPC/MCP security phase.

This is an explicit phase boundary, not a claim that transport authentication is already complete.

## Audit contract

Logout success audit:

```text
category = authentication
actor    = human / verified session UserID
operation = identity.local-session.logout
reason   = authentication.local_logout
target   = identity.session/<session-id>
```

Password-change success audit:

```text
category = authentication
actor    = human / verified session UserID
operation = identity.local-session.change-password
reason   = authentication.local_password_changed
target   = identity.user/<user-id>
```

Persisted audit metadata contains only safe facts such as:

- User revision;
- credential revision;
- resulting session revision;
- revoked-session count.

It does not include:

- current password;
- new password;
- opaque session bearer;
- JWT;
- JWT signing key.

Success-audit failure is transactionally coupled and rolls business/security state back.

## Executable regressions authored

### Self-service behavior

`internal/locallogin/yu22_self_service_test.go` covers:

- current-member accepts a real YU-21/YU-22 verified JWT;
- tampered JWT is rejected;
- a correctly signed JWT with a non-matching session revision is rejected;
- opaque-session current-member derives the same server-owned User identity;
- stale logout session revision fails without changing session state;
- logout revokes only the selected session and its bound JWT;
- an independent second session remains active;
- self password change advances User and credential revisions;
- self password change revokes two pre-existing sessions and their JWTs;
- old password no longer matches and new password matches;
- wrong current password leaves User/credential/session unchanged;
- stale User, credential and session CAS inputs leave state unchanged;
- forced logout success-audit failure rolls revocation back;
- forced password-change success-audit failure rolls User/credential/session state back.

### Administrator-reset interleaving

`internal/locallogin/yu22_admin_reset_race_test.go` uses the real YU-20 administrator manager and covers both winner orderings described above.

### Current-member revocation

`internal/locallogin/yu22_current_member_revocation_test.go` covers:

- revoked session JWT cannot resolve current-member;
- disabled User cannot resolve current-member from JWT;
- disabled User cannot resolve current-member from opaque session.

### Session migration and physical invariants

`internal/locallogin/yu22_migration_test.go` covers:

- existing YU-21 session rows upgrade to revision `1`;
- YU-21 and YU-22 migration ledgers remain exactly once on repeat migration;
- direct revocation without revision advance is rejected;
- a CAS-correct direct revocation is accepted;
- revoked session reactivation is rejected;
- a session insert with stale credential revision is rejected;
- a same-name no-op revocation trigger is rejected by physical migration verification.

`internal/locallogin/yu22_session_immutability_test.go` covers direct mutation attempts for:

- session bearer digest;
- captured credential revision;
- expiry/lifetime extension;
- final `revoked_at` rewrite.

All are rejected and the original session facts remain unchanged.

### JWT version contract

`internal/locallogin/yu22_jwt_version_test.go` locks:

- `ver=2`;
- `sv=1` on a new session;
- correctly signed legacy `ver=1` token rejection.

### Operation plans

`internal/locallogin/yu22_plan_test.go` validates the three internal plans with the fixed Yunka `operationplan.Validate` contract and verifies no transport bindings.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent ancestry | PASS by branch compare against `d693ab34ef5609e678b573ae11c4e6a4273dff72` |
| Framework boundary | PASS by branch diff: Yunka framework/gitlink unchanged |
| Protobuf/generated boundary | PASS by branch diff: no protobuf/generated artifact changed |
| Transport boundary | PASS by branch diff: no HTTP/gRPC/MCP/BFF route added |
| Session revision model | PASS by source review: revision persisted and signed into JWT v2 |
| Logout atomicity | PASS by source review: revoke CAS + audit share one root local transaction |
| Self password-change CAS | PASS by source review: session + User + credential expected revisions are explicit |
| Self password-change invalidation | PASS by source review: all active User sessions are revoked in the password-change root transaction |
| Admin reset/self change composition | PASS by source review and authored real-manager interleaving regressions |
| Session physical invariants | PASS by source review: revocation/revision/identity/lifetime triggers physically verified |
| Sensitive persistence review | PASS by source review: audit/session/JWT state contains no plaintext password/session bearer/signing key |
| YU-22 executable tests | **AUTHORED, NOT EXECUTED** in this environment |
| `go test ./...` | **NOT RUN**: execution container cannot resolve `github.com` to materialize the repository/module path |
| `go test -race ./...` | **NOT RUN** for the same environment reason |
| `go vet ./...` | **NOT RUN** for the same environment reason |
| GitHub CI | no usable task status check is assumed; final HEAD status is checked separately before merge |
| Canonical generation/full check | NOT RUN; protobuf/generated inputs are unchanged |

Execution-environment/tooling absence is neither behavioral RED nor PASS.

The materialization attempt for this task failed with:

```text
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/':
Could not resolve host: github.com
```

## Framework issue disposition

No Yunka compiler, generator, Executor, transaction or operation-plan defect was reproduced.

YU-22 uses existing fixed-framework public seams:

- `operation.Executor` for `none`/`local` operation execution;
- `execution.TransactionHandleFrom` for the root SQLite transaction;
- `operationplan.Validate` for internal canonical plan validation.

No new Yunka framework Issue is required and framework source remains unmodified.

## Residual boundary and next task

YU-22 intentionally does **not**:

- wire local JWT/session verification into HTTP middleware;
- wire local JWT/session verification into gRPC authentication;
- wire local member authentication into MCP;
- implement project RoleBinding management;
- implement centralized administrator-reset/User-disable/role-revocation session validity;
- expose BFF auth routes;
- add UI.

The next independent task is `YU-23`: connect the verified local member identity produced by YU-21/YU-22 to the same durable human authorization chain across HTTP/gRPC/MCP without trusting `Principal.Roles` or reintroducing the shared development `local-admin` authority.