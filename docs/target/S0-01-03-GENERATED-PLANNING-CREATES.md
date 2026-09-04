# S0-01-03：Project/Release/Sprint/Milestone 创建操作生成合同

- **状态：** Implemented and verified locally
- **权威起点：** `codex/iot-delivery-system-mvp` 的 `d45d85d67683de640c481980c098877596dd4e53`
- **范围：** 仅四个既有创建写行为的合同化；不新增更新、删除或生命周期行为。
- **边界：** `backend/`、数据/Vault、运行配置和 Yunka 子模块均未修改；不启动服务。

## 已证实结果

| 既有操作 ID | 生成请求 / 响应 | 生成执行语义 | 手写适配接缝 |
| --- | --- | --- | --- |
| `delivery.projects.create` | `CreateProjectRequest` / `ProjectResponse` | API key、`delivery.items.write`、本地事务、无幂等键 | `Adapter.CreateProject` 调用既有 `Service.CreateProject` |
| `delivery.releases.create` | `CreateReleaseRequest` / `ReleaseResponse` | API key、`delivery.items.write`、本地事务、无幂等键 | `Adapter.CreateRelease` 调用既有 `Service.CreateRelease` |
| `delivery.sprints.create` | `CreateSprintRequest` / `SprintResponse` | API key、`delivery.items.write`、本地事务、无幂等键 | `Adapter.CreateSprint` 调用既有 `Service.CreateSprint` |
| `delivery.milestones.create` | `CreateMilestoneRequest` / `MilestoneResponse` | API key、`delivery.items.write`、本地事务、无幂等键 | `Adapter.CreateMilestone` 调用既有 `Service.CreateMilestone` |

`Operations.CreateProject`、`CreateRelease`、`CreateSprint` 和 `CreateMilestone` 现在各自通过生成的 `OperationPlan…` 与 `ExecuteTyped` 进入既有 service；相应四个 `extensionPlan` 调用已移除。REST 和 MCP 继续调用这些 `Operations` 方法，保持既有调用路径兼容。

## TDD 与生成证据

1. RED：`go test -count=1 -mod=readonly .`。新增合同测试断言了四个 RPC、生成应用端口、生成 RPC executor 和 operation plan；运行时报告这四类生成声明缺失，且 operation plan 数为 `6`（期望 `10`）。
2. GREEN：Yunka canonical generator 重新生成 Protobuf、contract、application、RPC executor 和 assembly 产物；合同测试、application 和 HTTP 聚焦测试通过。
3. 行为回归保护：`TestOperationsPlanningCreatesUseGeneratedContractsAndPersistResponses` 经 `NewOperations`、`NewAdapter`、实际 Executor、SQLite 与 transactional Outbox 顺序创建 Project、Release、Sprint、Milestone；逐项验证输入映射、返回 ID、项目关联、状态、日期、描述、持久化记录及四条 pending Outbox 事件。该测试在已正确实现的候选上直接通过，不能替代最初的 RED。
4. 授权回归保护：`TestOperationsPlanningCreatesRequireGeneratedWritePermission` 以 viewer principal 表驱动调用四个 Operations 方法，全部得到 executor 的授权拒绝且无 Outbox 事件，证明未绕过生成 plan 的 `delivery.items.write` 要求。
5. 一致性与回归：`yunka check --full` 通过；`go test -count=1 -mod=readonly ./...`（`backend-yunka`）通过。未发现 `web/` 使用此次更新的 `contracts/generated/client.ts`，故无需前端 typecheck。

## 字段无损审计

- Project：第二轮 Navigator 发现 `Description` 被遗漏。先将行为测试改为传递非空描述，得到真实 RED（返回 `Description:""`）；随后以 `CreateProjectRequest.description = 4`、`Project.description = 7` 追加兼容字段，并补齐 Operations、Adapter 和双向 proto 转换，GREEN 后返回值与 SQLite 持久值均保留描述。
- Release：`ProjectID`、`Name`、`Version`、`TargetDate`、`Status`、`Description` 与 ID/时间戳均已在请求、Adapter、响应转换和行为测试中覆盖。
- Sprint：`ProjectID`、`Name`、`Goal`、`StartDate`、`EndDate`、`Status` 与 ID/时间戳均已覆盖。
- Milestone：`ProjectID`、`Name`、`TargetDate`、`Status`、`Description` 与 ID/时间戳均已覆盖。

## 未收口的边界

本切片不处理 WorkItem、依赖、评论、证据、闸门、决策或关闭操作；也不把其他手写 extension plan 声称为生成合同。公开 `Service` 直调与 seed 旁路仍是后续任务的 TODO。
