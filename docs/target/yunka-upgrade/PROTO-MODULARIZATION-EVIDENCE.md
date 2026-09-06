# Proto Contract Modularization evidence

- Fixed consumer parent: `0cce352a58d72a6b60d847e4009f1a9c0fe56ba7`.
- Fixed Yunka framework: `057ebcf88a87303eb633eb6e604d306f633dfac0`.
- Task branch: `codex/proto-contract-modularization`.
- Scope: physical protobuf modularization only; no DeliveryService split, operation semantic change, business implementation change, framework/gitlink change or deployment.

## Baseline

The business contract was concentrated in `backend-yunka/contracts/proto/iot_delivery.proto` (about 28.5 KB) with one `DeliveryService`, 25 canonical operations and all Delivery DTOs. `yunka_bootstrap.proto` was a 221-byte initialization placeholder whose own comment required deletion after real contracts exist.

The project profile already defines `backend-yunka/contracts/proto` as a directory-level `protoRoot`; the fixed generator therefore remained the authority for whether nested multi-file contracts are actually supported.

## Target layout

```text
backend-yunka/contracts/proto/delivery/v1/
  delivery_service.proto
  common.proto
  work_item.proto
  project.proto
  planning.proto
  dashboard.proto
  saved_view.proto
  notification.proto
```

Every business proto remains `package iot.delivery.v1` with the same `deliveryv1` Go package. `delivery_service.proto` remains the only `DeliveryService` and `delivery/v1` domain declaration.

## Executed framework qualification

A temporary carrier ran against the exact fixed consumer/framework SHAs with Go 1.25.13, protoc 21.12, protoc-gen-go 1.36.11 and protoc-gen-go-grpc 1.6.2.

The decisive run is GitHub Actions `34011698783`, job `101428541287`. The pinned framework accepted the nested layout and reported:

```text
GENERATED protobuf-go files=9
GENERATED contract services=1 messages=68 applicationFiles=5
GENERATED assembly bindings=1
OK protobuf-go files=9
OK contract services=1 messages=68 applicationFiles=5
OK assembly bindings=1
```

The generated `operation-plans.json` stayed byte-for-byte semantically identical with exactly 25 operations. All 68 real Delivery messages retained their full semantic model. The only removed message is `yunka.bootstrap.v1.Bootstrap`, matching the placeholder's documented lifecycle.

Two earlier carrier attempts were test-harness REDs, not framework/product defects: the first comparator incorrectly looked for operations in `manifest.json`; the second treated intended Bootstrap removal as a business-message regression. Both times pinned Yunka `generate` / `check` had already succeeded. The final verifier admits only that one placeholder deletion and otherwise fails closed.

The generated candidate source commit was `e39e8038e1e6e09a8399ab566b07e0237d4112f7`; subsequent documentation commits do not change contract or generated semantics and must still pass the normal PR regression stack on their own final SHA.

## Real integration RED: managed protobuf ownership manifest

The first full PR regression on `19db12d7ff59b2e58a65052db6d697019d735894` correctly failed after canonical generate/check. Yunka reported the expected nine protobuf-Go outputs, but the worktree became dirty only at:

```text
M .yunka/protobuf-go.json
```

Root cause: the initial carrier committed `backend-yunka/contracts/**` and therefore included the regenerated protobuf sources and contract manifest, but omitted Yunka's managed `.yunka/protobuf-go.json` ownership inventory. The committed inventory still named the old two monolithic generated files. This was a consumer delivery omission, not a framework multi-file defect; the YU-30 clean-tree gate behaved correctly.

A second exact-source carrier regenerated with the same pinned framework/toolchain and required the **only** drift to be `.yunka/protobuf-go.json`. It then verified the framework-produced inventory contains exactly these nine managed outputs before committing that single generated ownership file:

```text
common.pb.go
dashboard.pb.go
delivery_service.pb.go
delivery_service_grpc.pb.go
notification.pb.go
planning.pb.go
project.pb.go
saved_view.pb.go
work_item.pb.go
```

The repair commit is `17f8023611020ddc3a05fbec2ba94ff976a821ac`. The automatic write-back itself received GitHub `action_required` on follow-on PR workflows because it was authored by `github-actions[bot]`; that status is workflow-trigger governance, not a test result. This evidence update creates the final human/connector-authored task SHA so all canonical PR workflows execute again on the complete source tree rather than reusing the earlier partial PASS/RED.

## Framework disposition

Recursive/nested multi-file protobuf support is **not** a Yunka defect and no issue was filed for it.

A separate, evidenced DX gap remains: Yunka can discover/generate multiple files, but the current agent context exposes only the whole contract source root; generated message/service semantics do not provide a first-class operation/DTO-to-contract-file ownership relation; ChangeSet editable paths come from explicit plans rather than a canonical bounded contract-module context. This prevents the framework from using protobuf modularity to provide minimal AI context and fail-closed unrelated-module edits.

That framework-level productization is tracked as `hvritual/yunka.io#160` — **DX gap: multi-file protobuf contracts lack enforced module ownership and AI context scopes**. The consumer does not add an unenforced parallel `contract_modules` manifest and does not change the framework gitlink in this task.

## Required final gate

Before integration to `main`, the final task SHA must independently pass:

1. canonical Yunka generation/check and no unexpected generated drift;
2. YU-30 full regression, including ownership/audit/ChangeSet, Go test/vet/race, frontend checks and real browser E2E;
3. YU-31 real HTTP/gRPC/stdio MCP runtime and process-closure smoke;
4. YU-32 full/runtime review including YU-32H RED/GREEN security regressions;
5. exact-SHA artifact readback and zero unexpected worktree drift;
6. non-force integration only after re-reading unchanged `main` parent.

No deployment is part of this task.