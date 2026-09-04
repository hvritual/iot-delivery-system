# IoT Delivery System · Yunka MVP

这是旧 `backend/` 的并行改造实现，不迁移、不覆盖旧数据库，也不会默认写入正式 Obsidian Vault。

## 已验证能力

- 五板块驾驶舱、项目、版本、Sprint、里程碑、Epic/任务/子任务/缺陷、跨事项依赖、相似事项提示和 SQLite 持久化；依赖写入会拒绝循环。
- 事项编辑、评论/活动审计、搜索筛选、个人保存视图、成员周视图、基于估算点的项目进度汇总，以及项目排期健康（逾期、未排期、依赖阻塞、负责人剩余估算）。
- IoT 交付范围绑定（设备、固件、客户、环境、灰度批次）和研发交付关联（PR、构建、测试、缺陷、发布）。外部记录仍是其原系统的主数据。
- Obsidian 单向投影：总览、每日五板块驾驶舱、板块钻取页、规划、方案、决策、发布验证与复盘；R2 的层级/排期/依赖、IoT 范围、研发关联、评论和活动审计也会投影。由本地 Outbox 事件驱动，重复事件重投影不会重复追加内容。
- Yunka 生成合同：`contracts/proto/iot_delivery.proto` 当前声明 12 个 operation plan、API Key 鉴权策略与读写事务策略；其中 WorkItem 创建、更新、评论、上下文/决策更新、关卡推进和关闭复盘都只经生成合同执行。`internal/assembly/` 的生成装配负责应用端口、模块目录和 gRPC transport 注册；其余 R2 扩展仍按切片逐步合同化。
- Yunka `runtimehost → generated assembly → kernel → core.App` 管理 HTTP/gRPC、`/health`、`/__yunka/diagnostics`、SQLite、Outbox dispatcher 与本地 broker 生命周期。
- HTTP `/api/*` 保持现有 React 界面兼容；stdio MCP Server 覆盖项目、事项生命周期、相似度确认、计划、成员周视图、项目进度、项目交付健康和保存视图。

## 运行

```powershell
cd backend-yunka
$env:IOT_DELIVERY_RUNTIME_ENVIRONMENT = 'development'
$env:IOT_DELIVERY_BOOTSTRAP_MODE = 'disabled'
$env:IOT_DELIVERY_LOCAL_API_KEY = '<仅本地使用的管理员密钥>'
go run ./cmd/yunka-bootstrap
```

运行环境必须显式为 `development` 或 `production`；未知或缺失值会在创建 SQLite、写 Vault 或监听端口前失败。样例初始化默认关闭，只有隔离开发场景显式设置 `IOT_DELIVERY_RUNTIME_ENVIRONMENT=development` 与 `IOT_DELIVERY_BOOTSTRAP_MODE=example` 才可运行。production 一律拒绝样例初始化、全部 legacy local API-key 环境变量以及 `IOT_DELIVERY_ALLOW_INSECURE_SERVICE_CREDENTIALS_FOR_DEVELOPMENT=true`，并要求成对、有效的 `IOT_DELIVERY_BFF_ORGANIZATION_ID` 与 `IOT_DELIVERY_BFF_ASSERTION_KEY`。production HTTP 只接受签名 BFF assertion；无 assertion 的 local API-key 路径和 gRPC local fallback 均关闭。灾备恢复不得借样例初始化执行，必须使用后续受审计的专用恢复程序。

默认值：

| 项目 | 默认值 |
| --- | --- |
| HTTP | `127.0.0.1:8281` |
| gRPC | `127.0.0.1:8282` |
| SQLite | `data/iot-delivery-yunka.db` |
| Obsidian Vault | `runtime-vault/` |

可分别使用 `IOT_DELIVERY_YUNKA_HTTP_ADDR`、`IOT_DELIVERY_YUNKA_GRPC_ADDR`、`IOT_DELIVERY_YUNKA_DB`、`IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT` 覆盖。只有在明确设置最后一个变量时，才会写入对应 Vault；正式 Vault 的切换不属于本 MVP 的自动行为。

每次启动或交付事项变更后，运行时会覆盖当天的本地快照，而不会改写历史日期：

