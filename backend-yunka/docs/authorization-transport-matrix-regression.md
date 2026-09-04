# S0-03-08 权限传输矩阵回归事实报告

日期：2026-09-03
基线：`857e40f7c71c86e1a40c040fc33e424d505ec7fe`
Yunka gitlink：`9a51562aa7bcef42f6861bd91abd30aae13ed6ef`

YU-07 增量更新：2026-09-04；固定框架 `057ebcf88a87303eb633eb6e604d306f633dfac0`；任务父提交 `9bf80993accbd11544652af8b1eb1c1961bc73fd`。

## 结论

权限字典、生成 OperationPlan 与传输入口的当前登记口径为 **13/13 REST↔gRPC**；其中有 MCP 公共工具映射的是 **11/11 MCP**。允许结果通过同一 Operations/Executor/SQLite-Outbox 链执行；拒绝统一为 `unauthenticated` 或 `permission_denied`，拒绝路径不得提交业务对象或 Outbox。

本次 RED 是 MCP 将授权拒绝作为 `gateway authz: access denied: operation=... reason=...` 原始文本返回，无法作为稳定类别消费；S0-03-07 的领域职责分离 sentinel 也未被 REST/gRPC 识别为授权拒绝。GREEN 后 MCP 只返回稳定类别，应用适配器将生产验证/关闭中的职责分离拒绝包装为 `permission_denied`（但保留 `errors.Is` 对领域 sentinel 的可判定性），HTTP 不再回显授权内部信息。

## 权威操作与原生映射

| Operation | permission / scope | REST | gRPC | MCP |
| --- | --- | --- | --- | --- |
| `delivery.dashboard.get` | `delivery.dashboard.read` / organization | `GET /api/dashboard` | `GetDashboard` | N/A |
| `delivery.items.list` | `delivery.work-items.read` / project | `GET /api/items` | `ListItems` | `delivery.list_work_items` |
| `delivery.items.create` | `delivery.work-items.create` / project | `POST /api/items` | `CreateItem` | `delivery.create_work_item` |
| `delivery.items.update` | `delivery.work-items.update` / object | `PATCH /api/items/{item_id}` | `UpdateItem` | `delivery.update_work_item` |
| `delivery.items.comment.create` | `delivery.work-items.comment.create` / object | `POST /api/items/{item_id}/comments` | `CreateItemComment` | `delivery.add_comment` |
| `delivery.items.update-context` | `delivery.work-items.context.update` / object | `PATCH /api/items/{item_id}` | `UpdateItemContext` | N/A |
| `delivery.items.advance-gate` | `delivery.work-items.gate.advance` / object | `POST /api/items/{item_id}/gates/{gate}` | `AdvanceGate` | `delivery.advance_gate` |
| `delivery.items.close` | `delivery.work-items.close` / object | `POST /api/items/{item_id}/close` | `CloseItem` | `delivery.close_work_item` |
| `delivery.projects.create` | `delivery.projects.create` / organization | `POST /api/projects` | `CreateProject` | `delivery.create_project` |
| `delivery.projects.list` | `delivery.projects.read` / project | `GET /api/projects` | `ListProjects` | `delivery.list_projects` |
| `delivery.releases.create` | `delivery.releases.create` / project | `POST /api/releases` | `CreateRelease` | `delivery.create_release` |
| `delivery.sprints.create` | `delivery.sprints.create` / project | `POST /api/sprints` | `CreateSprint` | `delivery.create_sprint` |
| `delivery.milestones.create` | `delivery.milestones.create` / project | `POST /api/milestones` | `CreateMilestone` | `delivery.create_milestone` |

`TestAuthorizationTransportMatrixHasThirteenRPCAndElevenMCPPublicOperations` 从权限字典和生成 `operation-plans.json` 计算并断言 13 个 REST↔gRPC operation 与 11 个不重复 MCP 公共映射；它拒绝缺 REST/gRPC、重复 MCP 或计数漂移。`TestMCPRegistrationContainsExactlyElevenDictionaryPublicToolsAndSixExcludedExtensions` 通过 MCP `tools/list` 实际登记验证同一 11 个工具；相似项、周任务、进度、排期、保存/列出视图这 6 个本地扩展明确不计入公共矩阵。

## 认证可用性与 post-auth 授权语义

| 身份 | REST | gRPC | MCP | 进入 Operations/Executor 后 |
| --- | --- | --- | --- | --- |
| 人员 | REST BFF/JWT | N/A | 可由本地/委托 Principal 表示 | 真实 human GrantResolver + OperationGuard；同一 grant、scope、对象归属语义 |
| 服务身份 | N/A | gRPC service identity | N/A | 真实 service GrantResolver + OperationGuard；每 operation、permission、project 的显式 grant |
| 本地/委托调用者 | N/A | N/A | MCP local/delegated Principal | 仅在已认证 Principal 进入统一边界后评估授权 |

N/A 是架构限制，不以测试伪造生产认证通道。测试夹具可注入 Principal 来验证 post-auth authorization；这**不是 production network-authentication proof**，不连接真实 OIDC、数据库外部服务或 Vault。

