# YU-09 item read contract evidence

> Document class: **EVIDENCE**
> Task: `YU-09`
> Fixed consumer parent: `d6ac8403ec88d9d108e997394b05cee3c6ca7173`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-10`

## Result

YU-09 replaces the final three independent item-read extension plans with canonical protobuf typed Operations:

| Operation | Permission / scope | Request / response | Transports |
| --- | --- | --- | --- |
| `delivery.items.get` | `delivery.work-items.read` / object | `GetItemRequest` / `WorkItemResponse` | REST, generated gRPC, MCP `delivery.get_work_item` |
| `delivery.items.search` | `delivery.work-items.read` / project | `SearchItemsRequest` / `SearchItemsResponse` | REST, generated gRPC, MCP `delivery.list_work_items` |
| `delivery.items.similarity` | `delivery.work-items.read` / project | `FindSimilarItemsRequest` / `FindSimilarItemsResponse` | REST, generated gRPC, MCP `delivery.find_similar` |

All three are protected, tenant-required, read-only local Operations accepting API-key, JWT, and service-token authentication. The generated Operation count is 19. `delivery.items.list` remains a generated gRPC contract but no longer claims the REST/MCP surfaces that execute the richer search contract.

## RED to GREEN lineage

| Commit | Evidence |
| --- | --- |
| `eff339d774ca129f04170d55b262c3f27e6a912f` | RED: generated descriptor/plan count was 16 instead of 19; item Get without the legacy extension service failed with `delivery extension service is not configured`. |
| `544e169ccdff671e0fccb9a724407f54dc4e5a9e` | GREEN: generated three typed contracts; replaced extension execution; added DTO adapters, durable service-operation migration, scope guard, MCP get, transport authority, and regression coverage. |
| `601aaa47128c57e731f71035b8c2182857505aee` | Scope closure: production REST/MCP search, object get, similarity, project isolation, and zero-Outbox behavior. |

Every published commit tree is byte-identical to the locally tested commit tree.

## Behavior and authorization evidence

- Exact get resolves the persisted item's project on the server before grant evaluation; a caller cannot provide a substitute project ID.
- Search preserves project, board, owner, status, kind, release, sprint, milestone, and normalized keyword filters. With no explicit project, only the durable authorized-project set reaches the adapter result.
- Similarity preserves kind defaulting, exact-title marking, 0.55 threshold, score ordering, stable ID tie-break, default limit 5, and maximum limit 20. An explicit project is ownership-checked before execution; an omitted project cannot expose project-owned candidates outside the trusted set.
- The additive `S0-05-09_item_read_authorization_v1` migration installs or revalidates the three service-operation rows without creating any default service grant.
- Allowed and denied reads leave business rows and Outbox state unchanged.

## Verification ledger

| Gate | Result |
| --- | --- |
| Focused item read RED/GREEN | PASS |
| Production object/project scope | PASS |
| REST/MCP production matrix | PASS |
| Generated gRPC/MCP DTO equivalence | PASS |
| Full backend | `GOWORK=off go test ./... -count=1` PASS |
| Race | `GOWORK=off go test -race ./... -count=1` PASS |
| Vet | `GOWORK=off go vet ./...` PASS |
| Module tidiness | `GOWORK=off go mod tidy -diff` PASS, zero diff |
| Frontend regression | `npm test` PASS, 16 files / 45 tests |
| Frontend types | `npm run typecheck` PASS |
| Canonical generation | two consecutive fixed-toolchain full generations PASS with an unchanged generated diff digest |
| Canonical check | PASS: one service, 49 messages, 5 application files, modules and Assembly |
| JSON / formatting | `jq empty` and `git diff --check` PASS |

## Framework issue disposition

Fixed Yunka again reproduced the existing authentication vocabulary mismatch: new-operation scaffolding accepts `service`, while canonical generated semantics use `service-token`. The consumer used the established two-step workaround and did not modify framework source. Reproduction and verification were added to [yunka.io #150](https://github.com/hvritual/yunka.io/issues/150#issuecomment-5548683856). No new framework issue was discovered.

## Residual boundary and next task

- Local password/session/JWT verification remains later work.
- Service-token behavior is represented and registered but this task does not create default service grants.
- No deployment, force update, branch deletion, Dashboard/List repair, or combined Update/UpdateContext change belongs to YU-09.
- YU-10 must use the final merged YU-09 SHA as its fixed parent and address Dashboard/List same-ID plan divergence plus the composed Update/UpdateContext `requires_operations` contract.
