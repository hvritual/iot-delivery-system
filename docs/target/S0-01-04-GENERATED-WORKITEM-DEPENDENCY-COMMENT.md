# S0-01-04：生成 WorkItem、依赖与评论写合同

## 范围与 RED

`delivery.items.create` 曾在扩展字段存在时绕过生成计划；`update` 与
`comment.create` 也没有生成合同。RED 由缺少请求 DTO/端口的编译失败及
`TestOperationsCreatePreservesNestedWriteContract` 的嵌套字段丢失复现证明。

## 字段与 presence 对账

WorkItem 合同覆盖层级、排期、依赖、IoT（含 attributes）、追溯、评论和活动。
`update_mask` 未列字段保持；列出的字符串可清空、数值可为 0、三个 repeated
字段可用空数组显式清空。

## 可执行证据

- `TestOperationsCreatePreservesNestedWriteContract`：完整合法 CreateInput 与 SQLite 回读。
- `TestOperationsUpdateAllFieldPresenceThroughGeneratedContract`：非空关联保持与显式清空。
- `TestOperationsUpdatePresenceAndCommentPersistThroughGeneratedWrites`：评论 actor/时间、持久化和 Outbox 精确 +1。
- `TestOperationsRejectsInvalidDependenciesWithoutOutboxSideEffects`、`TestOperationsViewerWriteOperationsHaveNoSideEffects`：拒绝与零副作用。
- `TestOperationsRejectsOutOfRangeProgressBeforeWriting`：进度超过 100 在 int 到 int32 转换前拒绝，SQLite 与 Outbox 不写入。
- 命令：`go test -count=1 -mod=readonly ./...`、`yunka check --full`。

已删除 Create 的手写 extension-plan 旁路；Create、Update、Comment 均只走生成 OperationPlan、Executor 与 Adapter。

## 延后范围

UpdateContext/Decision、AdvanceGate、Close、证据/复盘、SavedView 与跨传输全面
一致性留给后续切片；本文件不宣称 S0-01-05～08 已完成。
