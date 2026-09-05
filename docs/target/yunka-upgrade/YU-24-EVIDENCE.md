# YU-24 project RoleBinding administration evidence

> Document class: **EVIDENCE**
> Task: `YU-24`
> Fixed consumer parent: `37211ecb8978ee35cb89d97532b65c811f0583c0`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Reviewed implementation head before evidence: `4e73a767c02504400f86eb6fda9652fd51a10b63`
> Scope stop: before `YU-25`

## Result

YU-24 adds authenticated, in-process project RoleBinding administration over the existing durable identity and project-scope model. It does not create a role editor, a new authorization engine, a new transport surface, or a new session policy.

The write path is:

```text
verified YU-23 JWT human Principal
        -> principalauthz / humanauthz durable grants
        -> YU-24 dedicated OperationGuard
        -> only durable organization system-administrator accepted
        -> shared Yunka Executor + root SQLite transaction
        -> durable Project/User/role contract reread
        -> RoleBinding CAS mutation
        -> success audit + Outbox in the same transaction
```

The authorization effect path remains the YU-23 path:

```text
next protected request
        -> same JWT human Principal
        -> humanauthz rereads current active RoleBinding rows
        -> revoked binding no longer resolves a grant
        -> OperationGuard evaluates the reduced current grant set
```

Therefore project-role revocation takes effect on the next authorization request without waiting for access-token expiry. YU-24 deliberately does not invalidate the session itself; YU-25 owns centralized session validity.

## Fixed-parent RED

### Expected

The YU-24 task contract requires:

1. only a durable `system-administrator` may assign or revoke project roles;
2. target Project and User must be real, active where applicable, and owned by the same tenant;
3. only existing project-capable roles may be assigned;
4. role permission/scope facts must remain consistent with the canonical dictionary;
5. duplicate/concurrent assign and revoke must have explicit conflict/CAS semantics;
6. audit and Outbox failure must roll back RoleBinding state;
7. revocation must remove the durable grant on the next YU-23 authorization request;
8. no BFF route, UI or YU-25 session policy is introduced.

### Observed at the fixed parent

At `37211ecb8978ee35cb89d97532b65c811f0583c0`:

- `roles`, `role_permission_grants`, grant scopes and `role_bindings` existed as durable authorization facts;
- `identity.role-bindings.manage` already existed in the versioned dictionary but was `reserved`;
- the dictionary already associated that permission with `system-administrator` and `project-administrator` at project allowed scope;
- `role_bindings` had no revision/CAS field;
- no project-role assignment/revocation application operation existed;
- no dedicated guard restricted the management operation to organization-level `system-administrator`;
- YU-23 already reread active RoleBindings on every protected request, so the missing piece was durable administration, not another authorization resolver.

This is a consumer capability gap, not a reproduced Yunka defect.

## Canonical permission activation

YU-24 changes exactly the existing dictionary permission:

```text
identity.role-bindings.manage
resource = identity.role-bindings
action   = manage
scope    = project
status   = active
```

The source JSON remains the authoritative permission dictionary. No new permission ID, role ID or arbitrary permission editor is added.

For upgraded databases, the YU-24 migration accepts the historical durable `reserved` row only as an upgrade input, validates its resource/action/scopes, and advances that row to `active`. Fresh databases seed the active source contract directly.

The existing role grants remain:

```text
system-administrator  -> identity.role-bindings.manage / project allowed scope
project-administrator -> identity.role-bindings.manage / project allowed scope
```

This does **not** mean a project administrator may execute the YU-24 management operations. The dedicated application OperationGuard below is deliberately stricter.

## Internal Operation Plans

New package:

```text
backend-yunka/internal/localprojectroleadmin
```

Operations:

```text
identity.project-role-bindings.assign
identity.project-role-bindings.revoke
```

Both plans require:

```text
TenantRequired = true
Authentication = [jwt]
Permission     = identity.role-bindings.manage
PermissionMode = all
Transaction    = local
Idempotency    = none
Boundary       = local
HTTP/RPC       = none
```

They are not inserted into the service-operation transport dictionary and do not create BFF/gRPC/MCP management routes. YU-26 owns remote BFF exposure.

## Dedicated system-administrator guard

`localprojectroleadmin.OperationGuard` does not trust `Principal.Roles`.

It requires:

- authenticated `AuthMethodJWT` human Principal;
- canonical TenantID/UserID;
- an allowed authorization decision for `identity.role-bindings.manage`;
- a resolved grant whose role is `system-administrator` and whose binding scope is `organization:<tenant>`;
- a second direct SQLite reread proving an active Organization, active User and active organization-scoped `system-administrator` RoleBinding;
- active permission/grant-scope rows in durable storage.

