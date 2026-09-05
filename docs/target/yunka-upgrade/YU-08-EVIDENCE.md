# YU-08 Release/Sprint/Milestone list contract evidence

> Document class: **EVIDENCE**
> Task: `YU-08`
> Fixed consumer parent: `64d7ff2978d7bc070402e05aeecbe3b7206134c0`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-09`

## Result

YU-08 replaces the final three planning-list extension plans with canonical protobuf typed Operations:

| Operation | Permission | Request/response | Transports |
| --- | --- | --- | --- |
| `delivery.releases.list` | `delivery.releases.read` | `ListReleasesRequest` / `ListReleasesResponse` | REST `GET /api/releases`, generated gRPC `ListReleases`, MCP `delivery.list_releases` |
| `delivery.sprints.list` | `delivery.sprints.read` | `ListSprintsRequest` / `ListSprintsResponse` | REST `GET /api/sprints`, generated gRPC `ListSprints`, MCP `delivery.list_sprints` |
| `delivery.milestones.list` | `delivery.milestones.read` | `ListMilestonesRequest` / `ListMilestonesResponse` | REST `GET /api/milestones`, generated gRPC `ListMilestones`, MCP `delivery.list_milestones` |

All three plans are tenant-required, project-scoped, read-only, non-idempotent, local Operations accepting API-key, JWT, and service-token authentication. The canonical generated Operation count is now 16. The compatibility REST response shapes remain raw arrays; protobuf HTTP bindings were deliberately not added.

## Path and feature boundary

- Developer-owned changes: canonical protobuf source, three Application adapters, typed compatibility Operations, permission dictionary, additive SQLite authorization migration, project/object guard, development compatibility grants, MCP registration/handlers, and tests.
- Generator-owned changes: protobuf Go/gRPC, contract manifest/client/OpenAPI/OperationPlans, generated Assembly, Application port, policies, and RPC executor. No generated file was hand-edited.
- Forbidden paths unchanged: `third_party/yunka/**`, legacy `backend/**`, and `web/**`.
- Forbidden feature expansion absent: no item get/search/similarity contract work and no YU-09 behavior.

## RED to GREEN lineage

| Commit | Evidence |
| --- | --- |
| `98d8371bdba0de83ce3bb3902111e6a04497ee31` | RED: descriptor/plan count was 13 instead of 16; no-service list execution failed with `delivery extension service is not configured`. |
| `e6dde6f5817f71f9cbf140cb0937a366866f1234` | Generated three new canonical Operations using the fixed generator and a conformant create-operation ChangeSet. |
| `af328f13c9b885cd0cf309be1fe572e8607fede4` | Bound the generated-plan equivalence test to all 16 plans. |
| `4b518095a234199cc740ecae6484b344101eefe2` | Replaced the three legacy extension calls with typed `ExecuteTyped` plans and added service-token through a conformant existing-operation ChangeSet. |
| `d83a7453f1accd147e94a502adf4c494133aad4f` | Added durable project-scoped read permissions, migration, guard handling, MCP tools, and the human three-transport matrix. |
| `4d99165f54f96a6d60582bb18c916c1bdfed166a` | Closed the versioned dictionary/contract inventory at 16 RPC and 14 dictionary-backed MCP Operations. |
| `45cb720aa80ea2c28bb2e876a651c94802ffd240` | Proved service-token grants remain explicit per Operation and project for all three generated gRPC lists. |

The first create-operation ChangeSet used API-key and JWT because fixed Yunka serializes the accepted `service` CLI value differently from generated `service-token` semantics. After the structural commit, one multi-subject existing-operation ChangeSet explicitly allowed the three authentication deltas and passed with no scope or semantic violation.

## Authorization and persistence evidence

- `PlanningListAuthorizationMigrationID = S0-04-08_planning_list_authorization_v1` upgrades databases whose original authorization/service-operation ledgers are already present.
- The migration installs and revalidates the three active read permissions, exact project scopes, six built-in role grants, and three service-operation rows transactionally.
- No service grant is created by default. Generated gRPC service-token calls are denied before explicit per-operation/per-project grants, allowed only for the granted project, and denied for another project.
- Human JWT matrix covers an authorized project viewer, a second-tenant administrator, another project, and cross-tenant access over REST, generated gRPC, and MCP for every planning-list kind.
- Denied and allowed reads leave Release/Sprint/Milestone rows and Outbox snapshots unchanged.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused six-package Go suite | PASS: identitycore, delivery/application, deliveryauthz, mcpserver, bootstrap, delivery |
| Human REST/gRPC/MCP scope matrix | PASS for all three list Operations |
| Service-token generated gRPC scope matrix | PASS for all three list Operations |
| Full backend | `GOWORK=off go test ./... -count=1` PASS |
| Race | `GOWORK=off go test -race ./... -count=1` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain `yunka generate --full` runs PASS, zero Git diff after each |
| Canonical check | PASS: one service, 43 messages, 5 application files, modules and Assembly |
| Context | schema v4 root profile resolves the nested backend paths |
| Ownership | canonical proto is `developer-contract`; all checked production Go paths are `developer-code/editable` |
| Audit | PASS against fixed parent; one pre-existing `AUDIT-AUTH-001`, `new=[]`, `fixed=[]` |
| Dev plan | PASS; one `app` process resolves to `go run ./cmd/yunka-bootstrap` |
| Formatting/JSON | `git diff --check` and `jq empty` PASS |

## Framework issue disposition

The two confirmed fixed-version DX gaps are recorded in the framework repository using the project's evidence/problem format:

1. [yunka.io #149 — nested project ChangeSet/audit Git path domain](https://github.com/hvritual/yunka.io/issues/149)
2. [yunka.io #150 — add-operation service/service-token vocabulary](https://github.com/hvritual/yunka.io/issues/150)

The repository-root profile and two-ChangeSet sequence are bounded public consumer workarounds. They do not repair or modify Yunka.

## Residual boundary and next task

- This matrix injects an already-authenticated Principal at the post-auth boundary. Local password/session/JWT verification remains later work.
- Service-token list evidence is generated gRPC only; REST/MCP are not claimed as service-token transports.
- No deployment, remote branch deletion, or force update is part of YU-08.
- The next independent task is YU-09: item get/search/similarity contracts, using the final merged YU-08 SHA as its fixed parent.
