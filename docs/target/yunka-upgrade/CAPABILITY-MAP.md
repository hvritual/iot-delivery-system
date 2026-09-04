# YU-00 消费者能力与 canonical 化映射

> 基线：`iot-delivery-system@4cd1209f41575a67ab143b83e1df6501089085b8`
>
> 用途：防止升级只覆盖生成 RPC，而遗漏真实 HTTP/MCP/服务扩展、身份授权、事务、投影和内部配置能力。
> 框架要求本体由独立任务 YU-00F 产出；本文件只描述消费者事实与迁移目标。

## 1. 12 个已生成 Delivery RPC

| Operation ID | 用例 | 事务 | 权限 | 当前 transport | 源证据 | 目标处理 |
| --- | --- | --- | --- | --- | --- | --- |
| `delivery.dashboard.get` | get_dashboard | read_only | `delivery.dashboard.read` | gRPC + REST | proto `17-29`; plan JSON `5-33` | 保留生成 plan；删除同 ID 扩展重建路径 |
| `delivery.items.list` | list_items | read_only | `delivery.work-items.read` | gRPC + REST + MCP | proto `30-42`; plan JSON `150-178` | 保留生成 plan；删除 legacy alias 路径 |
| `delivery.items.create` | create_item | local | `delivery.work-items.create` | gRPC + REST + MCP | proto `43-55`; plan JSON `121-149` | 保留行为、CAS/Outbox/guard |
| `delivery.items.update` | update_item | local | `delivery.work-items.update` | gRPC + REST + MCP | proto `56-68`; plan JSON `179-207` | 保留组合更新的根 UoW |
| `delivery.items.comment.create` | add_comment | local | `delivery.work-items.comment.create` | gRPC + REST + MCP | proto `69-81`; plan JSON `92-120` | 保留 expectedRevision |
| `delivery.items.update-context` | update_item_context | local | `delivery.work-items.context.update` | gRPC + REST | proto `82-94`; plan JSON `208-236` | 为组合调用声明 requires_operations |
| `delivery.items.advance-gate` | advance_gate | local | `delivery.work-items.gate.advance` | gRPC + REST + MCP | proto `95-107`; plan JSON `34-62` | 保留证据、状态机和职责分离 |
| `delivery.items.close` | close_item | local | `delivery.work-items.close` | gRPC + REST + MCP | proto `108-120`; plan JSON `63-91` | 保留生产验证和复盘约束 |
| `delivery.projects.create` | create_project | local | `delivery.projects.create` | gRPC + REST + MCP | proto `121-133`; plan JSON `266-294` | 合同化 tenant/scope 与持久化 |
| `delivery.releases.create` | create_release | local | `delivery.releases.create` | gRPC + REST + MCP | proto `134-146`; plan JSON `295-323` | 与 list 操作成对 canonical 化 |
| `delivery.sprints.create` | create_sprint | local | `delivery.sprints.create` | gRPC + REST + MCP | proto `147-159`; plan JSON `324-352` | 与 list 操作成对 canonical 化 |
| `delivery.milestones.create` | create_milestone | local | `delivery.milestones.create` | gRPC + REST + MCP | proto `160-172`; plan JSON `237-265` | 与 list 操作成对 canonical 化 |

上述源证据均相对于 `backend-yunka/contracts/proto/iot_delivery.proto` 和 `backend-yunka/contracts/generated/operation-plans.json`。

## 2. `operations.go` 的全部手写 Delivery 计划

### 2.1 13 个独立扩展 Operation ID

