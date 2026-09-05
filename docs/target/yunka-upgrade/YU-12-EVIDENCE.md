# YU-12 saved view and member-week contract evidence

> Document class: **EVIDENCE**
> Task: `YU-12`
> Fixed consumer parent: `1e0459766008515c0c6352031065d9040169ad84`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-13`

## Result

YU-12 moves `delivery.views.save`, `delivery.views.list`, and `delivery.members.week` from handwritten extension plans into canonical protobuf-owned typed contracts.

| Operation | Generated RPC | Permission | Transaction | Public mapping |
| --- | --- | --- | --- | --- |
| `delivery.views.save` | `SaveView` | `delivery.views.write` | local | REST + MCP |
| `delivery.views.list` | `ListSavedViews` | `delivery.views.read` | read_only | REST + MCP |
| `delivery.members.week` | `GetMemberWeek` | `delivery.members.read` | read_only | REST + MCP |

The generated contract now contains 22 operations and 58 messages. All three consumer methods use `operation.ExecuteTyped`; their former `executeServiceExtension` branches are absent. The obsolete development-only `delivery.items.write` alias was removed; the read alias remains only for the three YU-13 extensions.

## Published RED to GREEN lineage

| Published commit | Evidence |
| --- | --- |
| `e76251bddebd4c843ff5084397de69353729f2ed` | RED: descriptor and generated plan count remained 19 and lacked `SaveView`, `ListSavedViews`, and `GetMemberWeek`. |
| `2c4041880d88d788c08709f92ceaa979407a44e3` | GREEN: typed contracts, generated artifacts, adapters, permission dictionary, incremental SQLite migration, scope filtering, canonical UserID ownership, and regressions. |
| `e5e2f7176025d572b515d09f2a80e9f96b8215af` | Adds the production durable-grant regression for canonical owner isolation, cross-project save denial, zero denial side effects, and member-week filtering. |

The implementation tree `d4be338cf7a85cd7278d0d7ec34c565e69b82c34` and scope-regression tree `d131217b811e01beec8964642c062dc1d073fc07` are byte-identical to their tested local trees.

## Identity and authorization evidence

- Saved view owner is derived only from authenticated Principal.UserID; caller input and display-like Subject values cannot select the owner.
- Two JWT principals with the same Subject but different UserIDs cannot list one another's views.
- Service principals have no human UserID and are rejected from save/list personal-view operations; the generated view operations advertise API-key and JWT authentication only.
- View and member operations use dedicated project-scoped permissions. Production guards derive allowed projects from durable grants.
- Saving a view with an explicit unauthorized project filter is rejected; saved-view listing hides explicit project filters that are no longer authorized.
- Member-week output is filtered by the same trusted authorized-project set before transport serialization.
- The additive migration installs 3 permissions, 3 service-operation rows, 18 exact built-in-role grants, and its own immutable ledger row for already-upgraded databases.

## Transport and compatibility evidence

- Generated gRPC descriptors, application port, operation policies, executor, TypeScript client, OpenAPI, manifest, and assembly all include the three operations.
- Existing REST and MCP handlers now enter the typed application boundary without changing their public routes or tool names.
- The public MCP authorization inventory increases from 16 to 19 canonical operations; only project progress, project schedule, and notifications remain extension-owned for YU-13.
- Existing lifecycle, REST, MCP, SQLite, audit, and Outbox behavior remains green.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused RED/GREEN | PASS |
| Generated descriptor/plan bijection | PASS: 22 RPCs and 22 plans |
| Typed operations without legacy service | PASS |
| Canonical UserID owner isolation | PASS |
| Incremental authorization migration | PASS |
| Full backend | `GOWORK=off go test ./...` PASS |
| Race | `GOWORK=off go test -race ./...` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with zero tracked drift |
| Canonical check | PASS: one service, 58 messages, 5 application files, modules and Assembly |
| Audit | only the pre-existing `AUDIT-AUTH-001` finding remains |
| JSON / formatting | strict JSON decode and `git diff --check` PASS |

## Framework issue disposition

No new framework defect was reproduced. The fixed Yunka source remained unmodified and clean, so YU-12 creates no framework Issue.

## Residual boundary and next task

- Project progress, project schedule, and notifications list remain extension operations assigned to YU-13.
- No YU-13 contract or notification behavior is included here.
- YU-13 must use the final merged YU-12 SHA as its fixed parent.
