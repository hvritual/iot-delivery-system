# YU-07 evidence ledger — Project contract and typed project listing

## Result and immutable inputs

| Item | Value |
| --- | --- |
| Task | YU-07 — Project and `ListProjects` canonical protobuf typed contracts |
| Result | Complete at the task boundary; YU-08 is not started here. |
| Task parent | `9bf80993accbd11544652af8b1eb1c1961bc73fd` |
| Fixed framework | `057ebcf88a87303eb633eb6e604d306f633dfac0` |
| Framework mutation | None; the gitlink and materialized submodule are exact and clean. |
| Deployment | None. |
| Generation rule | All derived files were produced by the fixed canonical Yunka protobuf workflow; no generated file was hand-edited. |

YU-07 adds `ListProjects` as the thirteenth generated operation, moves the existing project-list behavior off its hand-written extension plan, makes `CreateProject` tenant-required, and adds a durable project-read permission/migration. The public REST and MCP compatibility contracts are preserved while generated gRPC uses the same typed plan and post-auth authorization boundary.

## Commit lineage and checkpoints

| Checkpoint | Commit | Parent | Purpose |
| --- | --- | --- | --- |
| RED | `3b84d2a420d1931087ee0717b5bb64126a388592` | `9bf80993accbd11544652af8b1eb1c1961bc73fd` | Executable tests require `ListProjects`, 13 generated plans, and typed execution without a legacy service extension. |
| Root-profile workaround | `3ce000a5e2b0ce014da0f89b53005315c9e53517` | `3b84d2a420d1931087ee0717b5bb64126a388592` | Track the repository-root Yunka profile needed for Git-backed ChangeSet/audit paths. |
| Structural contract | `137b5d9c8a0895f04ee7bdb377c2cce0143b206a` | `3ce000a5e2b0ce014da0f89b53005315c9e53517` | Canonically generate the `ListProjects` protobuf, application port, policy, RPC executor, and manifests. |
| Typed execution | `3c4656409e54c29adaa826f85dcada62d66f59a2` | `137b5d9c8a0895f04ee7bdb377c2cce0143b206a` | Route `Operations.ListProjects` through `OperationPlanListProjects`. |
| Authorization integration | `10b2698486ae5276c7936e0d75bde433bac1fde0` | `3c4656409e54c29adaa826f85dcada62d66f59a2` | Add durable permission/scope migration, guard filtering, service-grant behavior, two-tenant transport tests, and count updates. |
| Evidence and handoff | `e5a6d578b0b6264a7754ce47735960cfb89690fd` | `10b2698486ae5276c7936e0d75bde433bac1fde0` | Publish the YU-07 evidence, framework problem cards, and YU-08 dispatch boundary. |

An earlier version of this table contained unpublished local checkpoint object IDs. A fresh clone could not resolve them. The table above is the exact published, parent-linked lineage reachable from both `main` and `codex/yu-07-project-contracts`; the evidence-integrity repair that corrects the table is necessarily identified by its Git ref and delivery response rather than by self-reference inside its own content.

## Scope and path ledger

`git diff --name-status 9bf80993accbd11544652af8b1eb1c1961bc73fd..e5a6d578b0b6264a7754ce47735960cfb89690fd` contains 44 paths. They are limited to the repository-root Yunka workflow profile/workaround, `backend-yunka` Project contract/application/authorization/generated/test paths, and YU-07 documentation.

- Allowed developer-owned paths used: `.gitignore`, `.yunka/{project,providers,dev}.json`, root `go.mod`, `backend-yunka/Makefile`, `backend-yunka/README.md`, the canonical protobuf, Project-list application adapter, authorization dictionary/migration/guard paths, regression tests, and `docs/**` evidence/status files.
- Allowed generator-owned paths used: `.yunka/protobuf-go.json`, protobuf Go/grpc outputs, generated contract manifests/client/OpenAPI/operation plans, generated assembly, application port, operation policy/plan, and RPC executor.
- Forbidden paths with zero changes: `third_party/yunka/**`, legacy `backend/**`, and `web/**`.
- Forbidden feature expansion with zero additions: `ListReleases`, `ListSprints`, and `ListMilestones` protobuf RPCs/adapters/MCP tools; these remain YU-08 work.
- The evidence-integrity follow-up changes only this ledger, the YU-07 framework problem card, and the production shared-guard wiring test whose former name overstated per-operation coverage. It changes no production behavior or generated file.

