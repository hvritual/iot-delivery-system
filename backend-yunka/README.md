# IoT Delivery System · 当前运行与开发说明

本说明对应 YU-33 固定输入 `1da771dac46c1b10c2ea54a0fb4559316c20179b`，Yunka 固定为 `057ebcf88a87303eb633eb6e604d306f633dfac0`。YU-33 只改变文档；精确最终提交及回归回执见交付附带的 `YU-33-FINAL-RECEIPT.json`。

## 1. 入口与主数据

`cmd/yunka-bootstrap` 为 HTTP/gRPC 常驻入口；`cmd/iot-delivery-mcp` 为 development-only stdio 入口；`cmd/yu29-fixture` **仅用于可销毁的测试环境**，会删除传入的数据库及 fixture manifest，绝不能对正式路径执行。

| 项目 | 默认值（从 backend-yunka 目录启动） | 环境变量 |
| --- | --- | --- |
| HTTP | `127.0.0.1:8281` | `IOT_DELIVERY_YUNKA_HTTP_ADDR` |
| gRPC | `127.0.0.1:8282` | `IOT_DELIVERY_YUNKA_GRPC_ADDR` |
| SQLite | `data/iot-delivery-yunka.db` | `IOT_DELIVERY_YUNKA_DB` |
| Vault | `runtime-vault/` | `IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT` |

默认 Vault 也会被投影写入；只有明确配置才能指向其他路径，禁止无授权指向正式 Vault。旧 `../backend/data` 不被新运行时自动读取或迁移。

## 2. 运行模式与身份配置

`IOT_DELIVERY_RUNTIME_ENVIRONMENT` 必须明确为 `development` 或 `production`；`IOT_DELIVERY_BOOTSTRAP_MODE` 默认 `disabled`。production 拒绝 example seed、legacy local API-key 配置及不安全 service credential development 开关。`production` 是运行时安全模式，不等于已完成生产部署认证。

| 配置 | 实际用途与边界 |
| --- | --- |
| `IOT_DELIVERY_LOCAL_AUTH_JWT_KEY` | 启用本地成员登录/session/JWT 能力；canonical base64url、无填充、至少 32 字节；与 BFF assertion key 分离 |
| `IOT_DELIVERY_BFF_ORGANIZATION_ID` + `IOT_DELIVERY_BFF_ASSERTION_KEY` | 必须成对；当前 production bootstrap 即使使用本地成员登录，也仍要求这对配置 |
| `IOT_DELIVERY_API_TARGET`（Next） | 后端目标，默认 `http://127.0.0.1:8281` |
| `IOT_DELIVERY_WEB_ORIGIN`（Next） | 浏览器精确 Origin，例如 `http://localhost:5173`；不能与 `127.0.0.1` 混用，也不附带尾部斜杠 |
| `IOT_DELIVERY_LOCAL_API_KEY` | 可选 development legacy 兼容身份；本地成员模式不依赖它，production 禁止它 |

当前 production 业务入口支持已验证的本地 JWT，并保留分离的 BFF assertion/service 身份链；`/auth/local/*` 自行执行 session、Origin、CSRF 与成员管理授权。不得再描述为“production HTTP 只接受 BFF assertion”。浏览器无需配置 OIDC 才能使用本地登录；OIDC 走独立路由，不伪造 issuer/subject。

本地登录输入是 `organizationId`、`userId`、`password`，不是邮箱自动并号。默认不透明 session 为 12 小时、内部 JWT 为 5 分钟。有效 Principal 仍须经过持久 GrantResolver/OperationGuard；角色撤销下一请求生效，密码重置和停用使旧凭据/会话失效。UI 只表达权限，不构成授权依据。

**真实首次安装的限制：** Organization 需先存在；一次性管理员初始化目前为进程内 `AdministratorBootstrap().Initialize` 端口，没有通用生产组织创建/管理员初始化 CLI 或公开 HTTP 初始化入口。不得用手工 SQL、example seed 或测试 fixture 冒充受支持的生产开通/恢复方案。正式初始化工具及责任人仍列为遗留项。

## 3. 可复现的隔离演示（Linux / Bash）

以下步骤仅创建临时测试数据，不连接正式库、Vault 或通知目标。先使用干净终端，清除已有 `IOT_DELIVERY_*` 测试外配置；禁止 `set -x` 或上传 fixture.json。

准备依赖（仓库根目录）：

