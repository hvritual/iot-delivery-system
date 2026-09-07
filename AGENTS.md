# Delivery engineering contract

The active application is `backend-yunka/`; `backend/` is frozen historical code.
The pinned framework under `third_party/yunka` is read-only for ARCH work.

## Ordered work

ARCH-01 establishes the executable inventory and five delivery gates.
ARCH-02 extracts the Delivery domain/persistence boundary without changing APIs,
CAS, authorization, root transactions, audit or Outbox semantics.
ARCH-03 organizes Identity and extracts concrete persistence from use cases.
ARCH-04 closes remaining ownership/compatibility debt and re-certifies integration.
Each task starts from the exact integrated parent. Do not report later stages as
completed merely because their rules or package ownership have been declared.

## Five gates

1. G1 / Project architecture: every production Go package has exact ownership in
   `.architecture/policy.json`. Unknown packages fail. All committed backend-yunka production
   files, including generated and inactive build-tag files, are parsed. The native
   build is also type-checked through canonical `go list` export data. Alias,
   receiver-name and method-value changes do not confer write-boundary privileges.
2. G2 / Developer agent: `.architecture/change.json` records the precise base,
   exact permitted files, invariants and counterexamples. Verify the actual diff,
   not a task author's PASS claim. Governance and business changes are separate.
3. G3 / Review agent: require GitHub APPROVE on the current head from a distinct,
   write-authorized identity that is not the PR author or a commit contributor.
   Missing/unlinked identity, stale approval, dismissal or changes requested blocks.
   An agent's own review document is not independent approval evidence.
4. G4 / Harness: before merge, rerun the live premerge checker. It reconciles the
   current PR/base, latest workflow attempts, required jobs (including YU-32) and server enforcement.
   Missing, skipped, cancelled, stale or unreadable evidence is not success.
5. G5 / Framework integration: preserve the exact framework gitlink and clean
   submodule. YU-30 canonical-full remains mandatory for double generation,
   full checks, ownership/audit and ChangeSet. Never hand-edit generated files or
   describe this consumer checker as a change to Yunka framework source.

## Commands

Run from a clean, committed checkout with the canonical Go/toolchain/submodule:

```sh
bash scripts/check-architecture.sh "$EXACT_BASE"
(cd tools/archgate && GOWORK=off GOTOOLCHAIN=local go test -race -count=1 ./...)
(cd scripts && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v test_delivery_gates)
python3 scripts/delivery_gates.py premerge --repo hvritual/iot-delivery-system --pr "$PR_NUMBER"
```

The last command requires `GH_TOKEN` with repository metadata, actions, pull-request
and administration READ access. It performs no merge. It must be
executed using the established checker from the integration base, not an unchecked
replacement from the PR. During initial installation the proposed checker itself
requires independent review. Merge tooling must use the same exact head and must
not substitute an old successful receipt after either ref changes.

## Baseline and trust limits

Existing findings are visible frozen debt, not a declaration of good architecture.
An import finding is retained only while its whole file is byte-identical to the
fixed baseline. Typed write-boundary violations are never grandfathered. Changing such a file requires removing its prohibited dependencies;
adding another SQL use to an existing import cannot silently expand the baseline.
There is no update-baseline or skip flag. New package ownership is introduced in a
separate reviewed control task before business code uses it. Do not relax a rule
and modify governed code in the same task.

The typed pass certifies the recorded native GOOS/GOARCH; static imports cover all
committed variants. It is not whole-program dynamic dataflow or universal security
proof. A green inventory does not mean zero retained debt or zero runtime defects.

Branch settings remain an administrator-controlled trust boundary. The premerge
checker refuses to claim a hard remote merge lock when main is unprotected or
required checks/reviews cannot be verified. Classic strict branch protection,
required named checks, stale-review dismissal and administrator enforcement must
be established externally; this repository change does not grant administration
permission. The final `Architecture / delivery-ready` context itself is required;
requiring only its prerequisite contexts is insufficient. Hosted checks may use
the optional `ARCH_GATE_READ_TOKEN` secret, scoped to this repository with those
read-only permissions. The default workflow token may lack administration read:
a 403 remains BLOCKED, never a reason to remove the protection check. Supply this
credential only after reviewing the initial checker; do not commit its value.
Alternative ruleset enforcement requires its own verified reader, not
an automatic fallback. Never bypass a failed gate to finish this installation.

Private framework analysis documents are not republished by these tasks. Report
code changes, exact-source tests, remaining debt and incomplete enforcement
separately. Keep integration staged and reversible; do not migrate production data.
