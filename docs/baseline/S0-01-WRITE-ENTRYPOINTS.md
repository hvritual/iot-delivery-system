# S0-01-01 — backend-yunka 写入口盘点

**as-of commit:** `b26e6fbce9b01caf773184a74ad9ace2b25f5e0f`
**范围：** 仅 `backend-yunka` 的现状行为。`backend/` 仅允许作只读迁移/回归对照，未作为目标写面盘点。

JSON 内全部 `file:line` 源码证据均相对于 `backend-yunka/` 目录。

## 结论

共识别 **29 个主写入口**：REST 10、gRPC 4、stdio MCP 10、内部运行时 5。另有 **12 个公开 `Service` 写方法**，它们是已证实的进程内直调旁路面，单列而不重复计入入口总数。完整逐项链路、测试与源码证据在同名 JSON 中。

| 通道 | 数量 | 对账锚点 |
| --- | ---: | --- |
| REST | 10 | `httpapi.Register` 的路由与 handler 分支 |
| gRPC | 4 | Proto/operation plan/生成注册器 |
| MCP | 10 | `mcp.AddTool(... writeTool ...)` |
| 内部运行时 | 5 | 启动 seed、定时提醒、Outbox、Obsidian、通知消费者 |

## 已证实旁路与未知

- **已证实：** `/api/items/{id}/gates/{gate}` 与 `/api/items/{id}/close` 的动作分支未检查 HTTP 方法；注册表同时允许 GET、POST、PATCH，因此 GET/PATCH 也可到达写动作分支。见 [handler.go](../../backend-yunka/internal/httpapi/handler.go:71) 与 [handler.go](../../backend-yunka/internal/httpapi/handler.go:262)。
- **已证实：** 公开 `Service` 写方法与启动 `seedExample` 并不天然经过 Operation Executor；直接注入/调用可绕过 API-key 授权与统一事务策略。
- **未知：** `internal/rpcapi.Server` 实现了相同 gRPC 写方法，但在当前 bootstrap 追踪中未发现注册；这仅是“可达性未知”，不是“不存在”。
- **未知：** 外部通知目标来自运行时配置；源码只能证实其可能由 Outbox 消费链触发，不能断言当前配置了任何外部地址。

## 双向对账

- 四个生成变更 operation plan（create、update-context、advance-gate、close）均映射到生成 gRPC 入口，并由 REST/MCP 复用。
- 项目、规划、评论、视图与扩展工作项更新使用 `extensionPlan`，不是生成 operation-plans.json 的条目。
- SQLite repository、outbox、通知 inbox 和 Obsidian exporter 的实际写原语均已列入 JSON 下游对账；它们不重复计为外部入口。

## 验证约定

完成前须验证 JSON 解析、每一条 `file:line` 证据可定位、计数可由 `write_entrypoints` 复算，并运行 `git diff --check`。本资产只描述当前事实；没有启动服务、访问数据库或 Vault，也没有修改运行源码或生成合同。
