# YU-25 centralized local-session validity evidence

> Document class: **EVIDENCE**
> Task: `YU-25`
> Fixed consumer parent: `b3e149ca824dba5dbca6388af768e73a5a61fe35`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-26`

## Result

YU-25 centralizes the identity-producing validity decision for both local access JWTs and opaque server sessions.

The resulting authentication predicate is:

```text
signed JWT claims OR opaque-session digest
        -> exact persisted session
        -> session status = active
        -> session not expired
        -> active Organization
        -> active User
        -> current durable local credential exists
        -> current credential revision == session credential_revision
        -> exact session revision still matches signed JWT sv when JWT is used
        -> verified local human identity
```

No role or permission is copied into the session/JWT validity predicate. Authorization remains the YU-23 durable `humanauthz` read on every protected request.

## Candidate review instead of manufactured RED

YU-25 evaluated each task candidate against the fixed parent rather than assuming every listed risk existed.

### Candidate A — User disable leaves an old session usable

**Falsified as a new defect.**

At the fixed parent, both opaque-session verification and access-JWT verification already joined `users` with `users.status = 'active'`. YU-20 administrator disable changes the User to `disabled` with User revision CAS.

Therefore an old session/JWT could not establish a Principal after User disable even before YU-25.

YU-25 retains that behavior inside the centralized predicate and adds a regression through the real YU-20 administrator manager so the existing protection cannot drift.

### Candidate B — administrator credential reset leaves an old session/JWT usable

**Confirmed real RED.**

At the fixed parent:

- login stored `credential_revision` in the server session;
- YU-20 administrator reset advanced the durable local credential revision;
- access-JWT verification checked active Organization/User, active session, expiry and session revision;
- opaque-session verification checked the same active Organization/User/session facts;
- neither verifier compared the captured session credential revision with the current durable credential revision.

YU-20 reset intentionally did not mutate the YU-21/YU-22 session table. Consequently an already-issued JWT and its opaque session could remain authentication-valid after a successful administrator password reset until another independent session fact changed.

This is the primary YU-25 consumer defect.

### Candidate C — project RoleBinding revoke leaves revoked permission effective

**Falsified as a session-authentication defect.**

YU-23 `humanauthz` already rereads active RoleBindings and active permissions for every authorization resolution. YU-24 revoke changes the durable binding to disabled with CAS. There is no role/permission snapshot in the local JWT.

The correct policy is therefore:

```text
project RoleBinding revoke
    -> authentication session remains valid
    -> revoked project grant disappears on next durable grant resolution
```

Globally logging a user out because one project-scoped role changed would conflate authentication with authorization and would unnecessarily invalidate unrelated project access.

YU-25 locks this decision with a regression: after contributor binding revocation, the same JWT/session still authenticates while `delivery.work-items.create @ project:project-a` resolves to zero grants.

### Candidate D — HTTP/gRPC/MCP disagree about stale local credentials

The fixed parent used the same YU-22 manager for access/session verification, so the credential-reset gap was consistently insecure rather than transport-specific.

YU-25 keeps one server-side validity predicate and adds explicit cross-transport regressions so HTTP, gRPC and MCP cannot drift apart later.

## Centralized validity implementation

New consumer file:

```text
backend-yunka/internal/locallogin/session_validity.go
```

It owns the shared SQLite validity projection used by identity-producing local credentials.

The common query joins:

```text
iotd_local_sessions
organizations(status=active)
users(status=active)
iotd_local_user_credentials(
    organization_id = session.organization_id,
    user_id = session.user_id,
    revision = session.credential_revision
)
```

The credential join is the new YU-25 fence.

### Opaque session

`VerifySessionToken` now:

1. canonical-decodes the opaque bearer;
2. hashes the raw secret;
3. calls `readValidSessionByDigest`;
4. never sends the raw bearer to SQLite;
5. returns `ErrSessionInvalid` when any validity fact is absent.

`IssueAccessTokenFromSession` and `CurrentMemberFromSessionToken` already depend on `VerifySessionToken`, so they inherit the same policy without a second implementation.

### Access JWT

`verifyAccessIdentity` still performs strict JWT signature/header/claim verification first, then calls `readValidSessionByClaims`.

The server read requires:

- exact `sid`;
- exact TenantID/UserID;
- exact signed session revision `sv`;
- active/unexpired session;
- active Organization/User;
- exact current credential revision.

Only then can `principalFromVerifiedClaims` construct the local `AuthMethodJWT` Principal.

`CurrentMember` already depends on this verified-access path and therefore inherits the same policy.

## Administrator password-reset effect

The authored real-path regression performs:

```text
user-a login at credential revision 1
        -> old opaque session + access JWT
admin-yu22 login as a separate real durable account
        -> real YU-20 ResetCredential
        -> User revision 1 -> 2
        -> credential revision 1 -> 2
        -> old session row remains status=active, revision=1,
           credential_revision=1
