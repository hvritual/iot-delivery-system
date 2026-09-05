# YU-17 config revision Executor alignment evidence

> Document class: **EVIDENCE**
> Task: `YU-17`
> Fixed consumer parent: `98df86da52a2948d64464dd99c643c7459342f7f`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-18`

## Result

YU-17 preserves the existing internal configuration-revision capability and validates it against the post-YU-16 runtime execution model. The existing implementation already had the required root UnitOfWork, CAS, audit, Outbox, durable authorization and three internal handwritten plans. One consumer-owned audit classification defect was confirmed and repaired.

### Existing capability confirmed

The following facts already existed at the fixed parent and are retained rather than reimplemented:

- `config.revisions.change`, `config.revisions.compare`, and `config.revisions.rollback` are handwritten internal `operationplan.Plan` values exposed through `ConfigOperationPlans()`;
- the permission dictionary registers all three operations at organization scope and the dictionary integrity gate appends `ConfigOperationPlans()` to the generated plan set before validating the complete canonical operation set;
- Change and Rollback use `transaction: local`; Compare uses `transaction: read_only`;
- the runtime binder constructs one shared `RecordingExecutor` with the application-owned SQLite transaction factory and passes that same executor to both delivery and config application operations;
- `configrevision.SQLiteStore` detects the active execution frame and reuses its `*sql.Tx` transaction handle;
- successful Change/Rollback append the immutable revision, success audit and transactional Outbox event before the one root commit;
- Compare is read-only and does not append a revision, audit success event or Outbox event;
- stale-parent CAS is enforced by conditional SQLite append and returns `configrevision.ErrRevisionConflict` without creating a gap;
- pre-YU-17 tests already covered configuration audit failure and Outbox failure rollback, immutable history, all four configuration kinds, persisted human/service authorization and concurrent CAS at the SQLite store layer.

YU-17 therefore does not introduce a second configuration service, second UnitOfWork, transport or UI.

## Confirmed consumer defect

### Expected

When the shared `RecordingExecutor` rolls back a failed canonical configuration write, the durable failure audit should remain in the **configuration** audit domain. A configuration failure queried by `event_category = configuration` must not disappear merely because delivery and configuration share one executor.

### Observed at fixed parent

`audit.SecurityRecorder.RecordApplicationRollback` hard-coded:

```text
EventCategoryDelivery
ReasonCode = application.transaction_rolled_back
```

for every local transactional operation.

The runtime binder passes this recorder-backed executor to `configapplication.Operations`. Therefore a failed `config.revisions.change` or `config.revisions.rollback` could roll back the business transaction correctly but then persist its post-rollback failure fact as a **delivery** audit event.

### Evidence / impact / boundary

- Evidence: fixed-parent consumer source in `backend-yunka/internal/audit/security.go` hard-coded `EventCategoryDelivery` in `RecordApplicationRollback`.
- Impact: configuration failures are misclassified in category-filtered audit queries and operational review; success and failure facts for the same configuration operation occupy different audit domains.
- Boundary: consumer-owned audit recorder behavior. No Yunka compiler, generator, Executor, transaction or Outbox defect is required to reproduce it.

## Repair

`RecordApplicationRollback` now derives its durable audit classification from the exact registered transactional config operation IDs:

```text
config.revisions.change   -> configuration / configuration.transaction_rolled_back
config.revisions.rollback -> configuration / configuration.transaction_rolled_back
all other operations      -> existing delivery / application.transaction_rolled_back
```

The classification intentionally uses exact operation IDs rather than a prefix rule, so an unregistered `config.revisions.*` string cannot acquire configuration semantics accidentally.

Delivery rollback classification remains backward-compatible.

Implementation commits:

- `62030b65489609b7e636b16decdb2b885dc6ea8b` — initial config rollback classification repair;
- `ee13c3b7572f3ffc955c85100843e19e77cd8e65` — tighten classification to exact canonical config write operation IDs.

## YU-17 executable regression authored

### Internal canonical plan registration

`backend-yunka/internal/configapplication/yu17_executor_alignment_test.go` locks exactly three internal config plans and verifies:

- IDs: change / compare / rollback;
- domain `config`, application `revisions`;
- authentication `jwt + service-token`;
- exact permissions;
- local/read-only transaction modes;
- `PermissionMode = all`;
- local composition boundary;
- no RPC or HTTP binding.

This complements the existing root permission-dictionary integrity gate, which merges `ConfigOperationPlans()` with generated plans and requires all dictionary operations to resolve to that combined canonical set.

### Runtime-shaped shared Executor

The same test constructs `audit.NewRecordingExecutor` over the real Yunka `operation.Executor` and SQLite transaction factory, then passes it to `configapplication.New` exactly as the runtime binder does.

It covers:

1. successful Change -> Change -> Compare -> Rollback;
2. Compare leaves revision/audit/Outbox counts unchanged;
3. committed Change/Rollback produce immutable revisions, success configuration audits and transactional Outbox events;
4. stale-parent CAS leaves revision and Outbox state unchanged and records one post-rollback **configuration** failure audit;
5. forced config success-audit failure rolls back the candidate revision and leaves only the post-rollback failure audit;
6. forced transactional Outbox failure rolls back the candidate revision plus in-transaction success audit and leaves only the post-rollback failure audit.

### Classification boundary

`backend-yunka/internal/audit/yu17_rollback_classification_test.go` locks:

- canonical config Change/Rollback -> configuration failure classification;
- unregistered `config.revisions.*` -> no special treatment;
- delivery write -> unchanged delivery failure classification.

Regression commits:

- `49d0020739bcf896a92c129b73765bcb5b2bd2a7` — runtime-shaped config revision executor regression;
- `49df6d6dcbe669906b409be3869934e0e1e18f65` — exact classification boundary regression.

## Existing regression evidence reused

The fixed-parent suite already contains executable tests for the underlying mechanisms; YU-17 does not duplicate them unnecessarily:

- `internal/configapplication/operations_test.go`
  - immutable Change/Compare/Rollback chain;
  - transactional config Outbox event shape without payload leakage;
  - Outbox failure rollback;
  - audit failure rollback;
  - stale CAS with no success-side effects;
  - persisted human and service authorization;
  - all configuration kinds;
  - rollback failure paths;
- `internal/configrevision/sqlite_test.go`
  - stale parent rejection;
  - concurrent append: exactly one winner / one conflict with no revision gaps;
  - participation in Yunka SQLite transactions and forced rollback.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent | PASS: branch created from `98df86da52a2948d64464dd99c643c7459342f7f` |
| Branch ancestry | PASS at implementation review: ahead-only, behind `0` |
| Framework boundary | PASS: Yunka gitlink remains `057ebcf88a87303eb633eb6e604d306f633dfac0`; no framework source modified |
| Generated/protobuf boundary | PASS by branch diff: no protobuf or generated artifact change |
| Internal canonical plan inspection | PASS by source review: exactly three `ConfigOperationPlans()` and dictionary integration retained |
| Root Executor inspection | PASS by source review: runtime binder passes shared `RecordingExecutor` to config operations |
| SQLite root transaction inspection | PASS by source review: config store uses execution transaction handle for active local/read-only operations |
| Runtime-shaped YU-17 tests | AUTHORED, NOT EXECUTED in this connector session |
| Existing config application regression | PRESENT, NOT RE-EXECUTED in this connector session |
| Existing concurrent CAS regression | PRESENT, NOT RE-EXECUTED in this connector session |
| `go test ./...` | NOT RUN: execution container cannot resolve `github.com` to materialize repository/dependencies |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | repository has no usable status checks for this task at evidence authoring time |
| Canonical generation/full check | NOT RUN; canonical protobuf/generated inputs were not changed |

Environment/tooling unavailability is neither behavioral RED nor PASS. The RED for this task is the fixed-parent hard-coded rollback category, not the inability to execute Go in the current connector session.

## Framework issue disposition

No new Yunka framework defect was reproduced. The confirmed defect is entirely inside the consumer audit recorder. Yunka framework source and gitlink are unchanged, so no new framework Issue is created.

Existing framework issues remain independent of YU-17.

## Residual boundary and next task

YU-17 stops before local member credentials. It does not add password schema, password hashing, login/session issuance, BFF local-auth routes or UI.

The next independent task is `YU-18`: add the application-owned local credential schema/repository associated with the existing User identity, with slow password hashing, repeatable migration, no plaintext persistence/logging and explicit hash-parameter upgrade policy.