A project-scoped `project-administrator` binding can resolve the underlying permission, but it cannot satisfy this dedicated organization-level system-administrator guard. A forged `Principal.Roles=[system-administrator]` is also ineffective because neither `humanauthz` nor this guard uses that snapshot as authority.

## Project and member ownership

Project ownership is not taken from caller input.

The manager calls the existing `delivery.SQLiteRepository.GetProject` inside the Yunka root transaction. Existing project persistence explicitly stores and decodes `organizationId`, so the project must reread as:

```text
Project.ID             == requested ProjectID
Project.OrganizationID == verified Principal.TenantID
```

A missing or cross-tenant project is normalized to `ErrProjectNotFound`.

Target User is queried by exact `(organization_id, id)` and must be `active`. Email and display name are not identity keys.

## Assignable role contract

Only roles with canonical `bindingScope=project` are assignable. Organization-scoped roles such as `system-administrator` are rejected as role targets.

Before assignment, YU-24 compares the durable role contract against the versioned dictionary:

- role binding scope;
- exact permission IDs granted by that role;
- exact grant allowed scopes;
- each permission's resource;
- action;
- status;
- permission allowed scopes.

Extra grants, missing grants, changed permission status, changed action/resource or widened permission scopes fail closed as `ErrRoleContractDrift`.

This prevents an out-of-band SQLite mutation from being propagated by the legitimate role-assignment application path.

## RoleBinding CAS and history

Migration:

```text
YU-24_project_role_binding_admin_v1
```

adds:

```text
role_bindings.revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1)
```

For the YU-24-owned subject shape only:

```text
scope_type = project
user_id IS NOT NULL
team_id IS NULL
```

persistent triggers enforce:

1. active -> disabled requires `revision = old + 1`;
2. disabled project-user bindings cannot be reactivated;
3. revision cannot be changed while status remains unchanged;
4. id/org/role/scope/user/team/created_at are immutable;
5. project-user RoleBinding history cannot be deleted.

The scope predicates are intentional. Organization RoleBindings and team RoleBindings retain their pre-YU-24 lifecycle semantics; YU-24 does not freeze unrelated aggregates.

### Assign semantics

A new assignment creates a new active row at revision 1.

An already-active tuple:

```text
organization + role + project + user
```

returns `ErrBindingAlreadyActive`.

A revoked row is never reactivated. Reassignment creates a new binding ID and preserves the disabled historical row.

### Revoke semantics

Revoke input is:

```text
BindingID + ExpectedRevision
```

The CAS update is constrained to an active, same-organization, project-scoped, user-subject binding and increments revision exactly once.

Stale revisions return `ErrBindingRevisionConflict`; an already disabled row returns `ErrBindingRevoked`.

## Concurrent behavior authored

The existing SQLite runtime serializes in-process writers with one open connection, WAL and a bounded busy timeout. YU-24 still tests semantic concurrency rather than relying on that implementation detail alone.

Authored tests launch real goroutines through the full manager/Executor transaction path:

- two simultaneous assignments of the same tuple: exactly one succeeds, one returns `ErrBindingAlreadyActive`, and exactly one active row remains;
- two simultaneous revocations with the same expected revision: exactly one succeeds, one observes revoked/stale state, final status is disabled at revision 2.

## Transactional audit and Outbox

The YU-24 runtime manager is constructed by the existing runtime binder with the same:

- RecordingExecutor;
- SQLite transaction factory;
- audit store;
- transactional Outbox capability;
- delivery repository.

No second transaction manager or authorization stack is introduced.

Success audit records:

```text
category = configuration
scope    = project
actor    = verified human UserID
operation = YU-24 assign/revoke operation
TargetType = identity.role-binding
TargetID   = binding ID
metadata   = binding revision, role ID, user ID, transport
```

No password, credential hash, session token, JWT or signing key is included.

Outbox topic/events:

```text
identity.project-role-bindings
identity.project-role-binding.assigned
identity.project-role-binding.revoked
```

Payload contains only binding/org/project/user/role/status/revision facts.

Because audit append detects the active Yunka transaction and Outbox uses `EnqueueTx`, RoleBinding state, success audit and business event commit atomically.

Authored failure regressions prove:

- forced audit insert failure rolls back assignment;
- forced Outbox insert failure rolls back assignment and its success audit;
- forced revoke audit failure leaves the binding active at its original revision.

The shared RecordingExecutor rollback classifier was also extended so YU-24 application rollbacks are recorded as `identity.transaction_rolled_back`, not incorrectly as delivery transaction failures.