```

After commit:

```text
VerifyAccessToken(old JWT)             -> invalid
VerifySessionToken(old session)        -> invalid
IssueAccessTokenFromSession(old)       -> invalid
CurrentMember(old JWT)                 -> invalid
new login with reset password          -> valid
```

The test deliberately proves the stale server-session row still exists as active. The security result therefore comes from the centralized credential-revision fence, not from accidentally deleting/revoking the row in the test.

This keeps transaction ownership clean: YU-20 owns the password mutation/audit/Outbox transaction; YU-25 is a read-time validity fence and does not add a second write transaction to the reset path.

## User-disable effect

A second real-path regression uses the same two durable accounts and the real YU-20 `Disable` operation.

After User disable commits:

```text
VerifyAccessToken(old JWT)             -> invalid
VerifySessionToken(old session)        -> invalid
IssueAccessTokenFromSession(old)       -> invalid
CurrentMemberFromSessionToken(old)     -> invalid
```

The administrator Principal remains a valid, independent durable account. This prevents a single User transition from being confused with a tenant-wide authentication failure.

YU-25 does not add a User re-enable operation; re-enable semantics are outside this task.

## RoleBinding revoke policy

YU-25 explicitly keeps project-role revocation out of authentication validity.

The authored regression:

1. creates a real local session for `user-a`;
2. installs the YU-24 durable RoleBinding contract;
3. adds an active project `contributor` binding;
4. confirms `humanauthz` resolves `delivery.work-items.create @ project:project-a`;
5. applies the durable YU-24 revoke state (`status=disabled`, revision CAS result);
6. verifies the same access JWT and opaque session are still authentication-valid;
7. re-runs `humanauthz` and obtains zero matching grants.

This is the intentional separation:

```text
authentication validity = session/User/credential facts
authorization validity  = current RoleBinding/permission/scope facts
```

No role list, permission list, RoleBinding revision or project grant snapshot is added to JWT claims.

## HTTP / gRPC / MCP consistency

YU-23 already routes all local-member credentials through `localtransportauth.Verifier`:

- HTTP access bearer -> `VerifyAccessToken`;
- gRPC access bearer -> `VerifyAccessToken`;
- stdio MCP access token -> `VerifyAccessToken` on each tool invocation;
- stdio MCP opaque session -> `VerifySessionToken` on each tool invocation.

YU-25 adds two cross-transport scenarios:

### Credential revision changes

The durable credential is advanced from revision 1 to 2 while the old session remains captured at revision 1.

Expected for the old credential:

```text
HTTP  -> 401 before handler invocation
gRPC  -> Unauthenticated before handler invocation
MCP session verifier -> unauthenticated
```

### User disabled

After durable User status becomes disabled, the same expected results apply to all three credential surfaces.

HTTP and gRPC security audit entries use the existing generic:

```text
operation = authentication.local_access_token
reason    = authentication.invalid_credential
result    = failure
```

The stale access token itself is not persisted into audit metadata/diff/reason fields.

MCP keeps the YU-23 per-tool revalidation model; YU-25 does not introduce a process-level cached Principal.

## Transaction and audit boundary

YU-25 adds no new security-state write operation.

The state transitions remain owned by their existing atomic boundaries:

- YU-20 `Disable` -> User CAS + existing audit/Outbox;
- YU-20 `ResetCredential` -> User CAS + credential CAS + existing audit/Outbox;
- YU-24 RoleBinding revoke -> RoleBinding CAS + existing audit/Outbox;
- YU-22 self password change/logout -> existing session CAS/revocation + transactional audit.

YU-25 consumes only committed durable facts at authentication/authorization read time. This avoids a second writer, cross-package callback cycle or role cache.

Transport rejection uses the existing authentication-failure audit classification and never stores password/session/JWT secrets.

## Authored regression inventory

### `locallogin`

- real YU-20 administrator disable invalidates old JWT/session;
- real YU-20 administrator reset invalidates old JWT/session by credential revision;
- stale active session row cannot mint a new access JWT after reset;
- stale JWT/session cannot resolve current-member identity after reset/disable;
- new password can create and verify a fresh session after administrator reset;
- two real durable accounts remain independently classified;
- project RoleBinding revoke removes the durable project grant but deliberately does not globally invalidate authentication.

### `localtransportauth`

- credential-revision mismatch gives the same HTTP/gRPC/MCP verdict;
- disabled User gives the same HTTP/gRPC/MCP verdict;
- HTTP/gRPC protected handler is not invoked for stale credentials;
- HTTP/gRPC audit uses generic invalid-credential classification;
- stale access JWT is absent from audit payload fields.

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
YU-25 tests          AUTHORED, NOT EXECUTED
go test ./...        NOT RUN
go test -race ./...  NOT RUN
go vet ./...         NOT RUN
```

Environment/tool failure is not RED and no PASS is fabricated.

## Generation and dependency drift

YU-25 does not modify:

- `go.mod` / `go.sum`;
- protobuf contracts;
- generated assembly/contracts/client/OpenAPI files;
- authorization dictionary;
- `third_party/yunka`;
- Yunka framework source;
- BFF routes;
- UI.

No generation run is required by the authored diff.

## Framework disposition

No Yunka framework defect was reproduced.

The defect was entirely in consumer session-validity composition: the consumer persisted credential revision but did not reread it during later authentication.

YU-25 fixes that through existing consumer persistence and framework-neutral Principal construction. No framework Issue is created and the framework remains unmodified.

## Next boundary

YU-26 owns BFF exposure for local login/current/logout/change-password/admin-member and project-role operations, including cookie/error/cache semantics and explicit OIDC/local-auth routing.

YU-25 does not add BFF routes or begin YU-26.
