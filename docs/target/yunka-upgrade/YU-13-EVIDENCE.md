# YU-13 project health and notification contract evidence

> Document class: **EVIDENCE**
> Task: `YU-13`
> Fixed consumer parent: `bbf88ab59cbe78c4ab0308d8f08cc1b4de74cdf8`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-14`

## Result

YU-13 moves project progress, project schedule, and notification listing from handwritten extension plans into canonical protobuf-owned typed contracts.

| Operation | Generated RPC | Permission | Scope | Public mapping |
| --- | --- | --- | --- | --- |
| `delivery.projects.progress` | `GetProjectProgress` | `delivery.projects.read` | project | REST + gRPC + MCP |
| `delivery.projects.schedule` | `GetProjectSchedule` | `delivery.projects.read` | project | REST + gRPC + MCP |
| `delivery.notifications.list` | `ListNotifications` | `delivery.work-items.read` | project | REST + gRPC + MCP |

The generated contract now contains 25 operations and 69 messages. All delivery operations are canonical; `executeServiceExtension`, `extensionPlan`, and the legacy `delivery.items.read` compatibility alias are absent from production code and configuration.

## Published RED to GREEN lineage

| Published commit | Tree | Evidence |
| --- | --- | --- |
| `1d4fb6e5dda2c24e7075b3970a024a7f9ec1d3af` | `e6b6272ebe9dc9e2fc8f37ff99a9e58010be3330` | RED: descriptor contained 22 RPCs while the contract test required 25 and the three generated methods. |
| `ab410fda1647fb5dffbd46b5babd315757c6824f` | `0f0e55282ebf49172b41531aa5eb2b0662df0693` | GREEN: generated contracts, typed application paths, durable authorization, scoped notification filtering, MCP registration, and regressions. |
| `20a7613d77a6cee959943170d89355cbe139028d` | `05060927f4e9e6ae3dde24622ac8ea1c21fe00ed` | Proves a forged YU-13 migration ledger cannot bypass missing immutable service-operation rows. |

Both published trees are byte-identical to their tested local commit trees.

## Authorization and filtering evidence

- Progress and schedule carry an explicit project ID. The production guard resolves durable tenant ownership and project grants before invoking the application.
- Notification requests carry no caller-controlled project scope. The guard derives the authorized project set from durable grants.
- The application resolves project-created notification subjects directly and work-item notification subjects through the delivery repository, then filters against the trusted authorized-project set.
- Limit is applied after authorization filtering. The production matrix seeds a newer unauthorized notification and an older authorized notification; a limit of one returns only the authorized subject across REST, gRPC, and MCP.
- Other-project and cross-tenant progress/schedule reads return the same permission-denied classification across all three transports.
- Denied and successful read matrices leave business state and Outbox unchanged.
- The additive SQLite migration installs and verifies the three immutable service-operation rows for already-upgraded databases; it fails closed if its ledger is forged while rows are absent.

## Compatibility evidence

- Existing REST routes remain `/api/projects/{project_id}/progress`, `/api/projects/{project_id}/schedule`, and `/api/notifications`.
- Existing MCP project-health tool names remain stable; `delivery.list_notifications` is added so notification behavior is transport-equivalent.
- Project progress, capacity, risks, timestamps, and notification fields are represented in generated protobuf, OpenAPI, TypeScript client, manifest, application port, policy, RPC executor, and Assembly artifacts.
- The fixed Yunka source and gitlink remain unchanged.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused RED | PASS: generated descriptor RPC count `22`, want `25` |
| Generated descriptor/plan bijection | PASS: 25 RPCs and 25 plans |
| Production project-scope matrix | PASS: REST/gRPC/MCP allow and deny equivalently |
| Notification post-filter limit | PASS: only the authorized project subject is returned |
| Incremental authorization migration | PASS: 3 service operations and one immutable ledger row |
| Full backend | `GOWORK=off go test ./...` PASS |
| Race | `GOWORK=off go test -race ./...` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test -- --run` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with zero tracked drift |
| Canonical check | PASS: one service, 69 messages, 5 application files, modules and Assembly |
| Audit | only pre-existing `AUDIT-AUTH-001` remains |
| JSON / formatting | strict JSON decode and `git diff --check` PASS |

## Framework issue disposition

No new framework defect was reproduced. The fixed Yunka source remained unmodified and clean, so YU-13 creates no framework Issue.

## Residual boundary and next task

- YU-13 stops before cockpit, IoT/TraceLink, reminder, and Obsidian projection regression work.
- YU-14 must use the final merged YU-13 SHA as its fixed parent.