```bash
git submodule update --init --recursive
export GOWORK=off GOTOOLCHAIN=local
test "$(go version | awk '{print $3}')" = go1.25.13
test "$(node --version)" = v22.16.0
(cd web && npm ci)
```

自动演示与浏览器验收：

```bash
(cd web && npm run e2e:yu29)
```

要求真实 Chrome/Chromium 可用；可用 `IOT_DELIVERY_E2E_BROWSER` 指定路径。脚本占用 `5173/8281/8282`，生成独立临时库/Vault，并默认清理，不要让正式服务占用这些端口。

手动演示：在后端终端执行，保存私有临时目录供第二终端读取；**fixture 会覆盖传入的 DB，因此只能使用本命令新建的目录**。

```bash
umask 077
export DEMO_DIR="$(mktemp -d /tmp/iotd-demo.XXXXXX)"
export GOWORK=off GOTOOLCHAIN=local
cd backend-yunka
go run ./cmd/yu29-fixture -db "$DEMO_DIR/demo.db" -vault "$DEMO_DIR/vault" -manifest "$DEMO_DIR/fixture.json"
fixture_value() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$DEMO_DIR/fixture.json" "$1"; }
export IOT_DELIVERY_RUNTIME_ENVIRONMENT=production
export IOT_DELIVERY_BOOTSTRAP_MODE=disabled
export IOT_DELIVERY_YUNKA_DB="$DEMO_DIR/demo.db"
export IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT="$DEMO_DIR/vault"
export IOT_DELIVERY_BFF_ORGANIZATION_ID="$(fixture_value organizationId)"
export IOT_DELIVERY_BFF_ASSERTION_KEY="$(fixture_value bffAssertionKey)"
export IOT_DELIVERY_LOCAL_AUTH_JWT_KEY="$(fixture_value localAuthJwtKey)"
go run ./cmd/yunka-bootstrap
```

前端终端从仓库根目录执行：

```bash
cd web
export IOT_DELIVERY_API_TARGET=http://127.0.0.1:8281
export IOT_DELIVERY_WEB_ORIGIN=http://localhost:5173
npm run dev
```

浏览器访问 `http://localhost:5173`，仅在本机私下读取 fixture.json 的 `organizationId`、`adminUserId`、`adminPassword` 或成员对应字段登录。普通成员在项目角色未分配前被拒绝是预期结果，不应开启管理员 fallback。演示停止后只清理自己新建的临时目录；不把本流程当生产恢复。

## 4. 健康、错误与恢复边界

`GET /health` 与 `GET /__yunka/diagnostics` 为 host-owned 免鉴权端点，production 外部暴露需入口控制。当前 HTTP 是手写 compatibility routes，生成诊断 inventory 的 `routeCount=0` 不代表运行路由不存在。

| 返回/现象 | 应对 |
| --- | --- |
| 401 | 核对凭据、会话到期/停用/重置；重新登录，不能绕过验签 |
| 403 | 核对同源、CSRF 和当前项目 RoleBinding；不同浏览器 context 不能互用 token |
| 409 / revision conflict | 回读对象/会话最新 revision，再由操作者重新决定；不盲目覆盖 |
| 429 + Retry-After | 等待服务器要求的冷却；不能清空安全计数表逃避限流 |
| 503 | 核查身份配置、限流存储和审计/数据库可用性；禁止内存 fallback 放行 |
| unhealthy / Outbox 积压 | 先保存脱敏诊断和日志，在隔离副本上调查；不要删除业务表或事件来制造健康状态 |

SIGTERM/SIGINT 有序关闭及重启持久化已有 Linux 真实进程测试；这些证据不覆盖断电恢复、所有 goroutine 泄漏或 Windows/macOS 关闭行为。备份/恢复与正式数据回滚尚无生产认证；切换前必须由运行责任人演练，不能仅回退二进制便声称数据库已回滚。

## 5. MCP、通知与事务

MCP 三种凭据只能显式选择一种：`IOT_DELIVERY_MCP_SESSION_TOKEN`、`IOT_DELIVERY_MCP_ACCESS_TOKEN`、`IOT_DELIVERY_MCP_API_KEY`。前两种是实际本地成员身份，需要匹配数据库及 `IOT_DELIVERY_LOCAL_AUTH_JWT_KEY`，每次请求重新验证身份与持久授权；API Key 仅属开发兼容模式。入口仍拒绝 production；未提供远程 MCP/OAuth 产品化。