生产网络认证支持仅为 REST 的 BFF/JWT、gRPC 的 service token、以及 MCP 本地/委托 Principal；本回归没有模拟或声称外部网络认证已经完成。`TestPostAuthTransportMatrixAllowsGateWithEquivalentSQLiteAndOutboxEffects` 在认证后注入真实 project-scoped `project-administrator` Principal，分别走 REST、gRPC、MCP 的 handler/server/tool，验证 project A 的 gate allow 为 2xx/OK/`IsError=false`、SQLite gate 和 Outbox 变化等价，并验证 project B 与不存在事项稳定拒绝。`TestProductionServiceGrantResolverAllowsOnlyItsGrantedProjectOverGRPC` 落真实 `service_accounts`，只经 `serviceauthz.Manager.Grant` 建立项目级 grant，并从 gRPC 注入 `AuthMethodServiceToken`，验证 granted project 的 OK、错误 project scope 的 PermissionDenied、零 Outbox 和零 work-item 创建。Operations 层 `TestProductionAuthorizationMatrixAllowsRegisteredOperationClasses` 只证明其余操作类的统一 Executor 覆盖，不作为生产网络认证或全部三传输 allow 证据。

## 规范化结果与副作用

| 场景 | REST | gRPC | MCP | 副作用 |
| --- | --- | --- | --- | --- |
| allow | 2xx | `OK` | `IsError=false` | 响应关键状态与 SQLite/Outbox 等价 |
| unauthenticated | 401 / `unauthenticated` | `Unauthenticated` | `IsError=true`, `unauthenticated` | 零业务/Outbox 副作用 |
| 无 grant、viewer 写、错误/缺失 scope、跨项目、撤销后 | 403 / `permission_denied` | `PermissionDenied` | `IsError=true`, `permission_denied` | 零业务/Outbox 副作用，且不按对象存在性改变分类 |
| 实施人生产验证或关闭自己事项 | 403 / `permission_denied` | `PermissionDenied` | `IsError=true`, `permission_denied` | 零业务/Outbox 副作用；领域 sentinel 仍可 `errors.Is` |

## 已执行命令与证据

- S0-03-08 历史 RED：`go test ./internal/bootstrap -run TestProductionAuthorizationMatrixAllowsRegisteredOperationClasses -count=1` 首次将夹具连接到 legacy service extension 时，`delivery.items.list` 被未登记扩展路径拒绝；GREEN 将矩阵夹具改为只装配生成 OperationPlan 的 Operations，测试随即通过。
- `go test ./internal/bootstrap -run 'TestProductionAuthorizationMatrixRejectsRESTAndMCPWithoutSideEffects|TestPostAuthTransportMatrixAllowsGateWithEquivalentSQLiteAndOutboxEffects|TestProductionServiceGrantResolverAllowsOnlyItsGrantedProjectOverGRPC|TestProductionAuthorizationMatrixAllowsRegisteredOperationClasses' -count=1`：每个拒绝场景使用独立 SQLite Team/Membership/Role/Permission/RoleBinding 夹具，经 production human/service GrantResolver、OperationGuard 和 Executor；动态覆盖未认证、无绑定、viewer 写、跨项目 scope、缺失对象不泄漏、撤销即时生效、实施人生产验证（7×REST/gRPC/MCP）。gate allow 使用三套隔离同构 fixture，验证 REST 2xx、gRPC OK、MCP `IsError=false` 与 SQLite/Outbox 等价副作用；service token 在 gRPC 对一个 manager-created project grant allow、对另一项目独立 scope deny。Operations 层另覆盖只读、项目创建、项目内规划写、对象更新、项目发布写、独立生产验证与关闭；这些不冒充三传输网络认证证据。
- `go test ./internal/delivery/application ./internal/httpapi -run 'TestAdapterClassifiesSelfProductionVerificationAsPermissionDenied|TestWriteErrorUsesStableAuthorizationCategory' -count=1`
- S0-03-08 历史门禁使用当时的 `TestAuthorizationTransportMatrixHasTwelveRPCAndTenMCPPublicOperations`；YU-07 已以当前 13/11 测试替代它。
- YU-07：`go test ./internal/bootstrap -run 'TestProductionProjectList(MatrixUsesOneDurableScopeAcrossRESTGRPCAndMCP|DenialMatrixPreservesProjectsAndOutbox)' -count=1`，验证两个租户的项目集合在 REST/gRPC/MCP 完全一致，错配租户身份及无绑定成员三端均拒绝，项目与 Outbox 不变。
- YU-07：`go test . -run TestAuthorizationTransportMatrixHasThirteenRPCAndElevenMCPPublicOperations -count=1`
- YU-07 最终门禁：`go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、前端 16 文件/45 测试、前端类型检查、双次 canonical generate 零漂移、`yunka check --full` 与 `git diff --check`。

## 已知边界和未做事项

- YU-07 未新增 REST、gRPC 或 MCP 的生产认证通道；它把项目列表迁入 canonical typed plan，并在统一适配边界应用持久项目授权集。
- YU-07 通过固定生成器更新受管派生物，未手改生成文件，也未修改 `backend` 或 `third_party/yunka`；未部署或连接真实 OIDC、外部数据库/Vault。
- 生产网络认证、本地成员凭据/会话验签及外部身份提供方联调不在本回归的证据范围内。
