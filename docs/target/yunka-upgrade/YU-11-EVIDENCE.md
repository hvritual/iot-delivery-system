# YU-11 governed write transport evidence

> Document class: **EVIDENCE**
> Task: `YU-11`
> Fixed consumer parent: `e3ef2b93fee8021526497ccdfe5b2e652d2d7ee3`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-12`

## Result

YU-11 seals the cross-transport regression matrix for the four governed delivery writes without changing production behavior:

| Capability | Expected transports | Proved invariant |
| --- | --- | --- |
| Comment | REST, generated gRPC, MCP | server-derived author/time; CAS conflict category; rejected write leaves item and Outbox unchanged |
| Context / ADR | REST, generated gRPC | canonical plan and generated ADR identity/time; CAS conflict category; rejected write leaves item and Outbox unchanged |
| Gate | REST, generated gRPC, MCP | evidence required; evidence persists; CAS conflict category; implementation and production identities remain distinct |
| Close | REST, generated gRPC, MCP | retrospective persists; CAS conflict category; implementer cannot close; independent reviewer can close |

The absence of an UpdateContext MCP tool is intentional and matches `CAPABILITY-MAP.md`; YU-11 does not add a new public transport. The matrix uses the production SQLite authorization resolver, operation guards, generated gRPC executor, REST handler, MCP server, local UnitOfWork, audit recorder, and transactional Outbox.

## Published implementation lineage

| Published commit | Evidence |
| --- | --- |
| `58558fbe6181c883ad92590003e4a1643a91add3` | Adds the governed-write transport matrix. Its tree `fe641ea8884c4dc7b47d113bd23c7a8624d0a3da` is byte-identical to the fully tested local tree. |

The fixed parent already contained the canonical typed contracts and production implementations. Correctly constructed behavioral probes passed, so no business, generated, schema, permission, or framework source change was warranted.

## CAS and rejection evidence

- Each stale revision is positive and was made stale by a committed prior mutation; revision `0` is separately understood as `invalid_expected_revision`, not misreported as a conflict.
- REST returns HTTP `409` with `revision_conflict`.
- Generated gRPC returns `Aborted` with `revision_conflict`.
- MCP returns a tool error with `revision_conflict`.
- After every stale CAS failure, the complete stored WorkItem and Outbox snapshot are deeply equal to their pre-request snapshots.
- Empty gate evidence is rejected at every applicable transport; the complete stored WorkItem and Outbox snapshot remain unchanged.

## Evidence and separation-of-duty evidence

- Comment author is derived from canonical Principal UserID (`scoped`), not request input; created time and committed aggregate revision are server-derived.
- REST and generated gRPC produce the same persisted plan and ADR fields, including a server-generated ADR ID and creation time.
- The implementation identity is captured from the JWT principal that advances `development_completed`.
- The same canonical identity receives `permission_denied` when attempting production validation or close over REST, generated gRPC, and MCP, with zero business/Outbox mutation.
- A second JWT identity (`reviewer`) can production-validate and close through each transport; the stored implementation and production-validation principals remain `admin` and `reviewer` respectively.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused governed-write matrix | PASS |
| Full backend | `GOWORK=off go test ./...` PASS |
| Race | `GOWORK=off go test -race ./...` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with zero tracked drift |
| Canonical check | PASS: one service, 49 messages, 5 application files, modules and Assembly |
| Audit | only the pre-existing `AUDIT-AUTH-001` finding remains |
| JSON / formatting | strict JSON decode and `git diff --check` PASS |

Go validation uses `backend-yunka/` as the module boundary with `GOWORK=off`. Generation and check use Go `1.25.13`, protoc `3.21.12`, protoc-gen-go `1.36.11`, protoc-gen-go-grpc `1.6.2`, and the fixed Yunka gitlink.

## Framework issue disposition

No new framework defect was reproduced. `third_party/yunka` remained fixed and clean, so no issue was created or updated for YU-11.

## Residual boundary and next task

- YU-11 adds regression evidence only; it does not add transports or change production policy.
- Saved views and member-week ownership semantics belong to YU-12 and were not changed.
- YU-12 must use the final merged YU-11 SHA as its fixed parent.