## Contract and authorization result

| Requirement | Implemented evidence |
| --- | --- |
| Canonical operation | `delivery.projects.list` / `delivery.projects.read`, tenant required, read-only, no idempotency, local execution, API-key/JWT/service-token authentication. |
| Trusted scope | `ListProjectsRequest` is empty. Tenant and project visibility are derived from the authenticated Principal plus durable SQLite grants; no caller-supplied tenant/project scope exists. |
| Compatibility transports | REST remains `GET /api/projects`; MCP remains `delivery.list_projects`; no protobuf HTTP binding was added that would alter the existing raw REST response shape. |
| Generated execution | REST/MCP `Operations.ListProjects` and generated gRPC execute `OperationPlanListProjects`; the old `delivery.projects.list` extension plan is removed. |
| Permission dictionary | `delivery.projects.read` is active at project scope; all six durable roles receive the intended read grant; development profiles remain explicit compatibility data; service default grants remain empty. |
| SQLite migration | `S0-04-07_project_read_authorization_v1` upgrades fresh, old four-ledger, and intermediate databases; exact rows are revalidated even with a forged ledger; conflicting/extra state fails in one transaction. |
| Service identity | No implicit grant. A service identity is denied until `serviceauthz.Manager` creates a per-operation, per-project grant, after which only that project is visible. |
| Tenant isolation | Two authorized tenants were exercised across REST, generated gRPC, and MCP. Each sees only its own authorized set; an identity whose TenantID and durable User row disagree is denied on all three transports. |
| Rejection safety | Unbound and tenant-mismatched human calls are `403` / `PermissionDenied` / `permission_denied`; project rows and Outbox snapshots remain unchanged. Ungranted service-list denial also leaves Outbox unchanged. |

## RED → GREEN evidence

The RED commit was run before implementation. Its failures were contract/implementation failures, not missing-tool failures:

1. Contract/equivalence tests found no `ListProjects` protobuf RPC, application port, generated executor, or thirteenth plan.
2. The no-legacy-service operation test reached the former extension path and failed with `delivery extension service is not configured`.

After the structural and typed-execution checkpoints, the authorization integration made the focused set green:

```text
go test ./internal/delivery/application ./internal/deliveryauthz ./internal/identitycore ./internal/bootstrap ./internal/mcpserver . -count=1
PASS (all six packages)
```

The focused two-tenant positive/negative transport matrix also passed after independent review requested the second-tenant positive case:

```text
go test ./internal/bootstrap -run 'TestProductionProjectList(MatrixUsesOneDurableScopeAcrossRESTGRPCAndMCP|DenialMatrixPreservesProjectsAndOutbox)' -count=1
PASS
```

## Final verification ledger

| Gate | Command / observation | Result |
| --- | --- | --- |
| Full backend | `GOWORK=off go test ./... -count=1` | PASS, all packages |
| Race | `GOWORK=off go test -race ./... -count=1` | PASS, all packages |
| Vet | `GOWORK=off go vet ./...` | PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` | PASS, zero diff |
| Frontend regression | clean offline `npm ci`; `npm test` | PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` | PASS |
| Canonical repeatability | two separate `make -C backend-yunka yunka-generate ...` invocations | PASS; each completed, followed by zero diff in every generated path |
| Full framework check | `make -C backend-yunka yunka-check ...` | PASS: protobuf Go 2 files, one service, 37 messages, 5 application files, modules and assembly |
| Context | `make -C backend-yunka yunka-context ...` | PASS; repository-root profile resolves the backend contract/generated/module roots |
| Ownership | `yunka ownership check` over the canonical proto and all changed hand-written production Go paths | PASS; contract is `developer-contract`, Go is `developer-code` |
| Audit | `yunka audit --root . --base 3ce000a5e2b0ce014da0f89b53005315c9e53517 --format agent-json` | PASS; one pre-existing `AUDIT-AUTH-001` debt item, `new=[]`, `fixed=[]` |
| Dev plan | `yunka dev plan --root . --format agent-json` | PASS; resolves `app` as `go run ./cmd/yunka-bootstrap` in `backend-yunka` |
| Runtime smoke | fixed CLI with `GOWORK=off`, explicit development policy, temporary SQLite/Vault and a throwaway local key | Process reached the listening state and remained supervised until explicit SIGINT; this smoke is not used as credential or browser proof. Lifecycle/health/closure assertions remain covered by the full bootstrap tests. |
| Formatting | `jq empty` on the problem card and `git diff --check` | PASS |

