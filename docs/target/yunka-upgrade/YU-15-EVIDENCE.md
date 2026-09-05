# YU-15 root UnitOfWork and transactional Outbox evidence

> Document class: **EVIDENCE**
> Task: `YU-15`
> Fixed consumer parent: `cea2a11c02f3b4324bebc700d8214de44237c930`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-16`

## Result

YU-15 audits the registered delivery write paths against the existing root `operation.Executor` transaction boundary and closes two consumer-owned defects without modifying Yunka framework source or generated contract artifacts.

1. `delivery.views.save` was declared with `transaction: local`, but `Service.SaveView` reached `Repository.CreateSavedView` without the transactional Outbox staging used by the other registered delivery mutations.
2. the shared transactional Outbox stager hard-coded every mutation to topic `delivery.work-item`. Project, Release, Sprint and Milestone mutations already carried aggregate-specific event types, so their non-WorkItem IDs could be delivered to the WorkItem projection subscriber, which resolves `Envelope.Subject` as a WorkItem ID.

The runtime now constructs `NewRootTransactionalService`, which wraps only the remaining saved-view repository write seam and stages `delivery.saved-view.saved` before the repository write. The same stager is still used by all existing service mutations. Outbox routing now derives the topic from the event type aggregate while preserving the established `delivery.work-item` payload contract.

## Root transaction model

The existing consumer/framework seam already provides the required transaction ownership:

- generated write plans use `Execution.Transaction = local`;
- the root operation Executor owns the transaction factory;
- the SQLite delivery repository reuses the execution transaction handle when present;
- the SQLite audit store reuses that same execution transaction handle;
- the transactional Outbox stager requires `execution.TransactionHandleFrom(ctx)` and calls `EnqueueTx`;
- a direct mutation that reaches transactional staging without a root execution transaction therefore fails closed.

YU-15 does not introduce a second UnitOfWork or nested transaction owner.

## Consumer defects and repair

### Saved-view Outbox bypass

Fixed-parent behavior:

```text
delivery.views.save plan: transaction = local
Operations.SaveView -> audited application -> Service.SaveView
Service.SaveView -> Repository.CreateSavedView
(no transactional Outbox staging)
```

YU-15 behavior:

```text
Operations.SaveView
  -> root operation Executor / local transaction
  -> audited application
  -> Service.SaveView
  -> transactionalRepository.CreateSavedView
       -> transactional Outbox Stage
       -> Repository.CreateSavedView
  -> audit Append
  -> one root commit
```

If Outbox staging fails, the repository write is never attempted. If audit append fails after business and Outbox staging, the root Executor is expected to roll back both because all three use the same transaction handle.

### Aggregate topic mismatch

Fixed-parent behavior:

```text
delivery.project.created   -> delivery.work-item
delivery.release.created   -> delivery.work-item
delivery.sprint.created    -> delivery.work-item
delivery.milestone.created -> delivery.work-item
```

The Obsidian projection subscribes to `delivery.work-item` and resolves the event subject using `Service.Get`, so a Project/Release/Sprint/Milestone ID is not a valid WorkItem projection subject.

YU-15 routing is now:

```text
delivery.work-item.*  -> delivery.work-item
delivery.project.*    -> delivery.project
delivery.release.*    -> delivery.release
delivery.sprint.*     -> delivery.sprint
delivery.milestone.*  -> delivery.milestone
delivery.saved-view.* -> delivery.saved-view
```

The historical WorkItem JSON payload remains `workItemId + updatedAt`. Other aggregate topics use `aggregateId + updatedAt`.

## Regression coverage authored

`backend-yunka/internal/delivery/application/yu15_transactional_saved_view_test.go` covers:

- saved-view success commits business row + Outbox + audit;
- forced Outbox staging failure leaves saved view, Outbox and audit empty;
- forced audit append failure leaves saved view, Outbox and audit empty after root rollback;
- direct saved-view mutation outside the root execution transaction fails closed with zero business/Outbox residue.

`backend-yunka/internal/delivery/outbox_stager_topic_test.go` locks aggregate topic routing and rejects an unscoped delivery event type.

Existing pre-YU-15 transactional tests already cover WorkItem commit/rollback behavior and rejected writes leaving business/Outbox state unchanged; YU-15 adds the missing saved-view and aggregate-routing coverage rather than replacing those tests.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent / branch ancestry | PASS: YU-15 branch started at `cea2a11c02f3b4324bebc700d8214de44237c930`; compare showed behind `0` before evidence commits |
| Scope review | PASS: implementation changes are consumer-owned delivery/bootstrap code plus tests; no protobuf/generated file or Yunka gitlink change |
| Generated SaveView plan inspection | PASS: `delivery.views.save` remains `transaction: local` |
| Transaction-path source audit | PASS: repository, audit store and Outbox stager converge on the root execution transaction handle |
| Saved-view rollback regression | AUTHORED, NOT EXECUTED in this connector session |
| Aggregate topic regression | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: current execution container cannot resolve `github.com` to materialize the connected repository/dependencies |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | NOT AVAILABLE: repository has no Actions workflow and the branch commit has no CI status checks |
| Canonical generation / full check | NOT RUN; no protobuf or generated artifacts were changed |

Environment/tooling unavailability is not classified as a behavioral RED or PASS. The executable regression tests remain in the repository for the next environment that can run the Go 1.25 toolchain and dependencies.

## Framework issue disposition

No new Yunka compiler, generator, transaction, Outbox, authorization or control-plane defect was reproduced during YU-15. The defects closed here are consumer-owned write-path and event-routing defects. Yunka source and the fixed gitlink remain unchanged.

Existing Yunka issues `#149`, `#150`, and `#151` remain open and are not reclassified as YU-15 findings. No new framework Issue is created.

## Residual boundary and next task

- background Outbox dispatch state, reminder scheduling, projection consumption and notification delivery remain post-commit/background lifecycle concerns and are not moved into the business root transaction;
- config revision change/compare/rollback remains reserved for YU-17;
- YU-16 is the next independent task and owns accepted/denied/failure/rollback audit coverage plus sensitive-data redaction.
