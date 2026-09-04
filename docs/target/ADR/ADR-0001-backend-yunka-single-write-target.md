# ADR-0001：backend-yunka 是唯一目标实现与未来写入后端

- **状态：** Accepted
- **日期：** 2026-09-03
- **决策范围：** S0-01-02 架构目标、迁移边界与文档；S0-01-08 已在临时 SQLite/Vault 验证启动 seed 写链。没有真实数据或 Vault 变更。
- **权威源码锚点：** `c3792c5b2c78ee82bd8832b1daa25d99ac653193`
- **关联资产：** [S0-01-02 边界与责任](../S0-01-BACKEND-BOUNDARY.md)、[S0-01-01 写入口盘点](../../baseline/S0-01-WRITE-ENTRYPOINTS.md)、[S0-00 变更意图](../../baseline/S0-00-CHANGE-INTENT.md)

## 当前事实（CUR）

- `backend-yunka/` 已被阶段 0 的来源清单指定为目标实现；`backend/` 已被指定为只读迁移与回归行为证据，且不具有目标写面权限。[来源清单](../../baseline/SOURCE-MANIFEST.json)
- S0-01-01 在其所载的 `b26e6fb` 快照中盘点了 29 个 `backend-yunka` 主写入口、12 个公开 `Service` 直调旁路面，并将旧 `backend/` 排除出目标写面盘点。[S0-01-01](../../baseline/S0-01-WRITE-ENTRYPOINTS.md)
- 当前 bootstrap 已装配 Yunka executor、SQLite 本地事务工厂与 HTTP/gRPC transport；HTTP 兼容适配器由该 bootstrap 注册。[application.go](../../../backend-yunka/internal/bootstrap/application.go)
- 公开 `Service` 方法仍是进程内领域实现端口；它们不构成正式 transport 写合同。S0-01-08 已将生产可达的启动 `seedExample` 改为以 bootstrap 信任边界内直接构造、`Authenticated=true` 的内部 principal（非人类/API key 登录）调用 `Operations`，并删除启动时直接 `Sync` 投影。[service.go](../../../backend-yunka/internal/delivery/service.go) [application.go](../../../backend-yunka/internal/bootstrap/application.go)

## 已接受目标决策（TGT-ADR-0001）

1. `backend-yunka/` 是唯一目标实现，并且在正式迁移完成后是唯一可写的主数据后端。
2. `backend/` 冻结为只读迁移、回归与行为证据：不得新增功能、不得承担运行时写入者、不得成为新合同或新 transport 的落点。阶段 0 不删除它，也不迁移其数据。
3. 目标态中，所有业务写入必须沿正式合同进入统一 `Operations` / Yunka `OperationPlan` + `Executor`，并在 `GrantResolver` / `OperationGuard` 通过后进入同一个 Unit of Work。业务 repository 写与 Outbox staging 必须在该 Unit of Work 内并列参与原子提交；只有提交成功后，dispatcher 才能领取已提交的 Outbox 事件来驱动投影与通知。
4. Web/BFF、REST、gRPC、stdio MCP 及内部运行任务都是 transport/触发端；它们不得直连 repository、复制授权逻辑，或建立绕过 executor 的业务写路径。
5. S0-01-08 已关闭启动 seed 的生产可达旁路；公开 `Service` 方法仍作为仅供 `Operations`/application adapter 使用的领域实现端口，不能被 transport 或内部任务直接作为业务写入口。其他合同补全、迁移/切换、数据回滚演练仍属于后续原子任务，尤其是 S0-07～S0-09。

## 决策理由

现有目标后端已证明存在 Yunka 的本地执行器、授权解析、SQLite 事务以及 Outbox 组装点；例如生成 gRPC adapter 通过 `ExecuteTyped` 调用 executor，而 SQLite Outbox 支持接收当前事务句柄。[rpc executor](../../../backend-yunka/internal/delivery/transport/rpc/zz_yunka_management_operation_executor_gen.go) [auth.go](../../../backend-yunka/internal/localauth/auth.go) [sqlite transaction](../../../backend-yunka/internal/localtx/sqlite.go) [outbox](../../../backend-yunka/internal/localoutbox/sqlite.go)

这提供了一个可被收口的目标写路径，同时保留旧后端只读对照，避免把迁移证据误当作运行写入授权。

## 允许与禁止依赖

| 关系 | 规则 | 状态 |
| --- | --- | --- |
| 正式合同 → Operations / Executor | 允许且为所有业务写的必经入口 | TGT 约束 |
| Operations / Executor → GrantResolver / OperationGuard → Unit of Work | 允许；授权通过后建立唯一目标写入单元 | TGT 约束 |
| Unit of Work → repository 写；Unit of Work → Outbox staging | 允许；两者并列参与同一原子提交，repository 不由 Outbox 调用 | TGT 约束 |
| 已提交 Outbox → dispatcher → 投影 / 通知 | 允许；dispatcher 只能领取已提交事件，不能观察未提交 staging | TGT 约束 |
| REST、gRPC、MCP、web/BFF、内部任务 → repository | 禁止 | TGT 约束 |
| transport 自行复制鉴权、授权或事务策略 | 禁止 | TGT 约束 |
| `backend/` → 运行时写入、功能新增或新合同落点 | 禁止 | TGT 约束 |
| `backend/` → 只读迁移、回归、行为比对 | 允许 | 已接受边界 |

## 迁移、失败关闭与回滚边界

- **阶段 0：** 仅冻结边界与证据；不删旧后端、不启动服务、不读写数据库或 Vault。
- **正式迁移与切换：** 仍由 S0-07～S0-09 处理，必须另有数据范围、对账、回滚窗口、授权与验收证据；本决策不是切换许可。
- **失败关闭：** 任一正式写链未能证实“合同→执行器→授权→同一 Unit of Work（repository 写与 Outbox staging 并列原子提交）→提交后 dispatcher”，或发现旧后端仍是写入者时，禁止切换并回到只读对照状态；不得以双写、临时直连或旁路继续推进。
- **回滚：** 在未获单独授权的情况下，回滚仅限于目标运行配置/路由指向与已迁移副本，不允许向旧 `backend/` 或 Vault 回写。Git 文档恢复不能替代数据恢复。

## 后果

- 新功能与修复只能面向 `backend-yunka/` 的目标合同和受控执行链，不能通过编辑旧后端取得“快速交付”。
- 旧后端长期保留会带来维护成本，但换取迁移与回归的只读证据；其移除必须有独立的迁移完成与保留期决策。
- 公开 `Service` 方法与未生成的扩展 plan 必须继续被显式治理，不能因既有测试通过而扩大为 transport 写许可；S0-01-08 的零旁路定义与当前验证清单见 [S0-01-08 报告](../S0-01-08-NO-BYPASS-VERIFICATION.md)。

## 验证方式

本 ADR 的完成验证是静态且文档范围内的：

1. [S0-01-01](../../baseline/S0-01-WRITE-ENTRYPOINTS.json) 中 `CUR-*` 入口、旁路与源码定位可复核。
2. 边界图与责任表覆盖所有指定组件，并区分 CUR、TGT 与 TODO。
3. 本文件及关联资产的 Markdown 内链、源码引用、Mermaid 结构和 `git diff --check` 通过。
4. S0-01-08 已以临时 SQLite/Vault 验证 seed 的运行时收口；数据迁移、切换、回滚演练和真实环境端到端测试仍不在本 ADR 的验证范围，其实际通过证据不得由本 ADR 替代。