| # | Operation ID | 方法/能力 | 当前权限 | 事务 | 当前 transport | 证据行 | canonical 目标阶段 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `delivery.projects.list` | ListProjects | `delivery.items.read` | read_only | REST + MCP | `operations.go:206-220` | Phase 4 |
| 2 | `delivery.items.similarity` | FindSimilar | `delivery.items.read` | read_only | REST + MCP | `223-237` | Phase 5 |
| 3 | `delivery.items.get` | Get | `delivery.items.read` | read_only | REST | `240-243` | Phase 5 |
| 4 | `delivery.items.search` | Search/filter | `delivery.items.read` | read_only | HTTP list filters + MCP | `381-384` | Phase 5 |
| 5 | `delivery.releases.list` | ListReleases | `delivery.items.read` | read_only | REST | `398-401` | Phase 4 |
| 6 | `delivery.sprints.list` | ListSprints | `delivery.items.read` | read_only | REST | `415-418` | Phase 4 |
| 7 | `delivery.milestones.list` | ListMilestones | `delivery.items.read` | read_only | REST | `432-435` | Phase 4 |
| 8 | `delivery.views.save` | SaveView | `delivery.items.write` | local | REST + MCP | `438-441` | Phase 6 |
| 9 | `delivery.views.list` | ListSavedViews | `delivery.items.read` | read_only | REST + MCP | `444-447` | Phase 6 |
| 10 | `delivery.members.week` | MemberWeek | `delivery.items.read` | read_only | REST + MCP | `450-453` | Phase 6 |
| 11 | `delivery.projects.progress` | ProjectProgress | `delivery.items.read` | read_only | REST + MCP | `456-459` | Phase 6 |
| 12 | `delivery.projects.schedule` | ProjectSchedule | `delivery.items.read` | read_only | REST + MCP | `462-468` | Phase 6 |
| 13 | `delivery.notifications.list` | ListNotifications | `delivery.items.read` | read_only | REST | `471-485` | Phase 6 |

共同构造器在 `operations.go:536-550`。它把 authentication 固定为 `api-key,jwt`，permission 使用两个 legacy alias，并没有从 canonical protobuf/OperationPlan 生成。迁移验收必须逐项证明 13 个 ID 均已进入生成输出；不能只删除构造器。

### 2.2 两个同 ID 的手写替代计划

| Operation ID | 触发条件 | 生成权限 | 替代权限 | 风险 |
| --- | --- | --- | --- | --- |
| `delivery.dashboard.get` | `Operations.service != nil` | `delivery.dashboard.read` | `delivery.items.read` | 同一 Operation ID 的语义依运行装配分叉 |
| `delivery.items.list` | `Operations.service != nil` | `delivery.work-items.read` | `delivery.items.read` | 生成字典/guard 与扩展 alias 可能不一致 |

证据：`operations.go:46-95`；canonical 声明：`iot_delivery.proto:17-41`。目标是所有 transport 对同一 ID 只使用同一生成 plan。

## 3. 其他手写 OperationPlan

配置修订不在 `extensionPlan` 中，但属于必须保留的真实内部能力：

| Operation ID | 能力 | 权限 | 事务 | 外部 transport | 证据 |
| --- | --- | --- | --- | --- | --- |
| `config.revisions.change` | 追加不可变配置修订 | `config.revisions.write` | local | 无 | `configapplication/operations.go:111-168,266-287` |
| `config.revisions.compare` | 比较两修订 | `config.revisions.read` | read_only | 无 | `170-197,266-287` |
| `config.revisions.rollback` | 以旧 payload 追加回滚修订 | `config.revisions.rollback` | local | 无 | `199-257,266-287` |

它们已被 permission dictionary 注册（`permission-dictionary.v1.json:63-65`），但尚非 protobuf 生成计划。本轮目标保持“内部能力”，不新增 UI/REST/gRPC/MCP。

## 4. 端到端能力矩阵

