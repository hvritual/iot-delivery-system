# YU-20 administrator member-management evidence

> Document class: **EVIDENCE**
> Task: `YU-20`
> Fixed consumer parent: `4bad8f3cb77e6e4b5f2899b891bfe964e186e9cf`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-21`

## Result

YU-20 adds consumer-owned, administrator-driven local member lifecycle operations for:

- creating a member with an independent local credential;
- disabling an active member with User revision CAS;
- resetting/creating a member local credential with User revision CAS plus credential revision CAS.

The operations remain internal in YU-20. No login/session/JWT issuer, BFF local-auth route or UI is added.

Every successful write uses the same Yunka root `local` transaction boundary already used by the application runtime:

```text
durable JWT authorization + member guard
+ User state / revision
+ local credential
+ success audit
+ transactional Outbox
= one root commit
```

Application, credential, success-audit or Outbox failure rolls back the transaction. `audit.RecordingExecutor` records the post-rollback failure only after rollback.

## Fixed-parent RED

### Expected

After YU-19 permanently closed anonymous administrator bootstrap, an authenticated administrator-management boundary needed these invariants:

1. only a durable JWT human holding the organization-scoped `system-administrator` authority can manage members;
2. `Principal.Roles`, email, display name and delivery Owner values are never identity/authorization authorities;
3. same email/display name may still represent distinct Users and distinct credentials;
4. disable and credential reset have explicit optimistic-CAS semantics;
5. cross-organization targets are indistinguishable from absent targets to the application operation;
6. User/credential/audit/Outbox state commits atomically;
7. password and other credential/session material never enters audit or Outbox payloads;
8. after anonymous bootstrap is permanently closed, member administration cannot disable the last active system administrator and leave the deployment unrecoverable;
9. pre-YU-20 direct User-disable compatibility paths must not bypass the new CAS/security boundary.

### Observed at the fixed parent

The fixed parent had:

- YU-18 local credentials;
- YU-19 one-time administrator bootstrap;
- durable human RoleBinding resolution;
- shared Yunka Executor / SQLite UnitOfWork;
- audit and transactional Outbox.

It did **not** have:

- an active `identity.users.manage` permission;
- canonical member create/disable/reset operation plans;
- a member-specific durable system-administrator guard;
- a User lifecycle revision/CAS contract;
- a last-administrator invariant;
- atomic administrator member operations.

The existing `identitybinding.Resolver.DisableUser` also directly executed a `users.status` update with no User revision CAS, application audit or Outbox. This was a concrete consumer write bypass once YU-20 establishes the canonical member-disable boundary.

These are consumer capability/consistency gaps, not Yunka framework defects.

## Authorization contract

YU-20 adds the active organization-scoped permission:

```text
identity.users.manage
resource = identity.users
action   = manage
scope    = organization
```

The permission is granted only to `system-administrator` at organization scope.

It is deliberately **not** added to any development `localauth` profile.

The three internal plans are:

```text
identity.members.create
identity.members.disable
identity.members.credentials.reset
```

Each plan declares:

```text
tenantRequired = true
authentication = [jwt]
permissions    = [identity.users.manage]
transaction    = local
idempotency    = none
boundary       = local
HTTP/RPC       = none
```

The plans are registered through `localmemberadmin.OperationPlans()` and intentionally do not enter the service-operation dictionary. YU-26 owns BFF exposure.

Because authentication is exactly `jwt`, service-token and development API-key principals cannot enter the YU-20 application operation.

### Durable guard

The YU-20 `OperationGuard` does not trust `Principal.Roles` or the upstream Decision alone. Before application invocation it re-verifies:

- authenticated JWT human Principal;
- active User in the Principal tenant;
- active Organization;
- active organization-scoped `system-administrator` RoleBinding, direct or through an active Team membership;
- active `identity.users.manage` role grant;
- organization allowed scope;
- exact operation input type;
- exact operation / permission / grant scope coherence.

Only then is the authorized organization written into a typed scope context. Member application inputs never carry a trusted organization ID.

`humanauthz` already ignores `Principal.Roles` and derives grants from durable User/RoleBinding rows. Its query also requires `users.status = 'active'`, so disabling a member removes the durable grant on the next authorization check even if an old JWT still exists. Session/JWT revocation policy is intentionally deferred to YU-21/YU-22/YU-25.

## User lifecycle CAS

YU-20 migration adds:

```text
users.revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
```

Existing Users upgrade to revision `1`.

Semantics:

- Create member -> User revision `1`.
- Disable member -> requires exact expected active User revision and increments it by one.
- Reset credential -> first requires exact active User revision and increments it by one, then applies the expected credential revision CAS in the same root transaction.
- If credential CAS fails, the preceding User revision increment rolls back too.

### Persistent disable-CAS invariant

YU-20 installs and verifies a SQLite `BEFORE UPDATE OF status` trigger requiring every `active -> disabled` User transition to also set:

```text
NEW.revision = OLD.revision + 1
```

This closes the concrete pre-YU-20 `identitybinding.Resolver.DisableUser` bypass: after YU-20 migration that compatibility API attempts to change status without advancing revision and SQLite rejects the write. A regression test invokes that real API and proves the User remains `active` at revision `1`.

The migration verifies the physical trigger definition from `sqlite_master`; a same-name no-op trigger is rejected.

## Last active system-administrator invariant

YU-19 permanently disables anonymous re-bootstrap after initialization. Therefore disabling the final active system administrator would create an unrecoverable administrative lockout.

YU-20 protects this at two levels:

1. application transaction checks whether the target User is an active system administrator and whether another active administrator exists;
2. a SQLite `BEFORE UPDATE OF status` trigger rejects a CAS-correct direct SQL transition that would disable the final active system administrator.

The trigger recognizes both direct User RoleBindings and active Team-based organization administrator bindings.

The migration verifies the trigger definition and rejects a same-name no-op trigger.

The direct-SQL regression explicitly supplies `revision = revision + 1` so the test proves the last-administrator rule independently from the separate revision-CAS trigger and does not rely on SQLite trigger execution order.

## Independent identity semantics

`CreateInput` accepts only profile data and a password; organization scope comes from the authorized Principal.

The operation always creates a fresh User ID and then a credential bound to that User. Email and display name have no uniqueness/merge semantics in this operation.

The YU-20 regression creates two members with identical email and display name and verifies:

```text
2 User rows
2 distinct User IDs
2 independent credential rows
```

This preserves the existing identity rule that profile data is not an identity key.

## Disable semantics

`Disable` requires:

```text
UserID
ExpectedRevision >= 1
```

The SQL predicate includes:

```text
organization_id = authorized Principal tenant
status = active
revision = expected revision
```

Outcomes are normalized to:

- `ErrMemberNotFound` for a missing/cross-tenant target;
- `ErrMemberDisabled` for an already-disabled member;
- `ErrMemberRevisionConflict` for stale revision;
- `ErrLastAdministrator` when the operation would remove the last active administrator.

No credential row is deleted. YU-21 login must require an active User in addition to a matching credential.

## Credential reset semantics

`ResetCredential` requires:

```text
UserID
ExpectedUserRevision >= 1
ExpectedCredentialRevision >= 0
Password
```

`ExpectedCredentialRevision = 0` supports creating a local credential for an existing User that does not yet have one; positive values replace an existing credential with CAS.

The operation:

1. advances the active User revision with CAS;
2. calls the YU-18 credential repository in the same root `*sql.Tx`;
3. records success audit;
4. stages transactional Outbox;
5. commits once.

Any credential revision conflict rolls back the User revision advance.

## Audit and Outbox

Successful member operations use configuration/security audit classification because the current audit enum has no separate identity-management category.

Success audit carries only stable facts:

- human actor User ID;
- organization scope;
- target User ID;
- User revision;
- credential revision;
- transport/correlation metadata already admitted by the audit boundary.

Outbox topic/type values are:

```text
identity.members
identity.member.created
identity.member.disabled
identity.member.credential-reset
```

Payload contains only:

```text
organizationId
userId
userRevision
credentialRevision
```

It contains no password, password hash, salt, email, display name, token, session or CSRF material.

Post-rollback audit classification is exact for the three YU-20 operation IDs:

```text
category = configuration
reason   = identity.transaction_rolled_back
```

Unregistered `identity.*` operation strings do not gain that classification by prefix inference.

## Runtime wiring

Startup order now includes:

```text
identitycore
-> localcredential
-> localbootstrap
-> localmemberadmin
-> configrevision
-> audit
```

Production authorization composes the member guard and existing delivery guard through an operation-keyed resolver mux.

At application bind time, the YU-20 Manager receives:

- the existing SQLite database;
- YU-18 credential repository;
- the existing audit store;
- the existing transactional Outbox capability;
- the same RecordingExecutor used by other protected application operations.

`Application.MemberAdministration()` is an in-process port only. No new HTTP/gRPC/MCP route is registered.

## Executable regressions authored

`internal/localmemberadmin/memberadmin_test.go` covers:

- real YU-19 bootstrap administrator -> durable human grant -> YU-20 operation chain;
- two same-profile members remain independent Users/credentials;
- password reset invalidates the old password and advances User/credential revisions;
- disable advances User revision and preserves credential revision;
- forged `Principal.Roles=system-administrator` cannot authorize a regular User;
- authorization denial changes no User/Outbox state and records a denied audit;
- cross-tenant User cannot be disabled;
- stale User CAS leaves User/credential/Outbox unchanged;
- stale credential CAS rolls the User revision advance back;
- success-audit failure rolls User/credential/Outbox back;
- Outbox failure rolls User/credential/success-audit back and leaves only the post-rollback failure audit;
- password/profile/token/session/CSRF sentinels are absent from audit/Outbox;
- last active system administrator cannot be disabled through Manager or a CAS-correct direct SQL update.

`internal/localmemberadmin/migration_test.go` covers:

- upgrade of existing Users to revision `1`;
- repeatable migration ledger;
- exact system-administrator-only grant and organization scope;
- revision-CAS trigger presence;
- last-administrator trigger presence;
- real `identitybinding.Resolver.DisableUser` bypass is blocked after YU-20 migration;
- forged/no-op revision trigger is rejected;
- forged/no-op last-administrator trigger is rejected.

`backend-yunka/yu20_permission_dictionary_test.go` locks:

- resource and permission dictionary semantics;
- system-administrator-only grant;
- no development compatibility permission;
- exactly three internal operation plans;
- plan validation, JWT-only auth, local transaction and no transport bindings;
- no accidental service-operation dictionary exposure.

`internal/audit/yu20_rollback_classification_test.go` locks exact rollback classification and no prefix-based identity classification.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent ancestry | PASS by branch compare against `4bad8f3cb77e6e4b5f2899b891bfe964e186e9cf` |
| Framework boundary | PASS by branch diff: Yunka framework/gitlink unchanged |
| Protobuf/generated boundary | PASS by branch diff: no protobuf/generated artifact changed |
| Durable JWT authority source | PASS by source review: `humanauthz` reads active User/RoleBinding grants and ignores `Principal.Roles` |
| Development API-key boundary | PASS by source review: `identity.users.manage` absent from `localauth` permission profiles |
| Service-token boundary | PASS by internal plan contract: authentication is exactly `jwt` |
| User disable CAS | PASS by source/migration review: revision predicate + persistent revision trigger |
| Legacy direct disable bypass | PASS by authored real-API regression: `identitybinding.DisableUser` is rejected after YU-20 migration |
| Last administrator invariant | PASS by source/migration review and authored application/direct-SQL regressions |
| Atomic success path | PASS by source review: User/credential/audit/Outbox join one Yunka root local transaction |
| Sensitive event payload review | PASS by source review: no password/profile/token/session/CSRF in audit/Outbox payload |
| YU-20 executable tests | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: execution container cannot resolve/materialize the GitHub repository/module path |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | no usable task status checks were available during YU-20 authoring |
| Canonical generation/full check | NOT RUN; protobuf/generated inputs were not changed |

Environment/tooling absence is neither behavioral RED nor PASS.

## Framework issue disposition

No Yunka compiler, generator, Executor, transaction, authorization or Outbox defect was reproduced.

YU-20 uses existing Yunka public seams:

- `operation.Executor`;
- `execution.TransactionHandleFrom`;
- `authz.GrantAuthorizer` / `OperationGuard`;
- `outbox.TransactionalStore`.

No new Yunka framework Issue is required and framework source remains unmodified.

## Residual boundary and next task

YU-20 intentionally does **not**:

- verify a local password as a login operation;
- create a server-side session;
- issue or verify an internal JWT;
- implement current-member/logout/self-password-change;
- expose local-auth BFF routes;
- add member-management UI.

The next independent task is `YU-21`: authenticate a local member password against the YU-18 credential, require an active durable User, create a server-side opaque session, issue/verify a short-lived internal JWT, and ensure an unverified JWT can never create a Principal.