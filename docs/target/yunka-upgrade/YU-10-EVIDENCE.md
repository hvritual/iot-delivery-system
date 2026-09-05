# YU-10 canonical composition evidence

> Document class: **EVIDENCE**
> Task: `YU-10`
> Fixed consumer parent: `06b56247c4346c9d61c9fdadadace652de6883ba`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-11`

## Result

YU-10 removes both same-ID handwritten plan branches and makes Update plus UpdateContext a canonical local composition:

| Boundary | Before | Final |
| --- | --- | --- |
| `delivery.dashboard.get` | legacy branch rebuilt the same ID with `delivery.items.read` | always uses generated `OperationPlanGetDashboard` with `delivery.dashboard.read` |
| `delivery.items.list` | legacy branch rebuilt the same ID with `delivery.items.read` | always uses generated `OperationPlanListItems` with `delivery.work-items.read` |
| `delivery.items.update` | runtime appended the child operation after a separate pre-authorization | protobuf declares `delivery.items.update-context`; generated plan owns the dependency and permission closure |

The combined REST update now enters security once, starts one root local UnitOfWork, executes UpdateContext as one declared child, and commits or rolls back the two mutations together. Standalone generated gRPC and MCP update paths use the same expanded canonical root policy. Human and service-account resolution both require the complete permission set at the same durable object/project boundary; service accounts must hold separate explicit grants for the root and declared child operations.

## RED to GREEN lineage

| Published commit | Evidence |
| --- | --- |
| `56a3ef366490fc3f0eb8b4cd233dd18afe252520` | RED: Dashboard/List prepared alias permissions when a legacy service was attached; generated UpdateItem had no required operation. |
| `e05374d0132d8e607cafe3b9808246425e786f41` | GREEN: canonical plans only, protobuf-owned composition, one root authorization/UoW, multi-permission scope verification, explicit composite service-grant resolution, and regression coverage. |

Both published trees are byte-identical to the corresponding locally tested trees.

## Composition and rollback evidence

- `OperationPlanUpdateItem` contains `RequiresOperations=[delivery.items.update-context]` and `PermissionClosure=[delivery.work-items.context.update]`.
- Root permissions are the sorted union of `delivery.work-items.context.update` and `delivery.work-items.update`, with permission mode `all`.
- The application no longer mutates `Composition.RequiresOperations` at runtime and no longer executes a no-op child preflight.
- Behavioral instrumentation observes exactly one root plan, one child plan, and one root UnitOfWork begin for the combined update.
- Production REST tests prove both registered permissions are required, stale revision and denied requests leave SQLite and Outbox unchanged, and the successful operation advances the revision twice.
- Cross-transport revision races preserve exactly one winner and stable conflict classification.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused RED/GREEN | PASS |
| Canonical Dashboard/List plans with legacy service attached | PASS |
| Generated requires-operation and permission closure | PASS |
| Human multi-permission object scope | PASS |
| Composite service-operation grant mapping | PASS |
| Production REST/gRPC/MCP authorization and revision matrix | PASS |
| One root UoW and child join | PASS |
| Failure rollback and zero partial Outbox | PASS |
| Full backend | `GOWORK=off go test ./...` PASS |
| Race | `GOWORK=off go test -race ./...` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with identical diff hash `c791f03182480a1693752f5094fa2e04db6a6ec3` |
| Canonical check | PASS: one service, 49 messages, 5 application files, modules and Assembly |
| Audit | only the pre-existing `AUDIT-AUTH-001` finding remains |
| JSON / formatting | strict JSON decode and `git diff --check` PASS |

Go validation uses `backend-yunka/` as the module boundary with `GOWORK=off`, matching the established upgrade ledger. The repository root workspace still includes the comparison-only legacy backend whose obsolete replacement target is absent from the fixed Yunka checkout.

## Framework issue disposition

The default AX7 workflow reproduced a new deterministic framework defect: `change begin` writes `.yunka/change-contract.json`, while `change check` includes that same control file in the Git delta and rejects it as outside the declared scope. The issue was checked for duplicates and recorded in [yunka.io #151](https://github.com/hvritual/yunka.io/issues/151) using the project problem format. Consumer and generated framework source were not modified; canonical generation and full check still pass.

## Residual boundary and next task

- The compatibility alias dictionary remains only for extension-only operations assigned to later tasks; Dashboard/List no longer execute it.
- No YU-11 comment, context/ADR, gate, close, CAS, or separation-of-duty expansion belongs to this task.
- YU-11 must use the final merged YU-10 SHA as its fixed parent.