## Revocation takes effect on next YU-23 request

The YU-23 human resolver queries active Organization, active User, active RoleBinding and active Permission on every grant resolution. It does not trust `Principal.Roles` and does not cache RoleBindings in the JWT.

YU-24 regression therefore uses the same real `humanauthz.Resolver` before and after revocation:

```text
assign contributor
-> target resolves delivery.work-items.create @ project:project-a

revoke binding
-> same target JWT Principal resolves zero matching grants on next call
```

This satisfies YU-24's immediate authorization-revocation requirement without claiming that the opaque session itself has been centrally invalidated.

## Bootstrap composition

`bootstrap.New` now applies the YU-24 migration after local-member administration migration and before local login session migration.

`configuredAuthorization` composes the existing guard mux as:

```text
YU-20 member administration guard
YU-24 project role administration guard
Delivery operation guard
```

A dedicated bootstrap regression confirms both YU-24 operation IDs resolve to the YU-24 guard through the real configured authorization path.

The runtime binder creates the YU-24 manager using the same shared security Executor and application capabilities, then exposes only the in-process:

```text
Application.ProjectRoleAdministration()
```

No transport route is registered.

## Review findings and corrections

The task review found and corrected consumer issues rather than manufacturing framework defects:

1. **Permission dictionary formatting churn** — an early whole-file write created a large format-only diff. The original formatting was restored; the final dictionary delta is one semantic status change.
2. **Dictionary/source-of-truth mismatch candidate** — an intermediate design left source JSON `reserved` while activating only SQLite. Because `contracts/authorization/dictionary.go` declares JSON authoritative, source status was corrected to `active`; migration still upgrades historical durable `reserved` rows.
3. **Over-broad RoleBinding triggers** — an early trigger form applied CAS/immutability to every RoleBinding. It was narrowed to YU-24 project-user bindings and a regression proves an organization binding is not frozen.
4. **Incomplete role-contract validation** — an early check compared grant edges/scopes but not the referenced permission row. The manager now verifies permission resource/action/status/allowed scopes as well.
5. **Rollback audit category drift** — YU-24 rollback originally fell through to the shared Delivery classification. It is now explicitly classified as an identity rollback.
6. **Historical-row deletion gap** — update immutability alone allowed a direct DELETE to erase revoked history. A scoped delete trigger now preserves project-user RoleBinding history.

These are all consumer-layer findings.

## Authored regression inventory

### `localprojectroleadmin`

- assignment and revocation alter durable grants on the next resolution;
- duplicate assignment conflict;
- stale revision conflict;
- repeated revoke classification;
- revoked-row history and new-ID reassignment;
- cross-tenant Project rejection;
- cross-tenant User rejection;
- disabled User rejection;
- organization/unknown role rejection;
- project-administrator self/other elevation rejection despite permission and forged Principal roles;
- role grant drift rejection;
- permission status drift rejection;
- permission allowed-scope drift rejection;
- audit/Outbox failure rollback;
- concurrent assign and revoke winners;
- migration repeatability and reserved->active upgrade;
- persistent bypass/CAS/history triggers;
- trigger scope does not overreach organization RoleBindings;
- forged migration ledger cannot hide a malformed revision column.

### bootstrap / audit / dictionary

- real configured authorization resolves both YU-24 dedicated guards;
- rollback audit uses identity classification;
- active dictionary permission and existing canonical role grants are locked;
- internal plans have no remote bindings.

## Execution status

The executable environment was retried with:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
```

and returned:

```text
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Therefore executable verification is recorded exactly as:

```text
YU-24 tests          AUTHORED, NOT EXECUTED
go test ./...        NOT RUN
go test -race ./...  NOT RUN
go vet ./...         NOT RUN
```

Environment failure is not RED and no PASS is fabricated.

## Generation and dependency drift

YU-24 intentionally does not modify:

- `go.mod` / `go.sum`;
- protobuf contracts;
- generated assembly/contracts/client/OpenAPI files;
- `third_party/yunka`;
- Yunka framework source;
- BFF routes;
- UI.

The permission dictionary is handwritten application authorization source, not a generated artifact.

## Framework disposition

No new Yunka framework defect was reproduced.

YU-24 uses existing public framework seams:

- Principal context;
- GrantResolver / GrantAuthorizer;
- OperationGuard;
- Executor security phase;
- root local transaction;
- transactional Outbox.

No framework Issue is created and the fixed Yunka gitlink remains unmodified.

## Next boundary

YU-25 owns centralized session validity across member disable, administrator password reset and role revocation.

YU-24 stops after its branch is reviewed and fast-forwarded to `main`; it does not start YU-25.
