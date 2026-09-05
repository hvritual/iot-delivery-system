# YU-14 cockpit, projection, and reminder regression evidence

> Document class: **EVIDENCE**
> Task: `YU-14`
> Fixed consumer parent: `a27ab46c1584c1294e0cd9926f2199833f22cdd0`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-15`

## Result

YU-14 completes the executable regression matrix for the delivery cockpit, IoT bindings, engineering trace links, SQLite persistence, due reminders, and the one-way Obsidian projection.

The regression exposed three consumer-owned field-loss defects:

1. the UI editable representation omitted `IoTBinding.attributes`;
2. the UI editable representation omitted `TraceLink.recordedAt`, causing a later save to replace the original evidence time;
3. the Obsidian IoT table omitted `IoTBinding.attributes`.

The UI now preserves optional attributes as deterministic JSON in a fourth IoT column and preserves the trace evidence timestamp in a sixth trace column. Existing three-column IoT and five-column trace input remains accepted. The Obsidian validation projection now emits deterministic JSON attributes without becoming a second source of truth.

## Published RED to GREEN lineage

| Published commit | Tree | Evidence |
| --- | --- | --- |
| `46ed6a5e498966b059796b88ee5b1b68abeb284f` | `ae5eb085456c63eab818ccd5497e28bd9fbc4d62` | RED: UI round trips lost IoT attributes and trace time; Obsidian validation omitted IoT attributes. |
| `7e9a9e18460ef75649ee38dc2296191c627333ca` | `6684681eb61b6928d23addd0e24ad20a526bcb10` | GREEN: consumer UI encoding and Obsidian projection preserve those fields. |

Both published trees are byte-identical to their tested local commit trees.

## Zero-loss matrix

| Boundary | Executable evidence | Result |
| --- | --- | --- |
| Generated DTO | create input -> application adapter -> dashboard output preserves IoT kind/reference/label/attributes and trace kind/reference/title/URL/status/recorded time | PASS |
| SQLite | create -> close database -> reopen -> get preserves all IoT and trace fields | PASS |
| Cockpit model | dashboard normalization preserves nested IoT and trace DTO values | PASS |
| UI edit | stringify -> parse preserves attributes and recorded time; legacy shorter rows remain compatible | PASS |
| Obsidian | validation note contains IoT attributes and all trace fields including URL and recorded time | PASS |
| Reminder | first SQLite-backed scheduler queues one stable event; a new repository/store/scheduler after restart queues zero duplicates and observes one durable row | PASS |
| Projection replay | duplicate work-item events reproject the same current materialized state without duplicate archive entries | PASS |

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused RED | PASS: two UI round-trip failures and one Obsidian projection failure reproduced |
| Focused GREEN | PASS: delivery, application adapter, Obsidian, cockpit, and presentation regressions |
| Full backend | `GOWORK=off go test ./...` PASS |
| Race | final `GOWORK=off go test -race ./...` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test -- --run` PASS, 16 files / 45 tests |
| Frontend presentation | `node --test tests/*.test.mjs` PASS, 10 tests |
| Frontend types | `npm run typecheck` PASS |
| Frontend production build | `npm run build` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with zero tracked drift |
| Canonical check | PASS: one service, 69 messages, 5 application files, modules and Assembly |
| Audit | only pre-existing `AUDIT-AUTH-001` remains |
| JSON / formatting | strict JSON decode and `git diff --check` PASS |

The first full race attempt observed `SQLITE_BUSY` once in the pre-existing bootstrap seed test. The exact test passed five consecutive race runs, the complete bootstrap package then passed under race, and the final full repository race run passed. No YU-14 code participates in that bootstrap database initialization path, so no unrelated runtime change was made.

## Framework issue disposition

No Yunka compiler, generator, runtime, authorization, persistence, or control-plane defect was reproduced. The fixed Yunka source and gitlink remain unchanged. The confirmed defects were consumer presentation/projection omissions, so YU-14 creates no framework Issue.

## Residual boundary and next task

- YU-14 does not change protobuf contracts or generated artifacts.
- YU-14 does not modify transaction/Outbox ownership or write-path composition.
- YU-15 must use the final merged YU-14 SHA as its fixed parent and begins the separate root UnitOfWork plus transactional Outbox audit.