The first unconfigured dev attempts failed before startup because the repository `go.work` references compatibility directories absent from the fixed submodule, then because fail-closed runtime environment/local-key settings were intentionally absent. These are configuration/environment observations, not behavioral RED evidence and not framework defects. The successful smoke used the documented explicit development configuration and temporary paths only.

## Framework issue disposition

Two confirmed fixed-version framework problems and their bounded consumer workarounds are recorded in [YU-07-FRAMEWORK-PROBLEMS.json](YU-07-FRAMEWORK-PROBLEMS.json):

1. nested-project ChangeSet/audit Git path-domain mismatch;
2. `add operation --authentication service` plan vocabulary versus generated `service-token` semantics.

The root-profile workaround passed context, canonical generation/check, ChangeSet planning/checking, dev planning, and final audit. The authentication workaround used two formal conformant ChangeSets: create `ListProjects` with API-key/JWT, then add `service-token` through the supported existing-operation allowance. Yunka framework source was not changed. Workaround success does not mean either framework issue is repaired.

The transient ChangeSet working IDs and attestations were not committed by the original run and are not recoverable from a fresh clone. This ledger does not invent replacements. The reproducible retained evidence is the published RED-to-GREEN lineage above, the canonical source and generated outputs, and the listed verification commands; future tasks must retain replayable ChangeSet identifiers when the framework exposes them.

## Evidence boundary and residual risks

- The transport matrix injects an already-authenticated JWT Principal at the post-auth boundary, then uses the production SQLite `GrantResolver`, `OperationGuard`, executor, handlers/RPC server/MCP tool, audit store, and Outbox. It is authorization evidence, not real token/session verification. The later local-credential/session tasks must prove password verification, opaque session handling, internal JWT signing/verification, revocation, and two-browser behavior independently.
- The generated application port is an internal executor-owned boundary. Like the pre-existing item-list adapter, a direct internal call without guard context is compatibility fail-open. No public REST/gRPC/MCP route performs that direct call; a later hardening task may make the internal port fail-closed once development `localauth` no longer depends on the compatibility path.
- Service identity project-list authorization is exercised through generated gRPC, the only supported service-token transport. It must not be described as service authentication over REST or MCP.
- Historical baseline documents under `docs/baseline/` and the source-baseline `BASELINE`/`CAPABILITY-MAP` retain their explicit as-of meaning and were not rewritten to pretend the original source already had 13 generated plans.
- This task does not implement Release/Sprint/Milestone list contracts, local member credentials, sessions, UI, final browser E2E, deployment, or the remaining program stages.

## Publish and next independent task

The authorized publication sequence is: push the isolated `codex/yu-07-project-contracts` task branch, then update `main` only if its verified head is still the task parent (fast-forward, no force and no branch deletion). Exact remote ref SHAs are reported by the delivery response and remain independently inspectable in Git.

The next sidebar task is YU-08, using the exact YU-07 `main` delivery SHA as its fixed parent. It must contract `ListReleases`, `ListSprints`, and `ListMilestones`, remove the remaining three planning-list extension plans, reach 16 generated operations, preserve create/list behavior, enforce project/tenant scope across transports, and stop before YU-09. The detailed dispatch contract is maintained in [TASKS.md](TASKS.md).