- `10-交付管理/00-每日驾驶舱/YYYY-MM-DD-交付驾驶舱.md`：五个板块与受阻/逾期关注项。
- `10-交付管理/00-每日驾驶舱/YYYY-MM-DD-研发交付效能.md` 等板块页：点击后查看该板块的任务，再进入规划、方案、验证和复盘笔记。

## 本地前端

在另一个 PowerShell 中，使用与后端相同的本地管理员密钥启动 Next 桌面工作台：

```powershell
cd ../web
$env:IOT_DELIVERY_LOCAL_API_KEY = '<与后端相同的仅本地管理员密钥>'
npm run dev
```

Next 只在服务端的本机 `/api/*` 与 `/health` 代理向 `127.0.0.1:8281` 转发 `X-API-Key`；密钥不会进入浏览器代码、构建产物、数据库、Vault 或日志。当前 UI 是桌面优先工作台，不提供移动端布局承诺。

## 本地通知与可插拔通道

默认只注册耐久的 `local-inbox`：交付事项和项目的变更事件、截止日提醒会经 SQLite Outbox 投递到本地收件箱，前端可从 `/api/notifications` 查看。未设置下列环境变量时，不会向外部网络发送通知。

已内置的通道由 `yunka-bootstrap` 在配置完整时装配：通用 Webhook（`IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL`，可选 `IOT_DELIVERY_NOTIFICATION_WEBHOOK_SECRET`）、企业微信机器人（`IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL`）和 SMTP（`IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS`、`IOT_DELIVERY_NOTIFICATION_SMTP_FROM`、`IOT_DELIVERY_NOTIFICATION_SMTP_TO`；用户名和密码需成对提供）。可选 `*_NAME` 变量可更改通道名。任何部分配置都会使启动失败，避免悄悄遗漏投递。

通用 Webhook 发送 JSON、`Idempotency-Key` 和事件头；设置 secret 时附加 HMAC-SHA256 签名。企业微信机器人使用 Markdown 载荷。SMTP 可通过标准 SMTP relay 投递。通道返回非 2xx 或投递错误时会回传给 SQLite Outbox，由既有的租约、退避重试和死信状态治理；接收端应按 `Idempotency-Key` 去重。

截止日 worker 默认提前 1 个自然日、每小时扫描一次，可用 `IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS`（非负整数）和 `IOT_DELIVERY_DUE_REMINDER_INTERVAL`（Go duration，如 `30m`）调整。已完成事项不会提醒；同一开放事项每天只会写入一条稳定 ID 的 Outbox 提醒事件，因此重启和重复扫描不会重复投递。

要接入新渠道，只需实现 `internal/notification.Channel` 的 `Name()` 与 `Deliver()`，并在 `bootstrap.Config.NotificationChannels` 中注册，不需要改交付领域、Outbox 或 HTTP/MCP 代码。凭据只从运行环境读取；本地 MVP 尚未提供密钥托管、按成员偏好/升级规则、OAuth 邮件发送或生产级 SMTP TLS 策略。

## MCP 生命周期接入

本地 stdio MCP Server 仅支持 `development`，复用 Yunka 应用用例边界和本地 API Key 角色，不直接访问仓储；production 会在认证、SQLite 或监听器启动前明确拒绝该入口：

```powershell
cd backend-yunka
$env:IOT_DELIVERY_RUNTIME_ENVIRONMENT = 'development'
$env:IOT_DELIVERY_BOOTSTRAP_MODE = 'disabled'
$env:IOT_DELIVERY_LOCAL_API_KEY = '<仅本地使用的管理员密钥>'
$env:IOT_DELIVERY_MCP_API_KEY = $env:IOT_DELIVERY_LOCAL_API_KEY
go run ./cmd/iot-delivery-mcp
```

可使用 `IOT_DELIVERY_MCP_HTTP_ADDR`、`IOT_DELIVERY_MCP_GRPC_ADDR`、`IOT_DELIVERY_YUNKA_DB` 与 `IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT` 指定本地运行路径。任务工具之外还提供 `delivery.get_project_progress` 与 `delivery.get_project_schedule`，后者返回逾期、未排期、依赖阻塞和负责人剩余估算。该命令会以进程内临时 HTTP/gRPC 端口启动 Yunka 运行时；不要同时让它和 `yunka-bootstrap` 打开同一个 SQLite 文件。要并行验证，请为 MCP 设置独立的数据库和 Vault 路径。

