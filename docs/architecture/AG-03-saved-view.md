# AG-03 — Saved-view use case and narrow persistence

> Class: DECISION / bounded implementation record
> Framework plan: `hvritual/yunka.io/docs/architecture/APPLICATION-GOVERNANCE-PLAN.md`
> Qualification/main identity: exact PR and attached Git/test receipts, not this source document

## Task and scope

Extract the existing SaveView/ListSavedViews pair from the monolithic delivery
Service. Do not add a Yunka Application, split the public DeliveryService, update
protobuf, upgrade the framework gitlink, migrate data, deploy, or rewrite the
frozen backend. Runtime/generator remains `057ebcf88a87303eb633eb6e604d306f633dfac0`.
The implementation base is IoT Delivery `f190fe237fa1efb738b4866275804093a209de5b`.

## Structure and real authority reduction

- `internal/delivery/domain/saved_view.go` owns the shared filter/value definitions,
  normalization and canonical-user sentinel. Existing delivery types are aliases;
  constants, Go source use, JSON fields and storage payloads remain compatible.
  Reflective type package paths follow their new domain owner; this is not a new
  persistence format or an independent schema source.
- `internal/delivery/ports/saved_view.go` has exactly CreateSavedView and
  ListSavedViews. The all-owner form is retained only for the existing ID collision
  scan; user-facing listing still derives its scope from the trusted UserID.
- `internal/delivery/application/savedview/internal/usecase/service.go` owns the
  two use cases, and the public owner Build exposes only their two methods.
  It imports no full Repository, concrete SQLite, global Service or PB transport.
- `internal/delivery/saved_view_service.go` is a compatibility/dependency adapter.
  Two bound functions form a separate non-embedded repository object. Merely
  assigning the 22-method Repository to a smaller interface is insufficient and
  fails the actual-method-set regression. The legacy Service remains wide for
  unrelated use cases; this pilot does not claim whole-application remediation.
- `internal/delivery/infrastructure/persistence/savedview` owns the saved-view SQL.
  A per-call resolver delegates to the existing SQLite executor/transaction-handle
  selection. It never starts or commits a transaction, owns no pool, and exposes
  no DB handle through the narrow Repository.

The compatibility facade builds a resource-free handler per invocation so the
existing test clock and shared atomic sequence remain identical. Other entity ID
allocation continues sharing that sequence. Validation order, ID prefix/retry
bound, global collision detection, sorting and error propagation are retained.
The ID scan is not newly certified as atomic across processes.

## Transaction and security preservation

The existing root Executor, trusted Principal, operation permissions, protobuf
mapping, audited application and transactionalRepository remain authoritative.
The narrow create function binds the already-transactional wrapper, not the raw
SQL object; Outbox staging still precedes persistence in the same root UoW.
Existing YU-15 commit, audit failure, Outbox failure and direct-write rejection
tests remain unchanged. Saved-view operations had no revision parameter; no CAS
is invented. Work-item/project CAS, SOD and unrelated flows are unchanged.

## Checks are not broadened file exemptions

No entry is added to implementationAllowlist and no existing test is removed.
The original consumer parser guard gains only two exact-path, canonical-import
and declared-receiver checks: facade SaveView through savedview.Application, and
hidden use-case CreateSavedView through ports.SavedViewRepository. Renamed aliases
are accepted; wrong caller, fake package and wide Repository are rejected. This
is a bounded parser check, not full Go type/dataflow proof; generic AG-04 remains
separate. Source dependency tests recursively cover the new leaf roles and reject
return of the saved-view methods to service.go.

All Go tests participate in existing full/race workflows. Three real overlay
compiler probes are wired into run-yu30-regression.sh: public owner succeeds,
hidden and aliased hidden imports fail for the precise internal-package reason.
No tracked source is edited by probes. Their qualified platform is Linux.

## Acceptance

1. Run the same new behavior-characterization file on the immutable pre-refactor
   base and candidate: identity/validation order, normalization, shared sequence,
   cross-owner collision, sorting, SQLite reopen and errors. A wrapped legacy
   ErrNotFound from the all-owner collision query still means an available ID;
   the owner-scoped public list error remains unchanged.
2. Execute the six saved-view TestAG03 parent groups plus the startup regression with no skips. Deliberately substituting
   the broad Repository must fail the actual-method-set test; restore the source.
3. Run full YU-30 (two generate/check cycles, Ownership/Audit/ChangeSet, Go tests,
   race/vet, frontend tests/typecheck/build/audit and real browser E2E), YU-31 real
   runtime smoke, and YU-32H existing RED/GREEN. Missing tools are INCOMPLETE.
4. Keep contracts/generated/module/assembly, go.mod/go.sum, framework gitlink and
   legacy backend unchanged; read back exact head/tree and clean worktree.
5. Independent review, non-force integration and actual-main verification remain
   distinct gates. Update the framework's STATUS/evidence after consumer acceptance.

## Rollback

Revert this entire batch, including alias definitions, SQL delegation, facade,
checks and script wiring. There is no schema/data migration or production action.
Do not carry only a half-migrated alias/adapter into another task. AG-04 should
consume this concrete narrowing example, not treat it as a generic analyzer.

## AG-03R — bounded SQLite startup correction (IoT Delivery #6)

Run 34221473016 passed the saved-view assertions but failed the unchanged
bootstrap seed/restart race test with SQLITE_BUSY during connection configuration.
The existing constructor set busy_timeout after the lock-sensitive journal_mode
pragma. The corrective commit only puts the existing 5000ms busy setting first;
there is no retry loop, timeout increase, transaction/schema change or Yunka fix.
A permanent real-file exclusive-lock regression must reproduce exact SQLITE_BUSY
on the immutable old constructor, then wait and succeed after lock release on the
corrected constructor. It reads back WAL, timeout, foreign keys and preserved data.
The original bootstrap test is repeated under race; all normal gates still apply.
Qualification and actual review/main outcomes remain separate from this decision.