```bash
# 仅在已配置真实成员凭据的隔离 development 环境执行
cd backend-yunka
export IOT_DELIVERY_RUNTIME_ENVIRONMENT=development
export IOT_DELIVERY_BOOTSTRAP_MODE=disabled
go run ./cmd/iot-delivery-mcp
```

MCP 临时监听默认 `127.0.0.1:0`，可用 `IOT_DELIVERY_MCP_HTTP_ADDR` / `IOT_DELIVERY_MCP_GRPC_ADDR` 覆盖。YU-31 已在真实受控场景让 HTTP/gRPC 与 MCP 共享同一 SQLite 并验证撤权与重启；**这只证明该测试组合，不是任意多进程或多节点数据库部署保证**。日常独立演示优先隔离数据库/Vault；共享时必须使用同一权威数据库与一致密钥，并自行承担待认证的拓扑边界。

业务状态、审计与 Outbox 由根执行事务治理；事件经持久 SQLite Outbox → 进程内 broker → 幂等投影/通知收件箱。安全限流预留是独立先提交的安全控制事务，不应因业务拒绝回滚；因此“拒绝零业务事件”不等于“安全计数和拒绝审计零变更”。

同进程 SQLite 为单连接、WAL 与 5 秒 busy timeout。该设置不应被解释为任何负载下永不 SQLITE_BUSY。broker 不是外部可靠消息系统；未采用 Provider-managed MySQL、Kafka/NATS 或分布式事务。

默认只有 local-inbox；外部通道须经授权且完整配置后启用：Webhook 的 `IOT_DELIVERY_NOTIFICATION_WEBHOOK_URL` / 可选 `_SECRET`；企业微信的 `IOT_DELIVERY_NOTIFICATION_WECOM_WEBHOOK_URL`；SMTP 的 `IOT_DELIVERY_NOTIFICATION_SMTP_ADDRESS`、`_FROM`、`_TO`，用户名/密码成对配置。失败进入现有 Outbox 重试/死信路径；不把适配器测试冒充真实投递验收。

截止日提醒默认提前 1 天、每小时扫描；变量为 `IOT_DELIVERY_DUE_REMINDER_LEAD_DAYS` 与 `IOT_DELIVERY_DUE_REMINDER_INTERVAL`。同一开放事项每天稳定事件 ID 去重，完成项跳过。Obsidian `10-交付管理/` 为可重建的单向投影，不作为反向业务输入。

## 6. 合同、工具链与完整回归

当前 DeliveryService 为 **25 个生成 operation plan**。项目/版本/Sprint/里程碑、事项读写、保存视图、成员周视图、项目进度/排期和通知读取均已合同化。配置修订三项及身份相关手写内部计划不计入这 25 个，不代表它们有生成 gRPC 服务。

固定工具链：Go 1.25.13、Node 22.16.0、protoc 3.21.12（release 21.12）、protoc-gen-go v1.36.11、protoc-gen-go-grpc 1.6.2。依赖以 go.sum / package-lock.json 为准，不把安装时的新版本替换为验收版本。

GNU Make 从 `backend-yunka` 目录执行；默认工具位置 `.tools/protoc-21.12/bin/protoc`、`.tools/bin/protoc-gen-go`、`.tools/bin/protoc-gen-go-grpc`。`GO` / `TOOLS_DIR` 可指定已有工具位置。Yunka CLI 显式使用 `third_party/yunka/go.work`，消费者 Go 命令使用 `GOWORK=off`，不能把两者混为同一工作区。

```bash
cd backend-yunka
make yunka-context
make yunka-toolchain-check
make yunka-generate
make yunka-check
make yunka-verify
```

`yunka-verify` 是只读 full check 别名，不自动修复漂移。生成必须显式执行，生成后核对 diff；禁止手改 generated。Makefile 固定 repository-root profile 和 framework protobuf include，不能用临时复制 DSL 文件规避边界。

从仓库根目录执行全部门禁（需干净已提交工作树）：

```bash
bash backend-yunka/scripts/run-yu32h-red-green.sh
bash backend-yunka/scripts/run-yu30-regression.sh
bash backend-yunka/scripts/run-yu31-smoke.sh
```

