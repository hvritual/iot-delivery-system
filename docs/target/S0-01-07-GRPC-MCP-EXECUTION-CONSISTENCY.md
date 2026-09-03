# S0-01-07: gRPC 与 MCP 统一执行边界

## 目标态

- 业务 gRPC 只由 Yunka 生成的 `internal/delivery/transport/rpc.RegisterOperationExecutor` 注册，并通过生成 assembly 调用。
- MCP 是本地 stdio 适配器；它只接收 `*delivery/application.Operations`，所有工具调用均进入 Operations 的生成 OperationPlan、授权和本地事务链。
- 因此，MCP 不再能够注入裸 `*delivery.Service`，也不保留动态 Lifecycle 降级接口；`internal/rpcapi` 的手写 gRPC Service 已移除。

## 协议表达与一致性

生成 gRPC 使用 API-key metadata、local auth interceptor 和 gRPC status：缺失或错误认证为 `Unauthenticated`，角色权限不足为 `PermissionDenied`。MCP 使用启动时已解析的本地 Principal；未认证或无权限请求以 MCP tool error 返回。两种协议的错误载体不同，但 Operations/Executor 的授权、事务、领域行为与 Outbox 提交边界相同。

`internal/mcpserver` 的 bufconn + MCP in-memory 测试覆盖 `CreateItem` 与 `AdvanceGate`：双方分别使用真实 SQLite、transactional Outbox、local authorization、Operation Observer 和生成 gRPC transport。测试归一化 ID/时间字段后校验关键领域字段、关卡、Evidence、Activity、每次 Outbox 增量以及 `delivery.items.create`、`delivery.items.advance-gate` 的相同 operation ID。viewer 的 `AdvanceGate` 在两种传输均被拒绝且实体/Outbox 不变。

MCP 的 AdvanceGate Evidence 输入保留可选 `recordedAt`：缺失时传入零时间，由既有 Service 按与 gRPC 相同的规则补齐；若提供则保留该时间。`kind` 与 `title` 仍是必填。

## 已保留与非目标

MCP 保留项目、事项、更新、评论、规划对象与保存视图能力。`SaveView` 仍是 Operations 的显式 executor extension plan，并非生成 gRPC OperationPlan；本阶段不生成 SaveView RPC。

本阶段不宣称 MCP/gRPC 工具集合或错误文案完全对称。`create_work_item` 的相似事项确认保持 MCP 专有交互；MCP 的 TraceLink 输入仍沿用既有领域 DTO，本阶段未扩展其输入契约。更完整的协议错误语义矩阵和全系统对账属于后续阶段。
