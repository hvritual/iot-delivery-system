# S0-01-08：当前态无旁路核验

- **状态：** Verified locally
- **基线：** `c3792c5b2c78ee82bd8832b1daa25d99ac653193`
- **目标：** `backend-yunka/`；`backend/` 仅作只读行为证据，未修改。
- **定义：** “零旁路”仅指当前生产可达的业务主数据写入：注册的 REST、生成 gRPC、stdio MCP 以及 bootstrap seed 必须经 `Operations`/Yunka `Executor`、授权、SQLite Unit of Work 中的 repository 写和 Outbox staging，随后才可由 dispatcher 驱动投影。`Service` 仍是领域内部实现面，但 `Application.Service` 已删除，生产调用点由 AST allowlist 门禁约束。

## 双向对账

| 入口 | 上游到执行边界 | 写入/下游链 | 自动化证据 | 结论 |
| --- | --- | --- | --- | --- |
| REST | `httpapi.Register` → `Operations` | Executor → authz → SQLite transaction + Outbox → dispatcher | `internal/httpapi/rest_execution_boundary_test.go` | 无生产可达直连 repository/Service 写入口 |
| 生成 gRPC | generated RPC executor → `ExecuteTyped` | 同一 Executor/transaction/Outbox 链 | `delivery_contract_test.go`、bootstrap runtime test | 受生成 OperationPlan 约束 |
| stdio MCP | 已认证 local principal → `Operations` | 同一 Executor/transaction/Outbox 链 | `internal/mcpserver/server_test.go` | 无独立写策略 |
| bootstrap seed | 默认关闭；仅显式 `development + example` 时，bootstrap 信任边界内直接构造、`Authenticated=true` 的内部 principal（非人类/API key 登录）→ `Operations.List/Create/UpdateContext` | 两次业务写在事务中 stage；已提交事件由 dispatcher 投影 | [seed structure gate](../../backend-yunka/internal/bootstrap/application_test.go)；[S0-02-08 gate](S0-02-08-BOOTSTRAP-FAIL-CLOSED-LEAKAGE-GATE.md) | 已关闭此前 `Service.Create/UpdateContext/Sync` 旁路 |
| repository/Outbox 下游 | repository 与 stager 同处 Unit of Work | dispatcher 只消费已提交 Outbox；投影/通知为下游读模型 | `internal/delivery/transactional_outbox_test.go`、bootstrap seed 回归 | 不是业务主数据反向写入口 |

## 机器复核门禁与结果

1. `TestProductionWriteCallersUseOperationsBoundary` 用 Go AST 扫描全部生产 Go 源，排除测试、pb/zz 生成物，且仅精确 allowlist `service.go`、repository 实现和 application adapter/Operations；due_reminder、非生成 transport 与未来领域生产文件仍被扫描。它拒绝 transport/bootstrap/job 直接 mutation、直接 `Sync`（仅 projection consumer allowlist）、非 bootstrap 的 `delivery.NewService` 与所有导出的 `*delivery.Service` 签名。
2. `TestProductionWriteBoundaryScannerDetectsRenamedBypasses` 以解析片段验证：改名 `RawService()`、`svc := delivery.NewService` 后的 `Create/Close`、以及 `repo.Save` 都必须被识别。`Close` 只在 service/repository 形态上判为领域 Close，避免把 broker、连接和应用生命周期关闭误报；这不是全程序类型推断。
3. `TestBootstrapSeedIsAnAuthorizedTransactionalOutboxOperation` 保留 seed 组装门禁；`TestBootstrapSeedStagesCommittedOutboxEventsThenProjectsExactlyOnce` 使用临时 SQLite/Vault，断言总 Outbox=2、published=2、同一 subject 的 create/context-update 两种事件，重启后仍恰为 2 且投影只有一处。
4. bootstrap principal 是 bootstrap 信任边界内直接构造的内部 actor，不是人类 API-key 登录；S0-02 仍需系统身份建模。现有 REST/gRPC/MCP/transactional-Outbox 测试是互证；本报告未改写 `docs/baseline/` 的历史 as-of 盘点。

## 边界说明和遗留事项

- `SaveView` 是经 `Operations/Executor` 执行的显式本地扩展合同，不是业务主数据旁路，也不是生成 gRPC 合同。
- 截止日提醒、Outbox 状态领取、投影和通知是提交后的运行/读模型副作用；它们不取得业务主数据写权限。
- 未在本任务处理：未生成的 R2 扩展合同、旧数据/Vault 迁移与切换、外部通知投递、多用户身份和生产运行治理。

## 持久化运行记录

| 命令/门禁 | 结果 | 边界 |
| --- | --- | --- |
| 初次 bootstrap 测试 | setup 阻断，不计 RED | 工作树子模块未展开，缺少 `third_party/yunka/framework/go.mod`。 |
| 展开锁定 gitlink `9a51562…` 后的新增 AST 测试 | 有效 RED | 报出 `Application.Service` 裸出口；修复后 GREEN。 |
| `go test -count=1 -mod=readonly ./...`（backend-yunka） | PASS | 全量后端目标测试。 |
| 锁定 `.tools` + `yunka check --full` | PASS | 使用工作树 contracts proto；受管生成目录零差异。 |
| `npm test` / `npm run typecheck` / `npm run build` | PASS | 在同 SHA、干净的主工作副本执行，复用其既有锁定依赖；未安装依赖。 |
| `go test -count=1 -mod=readonly ./...`（backend） | PASS | 只读行为证据，未修改。 |
| `git diff --check` 与 gitlink | PASS | 无空白错误；gitlink 保持 `9a51562aa7bcef42f6861bd91abd30aae13ed6ef`。 |
