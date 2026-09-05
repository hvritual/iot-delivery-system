# YU-19 one-time administrator bootstrap evidence

> Document class: **EVIDENCE**
> Task: `YU-19`
> Fixed consumer parent: `3e9f9b4a07edd65b0dca12d206e2a41aadd1f3ab`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-20`

## Result

YU-19 adds an application-owned, one-time local administrator initialization capability on top of the YU-18 local credential repository. It does not add password login, session/JWT issuance, BFF auth routes, general member administration or UI.

The implementation closes the anonymous-bootstrap window with one durable, irreversible singleton latch and performs the successful initialization in one Yunka root `local` transaction:

```text
bootstrap latch claim
+ User
+ organization-scoped system-administrator RoleBinding
+ Argon2id local credential
+ system audit
= one root commit
```

Any application error after the latch claim causes the root Executor to roll back every staged row.

## Fixed-parent gap

### Expected

After YU-18, the consumer had a safe local credential persistence capability but still needed a first-administrator transition with these invariants:

1. an empty identity system can be initialized exactly once;
2. repeated or concurrent attempts cannot create multiple administrators;
3. after successful initialization, disabling or deleting the administrator must never reopen anonymous initialization;
4. an upgraded deployment that already contains a User must not expose a new anonymous privilege-escalation window;
5. User, administrator role binding, credential, permanent bootstrap closure and success audit must commit atomically;
6. the bootstrap mechanism must not fabricate a human Principal before any human credential has been established.

### Observed at fixed parent

The fixed parent contained `identitycore` User/RoleBinding, YU-18 `localcredential`, audit persistence and the Yunka root transaction capability, but no bootstrap-state schema and no one-time administrator initialization operation.

The absence is a consumer capability gap, not a Yunka framework defect.

## Irreversible bootstrap state

YU-19 introduces `iotd_local_admin_bootstrap_state` with one possible singleton ID:

```text
id = local-admin
state = closed
```

An **absent row** is the only open state. A durable row is always closed and has one of two reasons:

- `initialized`: this deployment successfully created its first local administrator; the row retains the historical organization and User IDs;
- `preexisting_identity`: the system already had a User before anonymous local-administrator bootstrap could safely run.

There is no SQL transition from closed back to open.

### Physical immutability

The schema installs both:

```text
BEFORE UPDATE -> RAISE(ABORT)
BEFORE DELETE -> RAISE(ABORT)
```

for the bootstrap-state table.

Migration verification does not trust trigger names alone. Every run reads the trigger definition from `sqlite_master` and requires the correct UPDATE/DELETE event plus the canonical `RAISE(ABORT, 'local administrator bootstrap state is immutable')` action. A same-name no-op trigger therefore fails migration verification.

The migration also verifies the state-table column contract and any already persisted closed row rather than trusting a migration-ledger entry.

## Upgrade and late-identity safety

Anonymous bootstrap is safe only while the entire identity system is empty.

YU-19 enforces this twice:

1. `ApplyMigrations` checks `users` and writes an immutable `preexisting_identity` closure when an upgraded database already contains any User;
2. `Manager.Initialize` re-checks the global User count inside the root transaction immediately before bootstrap. If a User appeared after migration but before first initialization, the Manager commits `preexisting_identity` and rejects initialization.

The second check covers late OIDC or other provisioning races between startup migration and first bootstrap attempt.

Once `preexisting_identity` is committed, later deleting that User does not reopen initialization because eligibility is determined from the irreversible latch, not the current User count.

## Internal operation contract

YU-19 defines the internal operation:

```text
identity.local-admin-bootstrap.initialize
```

The plan is a standalone valid `operationplan.SchemaVersion=2` contract:

- domain: `identity`;
- application: `local-admin-bootstrap`;
- typed request/response names;
- `transaction = local`;
- `idempotency = none`;
- composition boundary `local`;
- no HTTP binding;
- no RPC binding;
- no authentication or permission declaration;
- `operation.Protected(plan) == false`.

This is intentional. Before the first administrator exists there is no human Principal that can legitimately pass normal human authorization. The durable one-time latch is the authorization fact for this internal transition.

The plan is not inserted into the normal permission dictionary because it is not an authenticated role-granted operation and has no public transport.

## Yunka root UnitOfWork

The runtime constructs a dedicated `operation.NewExecutorWithOptions(nil, Transactions: transactionFactory)` for the unprotected internal bootstrap operation.

This does **not** create a consumer-owned transaction implementation. The operation still enters the fixed Yunka Executor, which creates the root `local` execution scope and owns commit/rollback.

Inside that root scope:

- bootstrap code requires `execution.TransactionHandleFrom(ctx)` to be the application SQLite `*sql.Tx`;
- latch claim, User insert and RoleBinding insert use that transaction directly;
- YU-18 `localcredential.SetPassword` detects the active Yunka execution and reuses the same transaction handle;
- `audit.SQLiteStore.AppendInTransaction` receives that exact transaction;
- one Executor commit finalizes all state.

No nested `database.BeginTx` or second UnitOfWork is introduced.

## Successful initialization

On an empty identity system and active pre-provisioned organization, `Initialize`:

1. confirms no closed latch;
2. confirms zero existing Users;
3. confirms the target organization is active;
4. allocates User, RoleBinding and audit identifiers;
5. claims the singleton latch as `closed / initialized`;
6. inserts an active User;
7. inserts an active organization-scoped `system-administrator` RoleBinding for that User;
8. stores the password through YU-18 Argon2id `localcredential.SetPassword(..., expectedRevision=0)`;
9. appends the successful bootstrap audit in the same SQLite transaction;
10. returns to the Yunka Executor for the one root commit.

YU-19 intentionally assumes the organization already exists. It does not create tenants or organizations. Because YU-19 exposes no public transport, the organization identifier is currently supplied only through an in-process management port. A later public auth route must bind organization scope from trusted server context/configuration rather than raw client input.

## Audit actor semantics

Successful initialization is recorded as a **system** action, not as an action by the newly created administrator:

```text
event_category = system
actor_type     = system
actor_id       = local-admin-bootstrap
operation      = identity.local-admin-bootstrap.initialize
result         = success
reason_code    = bootstrap.initialized
scope          = organization
Target         = identity.user / <new-user-id>
```

This avoids inventing a human Principal before the password credential exists.

Audit metadata contains only stable non-secret facts (`bootstrap_state=closed`, `role=system-administrator`). Password bytes are never placed in audit metadata/diff, bootstrap state or SQL text arguments outside the YU-18 password-hash boundary.

## Failure atomicity

The YU-19 regression suite authors executable cases for failures after the latch could have been claimed:

- Argon2id random-source/hash preparation failure;
- forced audit insert failure.

Both are expected to leave:

```text
Users        = 0
RoleBindings = 0
Credentials  = 0
Bootstrap    = open / no state row
Audits       = 0
```

because the Yunka root Executor rolls back the shared SQLite transaction.

The password sentinel tests also inspect durable credential/audit/bootstrap text/blob surfaces and require the supplied plaintext to be absent.

## Repeated and destructive-state behavior

After one successful initialization:

- a repeat attempt returns `ErrAlreadyInitialized`;
- disabling the administrator User does not reopen bootstrap;
- disabling its RoleBinding does not reopen bootstrap;
- deleting the RoleBinding, credential and User does not reopen bootstrap;
- SQL UPDATE/DELETE of the latch is rejected by immutable triggers.

Bootstrap availability therefore depends only on the irreversible historical latch, not on the current health or existence of the original administrator.

## Concurrency behavior

The singleton latch is claimed with a primary-key insert inside a root SQLite transaction. Concurrent initialization attempts therefore cannot commit two closed-state claims and cannot produce two durable administrators.

The authored concurrency regression starts two initialization calls against a multi-connection SQLite database and requires the resulting durable state to contain exactly one administrator User, one administrator RoleBinding, one credential, one latch and one success audit. A retry after the race must be rejected as already initialized.

SQLite may surface lock contention to one or both simultaneous attempts; YU-19 does not hide database availability errors as a successful initialization. The invariant is **at most one concurrent success and exactly one durable bootstrap result once one attempt succeeds**.

## Runtime wiring

`bootstrap.Application.New` now runs migrations in dependency order:

```text
identitycore
-> localcredential
-> localbootstrap
-> configrevision
-> audit
```

It constructs the local credential repository and one-time bootstrap Manager from the application database and existing SQLite transaction factory.

`Application.AdministratorBootstrap()` is intentionally an in-process port only. YU-19 adds no HTTP, gRPC or MCP route and no UI. Transport exposure remains reserved for the later local-auth work.

## Executable regression authored

`internal/localbootstrap/localbootstrap_test.go` covers:

- full successful User/RoleBinding/credential/latch/audit result;
- password verification through the YU-18 repository;
- plaintext absence from durable credential/bootstrap/audit surfaces;
- retry after successful bootstrap;
- disable/delete of original administrator without reopening;
- direct latch UPDATE/DELETE rejection;
- preexisting identity closure;
- credential failure root rollback;
- audit failure root rollback;
- concurrent bootstrap producing at most one durable winner;
- no transport/protected security declarations on the internal plan.

`internal/localbootstrap/preexisting_identity_test.go` covers a User created **after** migration but before first Initialize, then deleted after the closure, proving the late-identity closure remains permanent.

`internal/localbootstrap/migration_test.go` covers:

- repeatable migration with one ledger row;
- empty identity system remains open after migration;
- malformed physical schema is not hidden by a forged ledger row;
- same-name no-op immutability triggers are rejected.

`internal/localbootstrap/plan_contract_test.go` requires the standalone bootstrap plan to pass the fixed Yunka `operationplan.Validate` contract.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent | PASS: YU-19 branch derives from `3e9f9b4a07edd65b0dca12d206e2a41aadd1f3ab` |
| Branch ancestry | PASS at implementation review: ahead-only, behind `0` |
| Framework boundary | PASS by branch diff: Yunka gitlink/framework source unchanged |
| Protobuf/generated boundary | PASS by branch diff: no protobuf/generated change |
| Public transport boundary | PASS by source/diff review: no HTTP/gRPC/MCP route added |
| Yunka Executor capability review | PASS by fixed framework source inspection: unprotected plans may use root local transaction with nil SecurityPhase |
| Root transaction source review | PASS: latch/User/RoleBinding/credential/audit converge on the Yunka-owned SQLite transaction handle |
| Irreversible latch source review | PASS: closed-only table + UPDATE/DELETE abort triggers + physical definition verification |
| Upgrade/late-identity source review | PASS: migration and Initialize both close on preexisting User state |
| YU-19 executable tests | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: no repository execution environment with the declared Go toolchain/module materialization is available in this connector session |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | no usable task status checks available at evidence authoring time |
| Canonical generation/full check | NOT RUN; protobuf/generated inputs were not changed |
| YU-18 x/crypto module checksum materialization | still NOT RUN/inherited from YU-18; YU-19 does not modify `go.mod` or `go.sum` |

Environment/tooling absence is neither behavioral RED nor PASS. The YU-19 RED is the fixed-parent absence of a durable one-time bootstrap state/operation, not the unavailable Go execution environment.

## Framework issue disposition

No Yunka compiler, generator, Executor, transaction, identity or operation-plan defect was reproduced.

The fixed Yunka Executor already supports exactly the needed primitive: an internal unprotected operation can still enter the root local UnitOfWork when its plan does not claim authentication/permissions. YU-19 consumes that public behavior and does not patch or bypass framework source.

No new Yunka framework Issue is created.

## Residual boundary and next task

YU-19 stops before ongoing administrator member management. It does not provide:

- authenticated administrator creation of additional members;
- member disable/credential reset operations;
- password login;
- session or JWT issuance;
- BFF local-auth routes;
- member-management UI.

The next independent task is `YU-20`: authenticated administrator create/disable/reset-member operations with durable human authorization, optimistic CAS, independent local credentials, audit/Outbox atomicity and no identity merging by owner/display/email fields.
