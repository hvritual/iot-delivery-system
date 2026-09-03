# S0-01-05：生成治理写合同

## 目标态

`UpdateContext`、`AdvanceGate` 与 `Close` 是三个已经声明在
`contracts/proto/iot_delivery.proto` 中的生成 operation plan。无论
`NewOperations` 是否同时接收 `*delivery.Service`，它们均只通过：

`OperationPlan → operation.ExecuteTyped → Adapter → Service`

保留 `Operations.service` 与 `executeServiceExtension`，因为读取及尚未迁移的
R2 扩展仍在使用它们；本切片不扩大该机制的改造范围。

## 行为边界

- `UpdateContext` 保持 Plan、Solution、Blocker 的 omitted 与显式空值语义，并完整
  往返 Decision（调用方 ID/时间或领域自动补齐）。
- `AdvanceGate` 使用 `delivery.items.advance-gate`，要求完整 Evidence、只允许相邻
  关卡，并补齐缺失的 `recorded_at`。
- `Close` 使用 `delivery.items.close`，仅允许生产验证后的事项以非空复盘关闭。
- 三种成功写入均在生成计划的 `local` 事务内更新 SQLite 与 transactional Outbox；
  权限拒绝与领域校验失败均不得写入任一侧。

## 可执行证据

`TestOperationsGovernanceWritesUseGeneratedContractsWhenServiceIsProvided` 明确将
`service` 传入 `NewOperations`，以生成端口 spy 证明三项调用未走 service extension。
测试还覆盖完整字段往返、活动 actor/时间、SQLite 回读、每次成功恰好一个 Outbox
事件、既有错误类别、失败零副作用，以及 viewer 对三项写操作的生成权限拒绝。

不修改 proto：当前 schema 已包含请求、Plan、字段和权限声明；生成检查必须保持
无差异。