## 本地鉴权

`IOT_DELIVERY_LOCAL_API_KEY` 是必填环境变量，不会写入数据库、Vault、日志或源码。它映射到 `local-admin`。可选的角色密钥也只能从环境变量读取：

| 变量 | 角色 | 可访问权限 |
| --- | --- | --- |
| `IOT_DELIVERY_LOCAL_VIEWER_API_KEY` | `viewer` | 驾驶舱与事项读取 |
| `IOT_DELIVERY_LOCAL_CONTRIBUTOR_API_KEY` | `contributor` | 读取、创建和更新事项上下文 |
| `IOT_DELIVERY_LOCAL_RELEASE_MANAGER_API_KEY` | `release-manager` | 读取、创建/更新、关卡推进和关闭复盘 |
| `IOT_DELIVERY_LOCAL_API_KEY` | `local-admin` | 全部本地 MVP 权限 |

同一密钥不能分配给多个角色。业务 HTTP 请求需携带 `X-API-Key`；业务 gRPC 请求需携带 metadata `x-api-key`。`/health` 和 `/__yunka/diagnostics` 是 host-owned 运行状态端点，保持免鉴权。

## SQLite 事务 Outbox

每次创建、更新、关卡推进和关闭都会在同一个 Yunka `local` 执行事务中写入交付状态与 `iotd_outbox`；截止日 worker 也将稳定提醒事件写入该表。事件信封包含稳定 ID、类型、schema version 和发生时间；进程内 dispatcher 以至少一次语义发布到本地 broker，Obsidian consumer 以“当前状态全量投影”方式处理重复事件，本地通知收件箱以事件 ID 与通道组合去重。失败投递会保留诊断错误并进入退避重试或死信状态。

同一运行进程内，SQLite 使用单连接池、WAL 和 5 秒 busy timeout，命令事务、Outbox dispatcher 与本地收件箱不会因写入争用向 API 返回 `SQLITE_BUSY`。这不是多进程数据库协调方案：`yunka-bootstrap` 与 `iot-delivery-mcp` 仍不得同时打开同一 SQLite 文件。

这是本地最小化运行模型：Outbox 是 SQLite 持久化的，但 broker 不是外部持久化消息系统。没有 Kafka/NATS、云身份、租户模型、生产密钥治理或生产部署能力。

## 验证

```powershell
go test -count=1 ./...
```

## Yunka 合同生成

`contracts/proto/iot_delivery.proto` 是领域 API 合同；生成物和清单受 Yunka 管理。当前开发机使用了仓库内忽略的 `.tools/`，固定版本为：

- `protoc 3.21.12`（官方发布资产名为 `protoc-21.12-win64.zip`）
- `protoc-gen-go v1.36.11`
- `protoc-gen-go-grpc v1.6.2`

工具就绪后：

```powershell
$target = (Get-Location).Path
$tools = Join-Path $target '.tools'
$env:PROTOC_GEN_GO = Join-Path $tools 'bin/protoc-gen-go.exe'
$env:PROTOC_GEN_GO_GRPC = Join-Path $tools 'bin/protoc-gen-go-grpc.exe'
cd ../third_party/yunka/app
go run ./cmd generate --root $target --full --protoc (Join-Path $tools 'protoc-3.21.12/bin/protoc.exe') --proto-path (Join-Path $target '../third_party/yunka/contracts/proto')
go run ./cmd check --root $target --full --protoc (Join-Path $tools 'protoc-3.21.12/bin/protoc.exe') --proto-path (Join-Path $target '../third_party/yunka/contracts/proto')
```

生成后的 `operation-plans.json`、应用端口、策略、RPC executor 和 `internal/assembly/` 都是受 Yunka 管理的派生内容；手写实现仅放在 `internal/delivery/application/`、`internal/localauth/`、`internal/localoutbox/`、`internal/localtx/`、`internal/notification/`、`internal/mcpserver/` 与 bootstrap 装配处。Project、Release、Sprint、Milestone 的创建 operation plan 已纳入生成合同；其余扩展 operation plan 仍由后续硬化项处理。
