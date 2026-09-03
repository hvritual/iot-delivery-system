# IoT Delivery System

面向 IoT 软件研发交付的本地任务执行系统。它将规划、方案、决策、关卡证据、发布验证和复盘集中在一个可操作的驾驶舱中，并单向沉淀到 Obsidian。

## 当前本地 R2 能力

- 五板块每日总览：设备质量与连接、产品与平台能力、研发交付效能、运营保障与安全、客户与业务价值。
- 交付事项：负责人、优先级、规划、方案、阻塞项和 ADR 决策。
- 关卡治理：规划确认 → 方案评审 → 研发完成 → 测试通过 → 生产验证；每次推进都必须附证据。
- 关闭治理：仅生产验证后可关闭，且必须填写复盘。
- 项目、发布版本、Sprint、里程碑、Epic、任务、子任务和缺陷；依赖关系会阻止循环依赖，并在项目健康视图中解释逾期、未排期、依赖阻塞与负责人剩余估算。
- 任务相似度确认、编辑、评论/活动审计、搜索筛选/保存视图、成员周视图，以及按估算权重汇总的项目进度。
- IoT 范围（设备、固件、客户、环境、灰度批次）与 PR、构建、测试、缺陷、发布证据关联。
- SQLite 为任务状态主数据源；Obsidian 是自动生成的只读沉淀层。
- Yunka 负责 API 运行时生命周期、健康检查、有序关闭、声明式本地鉴权和事务 Outbox。

## 本地启动

### 旧后端对照（仅迁移验证）

首次准备：

```powershell
git submodule update --init --recursive
cd web
npm install
```

启动旧后端（默认写入 `F:\knowledge\10-交付管理`）：

```powershell
cd backend
$env:IOT_DELIVERY_OBSIDIAN_VAULT = 'F:\knowledge'
go run ./cmd/server
```

在另一终端临时将新桌面工作台指向旧后端：

```powershell
cd web
$env:IOT_DELIVERY_API_TARGET = 'http://127.0.0.1:8181'
npm run dev
```

打开 `http://127.0.0.1:5173`。旧后端默认 API 地址为 `http://127.0.0.1:8181`，本地数据保存在 `backend/data/iot-delivery.db`，不会提交到仓库；它仅用于迁移期间的对照，不是 Yunka 的主数据源。对照模式会保留五板块驾驶舱的只读浏览，并明确提示 R2 项目工作流需要 Yunka 后端，不会因旧接口缺失而整页报错。

## Yunka 并行 MVP

`backend-yunka/` 是已验证的并行改造目标：它保留现有 HTTP 前端契约，同时由 Yunka 的 `runtimehost → kernel → core.App` 承载 HTTP/gRPC 生命周期、健康检查、诊断与有序关闭。它默认不写入正式 Obsidian Vault，也不读取旧 SQLite 数据库。

```powershell
cd backend-yunka
$env:IOT_DELIVERY_LOCAL_API_KEY = '<仅本地使用的管理员密钥>'
go run ./cmd/yunka-bootstrap
```

默认地址为 HTTP `127.0.0.1:8281`、gRPC `127.0.0.1:8282`，SQLite 写入 `backend-yunka/data/iot-delivery-yunka.db`，Obsidian 投影写入 `backend-yunka/runtime-vault/`。可使用 `IOT_DELIVERY_YUNKA_HTTP_ADDR`、`IOT_DELIVERY_YUNKA_GRPC_ADDR`、`IOT_DELIVERY_YUNKA_DB` 与 `IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT` 覆盖。

在另一个 PowerShell 中，使用相同的本地管理员密钥启动桌面工作台：

```powershell
cd web
$env:IOT_DELIVERY_LOCAL_API_KEY = '<与 Yunka 后端相同的仅本地管理员密钥>'
npm run dev
```

打开 `http://127.0.0.1:5173`。前端已迁移为 Next App Router + Tailwind + shadcn `base-nova` 桌面工作台；Next 仅在服务端的本机 `/api/*` 和 `/health` 路由向 `127.0.0.1:8281` 转发 `X-API-Key`，密钥不会进入浏览器代码、构建产物、数据库、Vault 或日志。要临时回连旧后端或指定其他本地环境，启动前设置 `IOT_DELIVERY_API_TARGET` 即可，无需修改源码。

当前迁移以桌面工作台为验收范围，不提供移动端布局或抽屉交互承诺。

该并行 R2 工作台支持项目/版本/Sprint/里程碑、Epic/任务/子任务/缺陷与依赖、相似任务确认、事项评论和活动审计、保存视图、成员周视图、项目进度和项目排期健康、IoT 范围与研发证据关联。变更与截止日提醒先进入本地收件箱；Webhook、企业微信机器人和 SMTP 通道只会在完整环境变量显式配置后启用。它也提供本地 stdio MCP Server，用于任务生命周期、项目进度与项目交付健康查询。具体运行方式、通知变量、MCP 的 SQLite 并发限制以及生成合同边界见 [backend-yunka/README.md](backend-yunka/README.md)。

### 可选的通知与提醒配置

默认不发生任何外部投递。仅当已取得目标渠道授权且在运行环境中设置完整配置时，`yunka-bootstrap` 才会注册对应通道：

```powershell
# 三种通道均为可选；请只设置获授权的目标。
$env:IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL = 'https://example.invalid/iot-delivery'
$env:IOT_DELIVERY_NOTIFICATION_WEBHOOK_SECRET = '<Webhook HMAC secret，可选>'
$env:IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL = 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=<key>'
$env:IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS = 'smtp.example.invalid:587'
$env:IOT_DELIVERY_NOTIFICATION_SMTP_FROM = 'iot-delivery@example.invalid'
$env:IOT_DELIVERY_NOTIFICATION_SMTP_TO = 'team@example.invalid'

# 默认提前 1 天、每小时扫描一次；已完成事项不提醒，同一事项每天最多产生一个提醒事件。
$env:IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS = '1'
$env:IOT_DELIVERY_DUE_REMINDER_INTERVAL = '1h'
```

密钥和实际目标地址只应由运行环境或密钥管理系统提供，不应写入仓库、SQLite、Obsidian 或日志。

详细的已采用能力、未覆盖边界与切换条件见 [docs/YUNKA-MVP-MIGRATION.md](docs/YUNKA-MVP-MIGRATION.md)。

## Obsidian 约定

每次创建或更新交付事项，以及每次服务启动，系统会刷新：

```text
10-交付管理/
  00-交付总览.md
  01-规划/
  02-方案/
  03-决策/
  04-发布与验证/
  05-复盘/
```

旧后端生成的笔记带有 `generated_by: "iot-delivery-system/v1"` 和 `source_of_truth: "iot-delivery-system"`；Yunka 并行 MVP 生成 `generated_by: "iot-delivery-system-yunka/v1"`。请在交付系统中修改任务，不要直接编辑生成页面；Yunka 投影还会记录事项层级/排期、IoT 范围、研发关联、评论和活动审计。

## 验证

```powershell
cd backend-yunka
go test -count=1 ./...

cd ../web
npm test
npm run typecheck
npm run build
```

来源、复用边界和许可判断见 [docs/SOURCE-MANIFEST.md](docs/SOURCE-MANIFEST.md)。
