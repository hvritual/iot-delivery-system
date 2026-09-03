# IoT Delivery System · Yunka 并行 MVP 改造

## 结果状态

| 范围 | 状态 | 证据 |
| --- | --- | --- |
| 独立 Yunka 项目脚手架 | 已验证 | `backend-yunka/.yunka/project.json`、`yunka init` 生成的开发/Provider/Protobuf 清单 |
| 交付领域与 SQLite | 已验证 | 创建、关卡、证据、复盘、ADR、重启持久化测试 |
| R2 项目与研发交付管理 | 已验证 | 项目/版本/Sprint/里程碑、Epic/任务/子任务/缺陷、依赖、相似度、评论审计、搜索/保存视图、成员周视图和项目进度测试 |
| Obsidian 单向投影 | 已验证 | 临时 Vault 中总览、规划、方案、决策、发布验证、复盘笔记测试；含排期/依赖、IoT 范围、研发关联、评论和活动审计 |
| 通知与截止日提醒 | 已验证 | SQLite 本地收件箱、事件级每通道幂等、Webhook HMAC、企业微信机器人、SMTP 注入式通道测试；外部目的地仅在完整环境配置后启用，本次未向真实外部网络投递；截止日 worker 的 Outbox 幂等与运行时测试 |
| MCP 任务生命周期 | 已验证 | stdio server 的内存传输测试与命令构建，覆盖项目、事项、相似度、计划、成员周视图、项目进度和项目交付健康；尚未注册到外部 MCP 客户端 |
| HTTP 前端兼容 | 已验证 | Next App Router 的服务端 `/api/*`、`/health` 代理，R2 HTTP handler/runtime 测试、前端单元测试与生产构建；旧后端对照时会降级为五板块只读驾驶舱而非 R2 404 整页失败 |
| Yunka HTTP/gRPC 运行时 | 已验证 | `runtimehost → kernel → core.App`、`/health`、`/__yunka/diagnostics`、生成 gRPC 服务集成测试 |
| Yunka 合同生成 | 已验证 | `yunka generate/check --full`：1 个服务、15 个消息、2 份 Go/gRPC 生成文件 |
| 旧数据/Vault 正式切换 | 未执行 | 本次不读取、迁移或覆盖 `backend/data` 与正式 Vault |

## 当前实现边界

```text
Next App Router 桌面工作台
  └─ server proxy (/api/*, /health; injects local API key)
       └─ Yunka runtimehost
            ├─ HTTP compatibility adapter (/api/*)
            ├─ Yunka health + diagnostics
            ├─ generated DeliveryService gRPC adapter
            ├─ local API-key authentication and execution security
            └─ kernel/core.App lifecycle
                 └─ SQLite runtime component
                      └─ Delivery application operations
                           ├─ one-way Obsidian projection
                           └─ local notification router → local inbox / injected channel adapters

stdio MCP Server
  └─ authenticated Delivery application operations
```

SQLite 是该 MVP 的主数据源和 Yunka 运行时组件，而不是 Yunka 平台 Provider 管理的 MySQL 数据源。这是为保持现有本地交付管理模型和可运行性作出的明确适配，不应误读为框架已原生解决 SQLite 平台化。

同一 Yunka 进程将 SQLite 限制为单连接池，并启用 WAL 与 5 秒 busy timeout，以串行化命令事务、Outbox dispatcher 和本地通知收件箱的写入；通知读模型也加入当前 Yunka 读事务。该实现解决本地单进程争用，不解决两个独立进程同时打开同一 DB 文件的问题。

## 已采用与未采用

已采用：

- Yunka 的项目档案、Protobuf 生成清单和完整检查。
- 标准 `runtimehost` 对 HTTP/gRPC transport 的所有权。
- `kernel.Bootstrap` 与 `core.App` 生命周期，以及 SQLite 健康/关闭组件。
- 初始 6 个 operation plan 的生成 Protobuf/gRPC 类型、策略、RPC executor 和手工适配的 `DeliveryService` 实现。
- Yunka `operation.Executor`、声明式本地角色权限和 SQLite 本地执行事务；R2 HTTP/MCP 扩展也必须经过同一个执行器，而非直接访问仓储。
- SQLite 事务 Outbox、进程内 local broker、Obsidian 全量幂等投影，以及本地通知收件箱。通知通道以 `notification.Channel` 接口在 bootstrap 装配，默认没有外部通道；显式配置时可使用通用 Webhook、企业微信机器人和 SMTP，失败会进入已有 Outbox 的退避重试/死信路径。
- 截止日提醒 worker：开放事项按可配置提前天数扫描，同一事项每天使用稳定事件 ID 写入 Outbox；完成项跳过，重启和重复扫描不会重复投递。
- 本地 stdio MCP server：通过已鉴权的应用用例执行项目、事项、相似度确认、计划、成员周视图、进度、项目交付健康和保存视图。

当前未采用：

- R2 新增操作的 Protobuf/DSL contract 与生成 application/RPC executor；它们目前是手写 operation plan 扩展，虽然复用执行器和安全策略，但不是生成合同。`operation-plans.json` 仅覆盖初始 6 个操作。
- Gateway 的声明式鉴权、身份/组织/租户解析与授权范围解释。
- Provider 驱动的 MySQL 绑定、外送事务、Kafka/NATS 等可靠事件传输。
- 生产级的外部通知凭据治理、按成员偏好/升级规则、Webhook 接收端 SLA、SMTP TLS/邮件供应商治理。当前通道适配器和本地 Outbox 退避/死信已实现，但没有真实渠道授权或外部投递验收。
- 生产级可观测性采集、密钥治理、环境部署、备份/恢复与 SLO。
- MCP Streamable HTTP/OAuth、多用户身份映射和与常驻 HTTP 运行时共享同一 SQLite 文件的 sidecar 通讯机制。
- 旧 SQLite 的数据迁移、正式 Obsidian Vault 切换及清理旧投影。

这些不是“已由框架解决”的能力；它们是后续架构和运行治理工作项。

## 正式切换门槛

1. 获得对旧 SQLite 数据迁移、回滚窗口和验收样本的明确授权。
2. 在副本上验证 ID、时间、状态、关卡证据、ADR 和复盘的全量迁移对账。
3. 获得正式 Obsidian Vault 写入授权，并先在隔离路径比对投影差异。
4. 明确鉴权、审计、备份/恢复、运行告警与部署责任人。
5. 通过双写或只读对账窗口验证后，再将前端目标或正式服务地址切换。

在这些条件满足前，旧 `backend/` 与 `backend-yunka/` 必须并行保留。