YU-30 包含双次生成/检查、实际 ownership/audit/ChangeSet、Go test/vet/race、npm ci/test/typecheck/build/audit、真实 YU-29 E2E；治理 audit 要求零当前 existing/new proven debt，但仅覆盖其已实现规则。ChangeSet 正/反/恢复检查只认证当前 canonical operations，不追认所有历史变更。

PowerShell 的历史脚本 `scripts/run-s0-04-09-hard-gate.ps1` 保留，不以其替代本轮 Linux 的完整认证。Windows 工具需显式 `.exe` 路径；当前最终认证不扩展为 Windows/macOS 一致性保证。

上游 #149/#150/#151 已关闭，但固定 gitlink 不变，root profile、显式 ChangeSet 控制文件路径等消费者适配仍保留。采用新框架须单独升级和重新验收；不能因 Issue 关闭直接删除 workaround。

## YU-32H 本地密码安全合同

新建管理员、创建成员、管理员重置及本人改密共用 `localcredential.ValidateNewPassword`：至少 **15 个 Unicode 码点**，最多 **4096 字节**，有效 UTF-8；允许空格、中文、长密码及密码管理器，不修剪、不截断、不强制字符组合。旧密码仍按原始字节验证；仅已验证的原密码可以迁移旧哈希工作因子，不能借 rehash 设置任意弱密码。已有哈希无法反推出密码长度，存量账号应在受控轮换时应用新策略；不静默重置或锁死已有账号。

每个账号（OrganizationID + UserID）最多 **10 次 / 5 分钟**密码验证，下一次触发 **15 分钟**冷却；每个连接来源最多 **120 次 / 1 分钟**，下一次触发 **1 分钟**冷却。预算包括成功尝试，登录和本人改密共用账号预算；成功不清除并发请求已预留的次数。冷却期间的请求不延长锁定，届满自动恢复；既有有效会话、退出及管理员重置流程不因限流自动撤销。

计数保存在同一 SQLite 的 `iotd_local_password_attempts`，使用短写事务在业务根事务和 Argon2 前预留次数。认证失败、业务回滚、重启以及共享该数据库的多个 Manager/进程不能清零或重复领取预算。数据库不可用、表损坏或容量耗尽时 fail closed，不使用内存 fallback。最多保留 4096 个活动来源/账号桶，并在每次预留时清理过期记录；桶只存 SHA-256 标识摘要和计数/时间，不保存密码、原始账号或 IP。摘要不构成匿名化承诺。

网络来源只取 Go HTTP 连接的 `RemoteAddr`；忽略 `Forwarded`、`X-Forwarded-For` 和 `X-Real-IP`，IPv4-mapped IPv6 归一化为 IPv4，IPv6 按 /64 合并。Next BFF 不转发浏览器伪造的来源头；经过 BFF 的用户共享 BFF 的来源预算，但账号预算仍独立。无真实连接上下文的内部调用共享保守来源桶。这是明确的“不信任代理头”模式，不宣称已支持任意多级代理的真实客户端 IP 解析。网络层的独立客户端限流须由受控入口另行配置。

HTTP 被限流返回 `429` + `Retry-After` + `no-store`，不签发 cookie/session/JWT、不泄露账号是否存在；Next 保留该响应及等待时间，界面提示临时限流。锁定转换产生脱敏安全审计，已锁定期间不逐次放大审计日志。密码不合格返回 `400`；限流存储故障返回 `503`。

跨实例保证仅覆盖**同一权威 SQLite**，不包括使用彼此独立数据库副本的集群；这不是分布式 SQLite 生产拓扑认证。仍需 HTTPS、入口 DDoS/请求体限制、凭据泄露监控及组织级弱密码/泄露密码清单策略。本轮不声称完整 NIST 合规或生产上线认证。

复验：`bash backend-yunka/scripts/run-yu32h-red-green.sh`、完整 YU-30 回归、YU-31 真实运行时验证。首条命令把只依赖旧 API 的两个回归复制到固定父提交，要求出现短密码被接受和重复猜测未限流的真实失败，再验证当前实现；缺工具、编译失败不能代替 RED。

设计依据（2026-09-06 查阅）：NIST SP 800-63B-4 Password Verifiers / Rate Limiting（https://pages.nist.gov/800-63-4/sp800-63b/authenticators/）；OWASP Authentication Cheat Sheet / Login Throttling（https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html）。限流阈值是本产品本轮采用的明确合同，不是声称上述标准指定的默认值。