| 能力族 | 当前实现 | 当前关键证据 | 目标状态 | 阶段 |
| --- | --- | --- | --- | --- |
| 项目规划 | Project/Release/Sprint/Milestone create + list | `model.go:47-128`; `service.go:58-223,511-571` | typed DSL 全覆盖；create/list 同一权限与 scope 模型 | 4 |
| 事项层级 | Epic/Task/Subtask/Defect、父子与依赖循环校验 | `model.go:23-45,244-280`; `service.go:226-503` | canonical create/update/get/list/search/similarity | 5 |
| 上下文与协作 | 计划、方案、ADR、阻塞、评论、活动 | `model.go:217-280`; `service.go:1004-1040,1335-1406` | CAS、audit、transport 冲突语义一致 | 5 |
| 关卡与关闭 | 五关卡、证据、生产验证、复盘 | `model.go:157-172`; `service.go:1217-1333` | 状态机与职责分离保持；旧会话撤权即时生效 | 5/10 |
| IoT/研发链 | device/firmware/customer/environment/rollout + PR/build/test/defect/release | `model.go:174-215` | DTO/持久化/投影/前端完整保留 | 5/6 |
| 驾驶舱与视图 | dashboard、search、saved views、member week | `handler.go:44-64`; `service.go:572-651,1042-1090` | 生成计划 + 人员 UserID 语义，不把显示名当身份 | 6 |
| 进度与排期 | weighted progress、blocked/overdue/unscheduled/capacity | `model.go:374-420`; `service.go:652-822` | 项目 scope guard 与跨 transport 等价 | 6 |
| 通知与提醒 | local inbox、due worker、Webhook/WeCom/SMTP adapters | `bootstrap/application.go:225-310,671-693`; `notification/**` | 生命周期、健康、Outbox retry/DLQ、敏感配置边界保留 | 6/7 |
| SQLite/UoW | SQLite root transaction factory 与 joined repositories | `bootstrap/application.go:270-287`; `localtx/sqlite.go` | 目标 Yunka root UoW typed capability 接入，无第二事务根 | 3/7 |
| Outbox/投影 | 事务 Outbox → local broker → Obsidian/notification | `service.go:1578-1590`; `localoutbox/sqlite.go`; `obsidian/**` | 写入失败整体回滚；拒绝路径业务/Outbox 零变更 | 7 |
| 审计 | auth accepted/rejected、authorization denied、application rollback | `audit/executor.go:12-49`; `audit/security.go` | success/denied/failure/rollback 完整、脱敏、真实 Principal | 7/9 |
| 配置修订 | immutable change/compare/rollback | `configapplication/operations.go:111-287` | 保持内部，无新增 UI | 7 |
| 身份核心 | organization/user/external identity/service account/roles/teams/bindings | `identitycore/migration.go:20-66` 及后续 schema | 新增独立 local credential；不伪造 OIDC、不按邮箱合并 | 8 |
| 本地认证 | development API keys；production OIDC BFF | `localauth/auth.go`; `bffhttp/middleware.go` | password verify → opaque session → verified short JWT → real Principal | 8/9 |
| 人类授权 | SQLite RoleBinding/GrantResolver + OperationGuard，仅 JWT 分支 | `humanauthz/resolver.go:44-98`; `deliveryauthz/guard.go:128-227` | 本地成员复用同一 durable human chain | 9/10 |
| 成员管理 | schema/预留 permissions 已有，业务内账号 API/UI 不存在 | `permission-dictionary.v1.json:41-48`; `web/app/auth/**` | 初始化、创建、停用、重置、项目角色；并发 CAS | 8/10/11 |
| Web session/CSRF | OIDC session Map + guard；client 未发 token | `session.ts`; `session-guard.ts`; `src/api.js:1-9` | 服务端 session、当前成员、退出、改密、CSRF 全链 | 9/11 |
| runtime closure | runtimehost、health、diagnostics、reverse shutdown | `bootstrap/application.go:362-404,580-693` | 目标版本 lifecycle/health/diagnostics 复核 | 3/12 |

## 5. 不可丢失的验收不变量

1. 每个行为变更必须真实 RED → 最小 GREEN → 相关完整回归；缺工具不是 RED。
2. REST、gRPC、MCP 必须进入同一真实 SQLite Principal/GrantResolver/OperationGuard/UoW 链；`localauth` 不能证明生产授权。
3. 认证/授权拒绝、CAS 冲突和应用失败不得产生业务或 Outbox 变更；仅允许策略规定的安全审计。
4. expectedRevision 的冲突分类必须在 REST/gRPC/MCP 保持一致。
5. Owner/显示名不充当身份；职责分离比较 canonical Principal。
6. 停用、改密/重置、撤权应使旧会话及时失效。
7. 生成输出只能由 canonical protobuf typed DSL 生成，禁止手改。
8. 配置修订仍为内部能力，不借本轮扩张外部界面。
9. 发现框架候选问题时按 YU-00F 格式先最小复现、反证和归因；consumer 接入债不算框架缺陷。
