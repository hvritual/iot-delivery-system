from pathlib import Path
import hashlib, json, re, subprocess, sys, textwrap

ROOT = Path.cwd()
PARENT = '1da771dac46c1b10c2ea54a0fb4559316c20179b'
FRAMEWORK = '057ebcf88a87303eb633eb6e604d306f633dfac0'
U = 'docs/target/yunka-upgrade/'

def git(*args):
    return subprocess.check_output(['git', *args], text=True).strip()

def write(path, value):
    Path(path).write_text(textwrap.dedent(value).strip() + '\n', encoding='utf-8')

def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

assert git('rev-parse', 'HEAD') == PARENT
assert git('ls-tree', 'HEAD', 'third_party/yunka').split()[2] == FRAMEWORK
assert not git('status', '--porcelain', '--untracked-files=all')
operations = json.loads(Path('backend-yunka/contracts/generated/operation-plans.json').read_text())['operations']
assert len(operations) == 25
old_backend = Path('backend-yunka/README.md').read_text()
password_section = old_backend[old_backend.index('## YU-32H 本地密码安全合同'):]
assert '当前声明 19 个 operation plan' in old_backend
assert 'IOT_DELIVERY_LOCAL_API_KEY` 是必填' in old_backend
assert '13 个 operation plan' in Path('docs/YUNKA-MVP-MIGRATION.md').read_text()
assert '9a51562' in Path('docs/SOURCE-MANIFEST.md').read_text()

write('README.md', '''
# IoT Delivery System

面向 IoT 软件研发交付的本地工作台：把项目、事项、方案、决策、关卡证据和复盘集中管理，并单向投影到 Obsidian。

## 当前交付基线

YU-33 是 YU-32H 之后的**文档与交付清单收口**，不新增功能、不部署、不迁移正式数据。

- 固定输入：`1da771dac46c1b10c2ea54a0fb4559316c20179b`；框架 gitlink：`057ebcf88a87303eb633eb6e604d306f633dfac0`。
- 当前应用：`backend-yunka/`；前端：`web/`。`backend/` 仅保留作历史对照，禁止作为默认新启动入口。
- 当前业务合同为 **25 个生成 operation plan**；身份管理与配置修订保留显式手写内部合同，不能把它们冒称生成 RPC。
- 本地成员密码、独立会话、项目角色、CSRF、停用/重置/撤权与密码限流已实现；legacy API Key 仅是显式启用的开发兼容模式，不是成员登录必需配置。

完整事实源：[运行说明](backend-yunka/README.md)、[架构边界](docs/ARCHITECTURE.md)、[最终清单](docs/target/yunka-upgrade/FINAL-MANIFEST.json)、[遗留项](docs/target/yunka-upgrade/RESIDUAL-RISKS.md)、[任务台账](docs/target/yunka-upgrade/TASKS.md)。历史证据只证明其命名提交，不覆盖后来版本。

## 新开发者验证入口

先检出获验收的精确提交，初始化子模块。使用 Go **1.25.13**、Node **22.16.0**；生成工具版本与安装布局见运行说明。不要通过自动下载其他 Go 版本绕过版本门禁。

```bash
git submodule update --init --recursive
export GOWORK=off GOTOOLCHAIN=local
cd web
npm ci
npm run e2e:yu29
```

该 E2E 使用真实 Chrome/Chromium、真实成员账号和独立浏览器 context；临时创建数据库及 Vault，验证结束后清理。需要空闲的 `5173/8281/8282` 端口，并在干净的测试环境执行。它不是生产部署、数据迁移或正式 Vault 切换。手动登录工作台的隔离演示步骤见运行说明。

完整验收从仓库根目录执行：

```bash
bash backend-yunka/scripts/run-yu32h-red-green.sh
bash backend-yunka/scripts/run-yu30-regression.sh
bash backend-yunka/scripts/run-yu31-smoke.sh
```

YU-30 要求已提交且干净的工作树、完整 Git 历史、固定框架及已安装的 protobuf 工具；YU-31 真实进程认证以 Linux 为范围。缺工具不算产品 RED，也不算 PASS。

## 已实现的工作流

五板块驾驶舱；项目/版本/Sprint/里程碑；Epic/任务/子任务/缺陷；依赖、相似度、搜索、保存视图、成员周视图、进度与排期健康；IoT 范围与研发证据关联；带 CAS、证据和职责分离的推进/关闭；SQLite 主数据、事务 Outbox、审计、本地通知及 Obsidian 单向投影。

浏览器使用 Next App Router 的 BFF 与本地会话，业务授权来自 SQLite 中的有效身份和项目 RoleBinding，不以显示名、邮箱或前端按钮可见性授权。OIDC 为单独的可选路径。本轮仍未注册外部 MCP 客户端或验收真实外部通知投递。

## 交付完成与上线分开

最终提交必须自行通过完整回归、文档核对、artifact SHA/工作树回读，再 non-force 合并 main。提交后的 CI ID 与 main 回执保存在本次交付的独立 `YU-33-FINAL-RECEIPT.json`，不在提交内伪造自引用 SHA。

正式切换还需数据迁移授权、副本对账、Vault 差异检查、备份恢复和运行责任确认，见 [迁移门槛](docs/YUNKA-MVP-MIGRATION.md)。证据通过不代表零风险或全面生产认证。[来源与发布边界](docs/SOURCE-MANIFEST.md)单独保留。
''')

write('backend-yunka/README.md', '''
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
''')
with Path('backend-yunka/README.md').open('a', encoding='utf-8') as f:
    f.write('\n' + password_section.rstrip() + '\n')

write('docs/ARCHITECTURE.md', '''
# 当前架构与数据边界

事实输入：YU-32H `1da771dac46c1b10c2ea54a0fb4559316c20179b`；本轮 YU-33 文档收口不改变运行代码。历史旧后端架构不再代表当前默认入口。

```text
Browser / Next App Router
  -> local-auth BFF: session + Origin + CSRF
  -> local JWT / separately configured OIDC-BFF and service identity
  -> runtimehost -> generated Assembly -> capability-aware runtime binder
  -> kernel / core.App
       HTTP compatibility + generated gRPC + development stdio MCP
         -> verified Principal
         -> SQLite GrantResolver + OperationGuard
         -> canonical Operations / root Executor
         -> SQLite business state + transactional audit + Outbox
              -> local broker -> Obsidian projection / notification inbox

Password verification
  -> independent durable attempt reservation BEFORE Argon2/root transaction
  -> authentication/session checks -> root transaction / CAS
```

## 事实源与所有权

| 事实 | 权威来源 | 非权威副本/边界 |
| --- | --- | --- |
| 项目、事项、身份、RoleBinding、session、Outbox | 当前配置指定的 backend-yunka SQLite | 旧 backend 数据库不自动导入 |
| 领域操作及派生物 | canonical protobuf + 固定 Yunka generator | 25 个 Delivery operation plan；内部身份/配置计划另列 |
| 权限决定 | 当前已验证 Principal + SQLite 有效授权 | 邮箱、显示名、JWT roles snapshot、UI 可见性不能替代持久授权 |
| 投影 | SQLite 事件驱动的 Obsidian 输出 | Vault 不反向改写业务；可重建，不作为备份替代 |
| 本轮源码与验证 | 精确 Git SHA/tree、CI job、artifact bytes | 旧任务 EVIDENCE / 旧 manifest 只代表对应历史提交 |

`backend-yunka/internal/delivery/application` 不直接导入 gateway/authz；SOD 错误在 `deliveryauthz` 边界归一化，保留 domain sentinel。业务拒绝/失败不得留下业务状态或 Outbox 的半提交；独立安全计数与拒绝审计可以产生安全控制写入。

## 生命周期与合同范围

生成 Assembly 与 typed capabilities 接入 SQLite/UoW 和 event runtime；运行时持有数据库、dispatcher、提醒 worker、broker subscriptions 与 broker 的生命周期。HTTP 为手写兼容适配，gRPC 使用生成 executor；MCP development 入口复用操作与真实成员授权链。

25 个生成 operation plan 见 [最终清单](target/yunka-upgrade/FINAL-MANIFEST.json)。项目 scope、CAS、职责分离、审计、业务/Outbox 回滚与真实 HTTP/gRPC/MCP 已有分层回归。关卡证据与复盘仍必需；通过测试不等于无限资源/所有故障模式的保证。

## 已知运行边界

单 SQLite/本地 broker 是明确的本地模型。YU-31 认证受控多进程共享场景，YU-32H 认证同一数据库持久限流预算；均不认证独立副本集群、断电恢复或跨平台完整资源回收。当前没有 Provider-managed MySQL、远程 MCP/OAuth、多节点事件可靠性或生产 TLS 部署认证。

首次 Organization/管理员生产初始化、旧数据/Vault 切换及备份恢复仍需单独流程。详见 [运行说明](../backend-yunka/README.md)、[遗留风险与责任](target/yunka-upgrade/RESIDUAL-RISKS.md) 和 [切换门槛](YUNKA-MVP-MIGRATION.md)。
''')

write('docs/YUNKA-MVP-MIGRATION.md', '''
# Yunka Upgrade 收口与正式切换门槛

本页为 YU-33 当前状态，输入固定于 `1da771dac46c1b10c2ea54a0fb4559316c20179b`。旧的 13/19 个 operation plan 和“成员登录尚未实现”描述已被当前源码与验证替代。历史逐任务证据保持不变。

| 范围 | 当前状态 | 证据入口 |
| --- | --- | --- |
| 云端源码/框架固定、25 个生成 Delivery plans | 已实现并有回归 | FINAL-MANIFEST、YU-30 |
| root UoW、Outbox、审计、typed runtime lifecycle | 已实现并有回归 | YU-15/16/17、YU-30/31 |
| 成员密码/session/JWT、项目角色、CSRF、双浏览器 | 已实现并有回归 | YU-18…29、YU-30 |
| AUDIT-AUTH-001 | 已修复；规则内 existing/new proven debt 为零 | YU-32 |
| 密码最低长度、猜测限流、持久计数与来源边界 | 已修复并有真实 RED/GREEN | YU-32H |
| 外部通知适配器 | 已实现；未验收真实外部投递 | YU-14 及运行说明 |
| stdio MCP | development-only；有真实进程测试 | YU-31 |
| 旧 SQLite / 正式 Vault 切换 | 未执行、不得自动执行 | 下述门槛 |
| 完整生产运行/安全认证 | 未完成，另行审批与验收 | RESIDUAL-RISKS |

## 正式切换门槛（本轮不执行）

1. 明确旧库迁移、正式 Vault 写入、切换窗口、回退条件和责任人的授权。
2. 只在副本上迁移并对账：ID、时间、状态、关卡证据、ADR、复盘、身份/项目归属与新增安全 schema；差异必须可解释。
3. 在隔离 Vault 对比投影；旧 backend 与新 backend-yunka 不同时写同一事实源。是否双写必须另行设计，不能擅自打开。
4. 完成备份恢复/回退演练，确认凭据、审计、告警、运行资源和 SLO 的责任分工。
5. 经过只读对账窗口及审批后才切换前端目标/正式地址；保留回滚材料，最后再决定旧系统退役。

本轮没有执行上述迁移、投递、上线或授权动作。SQLite 是消费者实现，不冒称框架原生 MySQL Provider 能力；本地 broker 也不冒称可靠外部消息系统。

[当前运行说明](../backend-yunka/README.md) · [最终清单](target/yunka-upgrade/FINAL-MANIFEST.json) · [遗留项](target/yunka-upgrade/RESIDUAL-RISKS.md) · [YU-33 证据](target/yunka-upgrade/YU-33-EVIDENCE.md)
''')

old_source = Path('docs/SOURCE-MANIFEST.md').read_text()
write('docs/SOURCE-MANIFEST.md', '''
# 当前来源与历史复用记录

## YU-33 当前版本

当前消费者输入为 `1da771dac46c1b10c2ea54a0fb4559316c20179b`，子模块固定为 `057ebcf88a87303eb633eb6e604d306f633dfac0`。已采用 generated operation plans、generated Assembly、typed capabilities、身份授权执行链与事务 Outbox；未采用 Provider-managed MySQL 或生产部署治理。

精确版本、文件哈希、工具链与证据索引由 [FINAL-MANIFEST.json](target/yunka-upgrade/FINAL-MANIFEST.json) 管理；最终收口提交由独立 YU-33-FINAL-RECEIPT.json 绑定。下方旧 pin 和未采用清单仅用于历史来源追溯，**不是当前能力清单**。

本轮不重新作许可证法律结论，也不把历史来源审查视为二进制或依赖再分发授权。公开消费者文档不包含 YU-00F 私有框架分析文件；对应输入限制保留在遗留项中。

## 历史记录（保留原文，旧基线语义）
''')
with Path('docs/SOURCE-MANIFEST.md').open('a', encoding='utf-8') as f:
    f.write('\n' + old_source.replace('# 来源与复用边界', '### 历史来源与复用边界', 1).replace('## 许可与发布门槛', '### 历史许可与发布门槛', 1).rstrip() + '\n')

write(U + 'RESIDUAL-RISKS.md', '''
# YU-33 已关闭问题、遗留项与责任边界

核对日期：2026-09-06。源码输入：`1da771dac46c1b10c2ea54a0fb4559316c20179b`。以下 owner 为**责任角色，具体人待指派**；记录风险不等于用户已接受风险，也不等于授权实施后续工作。

## 已关闭的消费者问题

| 问题 | 状态 | 证据 |
| --- | --- | --- |
| AUDIT-AUTH-001 应用层直接依赖 gateway/authz | repaired；规则内 current debt 已清零 | YU-32-EVIDENCE.md；最终 YU-32H audit |
| development 模式错误地强制要求 legacy API Key | repaired；真实成员 MCP 不再被旧密钥阻断 | YU-31-EVIDENCE.md |
| 最低密码长度、无限猜测、计数回滚/重启、代理信任边界缺少合同 | implemented and tested | YU-32-EVIDENCE.md 的 YU-32H 节；运行说明 |
| 接入限流造成管理员重置/本人改密 CAS 分类回归 | repaired；未放宽旧测试 | YU-32H-CI-RECEIPT.json；yu32h_reset_cas_test.go |
| 当前说明仍引用旧框架/13或19个 plans/旧认证入口 | 本轮文档修正；最终提交验收后关闭 | YU-33-EVIDENCE.md |

## 仍须后续处理

| ID / 状态 | 范围与风险 | Owner 角色 | 后续动作与完成标准 |
| --- | --- | --- | --- |
| R-01 pending-input | YU-00F 私有框架 requirements/problems/manifest 不在 verified checkout | 中控/架构负责人 | 在获授权私有渠道取得实际文件，验证 SHA 与基线后补充审查；本轮不从记忆重建，不公开上传 |
| R-02 pending-operation | 历史密码长度无法从哈希恢复，新策略不静默重置存量密码 | 身份管理员 | 制定受控轮换/重置计划，确认旧会话失效且不锁死合法账号 |
| R-03 topology-limited | 限流仅共享同一权威 SQLite；独立 DB 副本不共享预算 | 运行负责人 | 明确部署拓扑，按拟部署规模验证并发/故障；多副本方案须独立设计和验收 |
| R-04 ingress-control | BFF 用户共享来源预算；不信任任意 Forwarded/XFF | 网络/安全负责人 | 受控入口配置客户端限流、HTTPS、请求体和抗攻击策略；核实不允许伪造来源绕过账号预算 |
| R-05 pending-security | 泄露/弱密码黑名单、凭据泄露监控、密钥轮换及供应商治理未构成完整生产认证 | 安全负责人 | 单独建立策略、操作流程和可执行验收；不声称完整 NIST 合规 |
| R-06 pending-provisioning | Organization 前置且管理员初始化是进程内端口；无通用生产开通/恢复 CLI | 应用维护者/身份管理员 | 交付受审计的初始化/恢复流程；明确幂等/并发/身份授权；不能用 fixture 或手工 SQL 顶替 |
| R-07 not-executed | 旧库、正式 Vault、前端正式目标尚未切换 | 数据/运行负责人 | 取得授权，在副本完成迁移与全量对账、隔离投影比较及回退演练，再审批切换 |
| R-08 not-certified | Linux loopback smoke 不等于 Windows/macOS、断电恢复、长稳压测、TLS 或普遍资源无泄漏证明 | 测试/运行负责人 | 按实际 OS/负载/故障模型补测试；备份恢复、告警和 SLO 需独立验收 |
| R-09 integration-pending | 外部通知未真实验收；MCP 未外部客户端注册且为 development-only stdio | 集成负责人 | 先授权目标及凭据，再做外部投递/回读、客户端身份映射；远程 MCP/OAuth 为独立任务 |
| R-10 governance-retention | Actions artifacts 有到期时间；分支精确 SHA 门禁当前靠显式核验，不代表永久规则强制 | 研发治理负责人 | 将实际 artifact bytes 与回执保存到获授权长期存储，验证下载哈希；规则安装另行决策 |
| R-11 distribution-review | 历史许可记录不构成当前依赖/二进制对外再分发授权 | 项目/权利确认负责人 | 外部分发前核对实际固定依赖及授权，不以仓库可访问性替代权利确认 |

## 框架 Issue：当前状态与固定版本分开

2026-09-06 通过 GitHub 回读 `hvritual/yunka.io`：

| Issue | 服务端状态 | 关闭时间 UTC | 本项目固定框架的处理 |
| --- | --- | --- | --- |
| #149 nested-project Git path domain | closed / completed | 2026-09-05T11:26:48Z | 仍用 repository-root profile；本轮不升级 gitlink |
| #150 service/service-token vocabulary | closed / completed | 2026-09-05T11:47:26Z | 历史两阶段 ChangeSet 适配保留；不手改生成物 |
| #151 change contract 控制文件误判 scope | closed / completed | 2026-09-06T02:10:34Z | 现有验证使用显式外部控制文件路径；不删除适配 |

关闭状态不自动证明修复已进入本项目的 `057ebcf`，也不替代新版本消费者验证。本轮未重新认证上游修复代码、未修改框架、未新增框架问题；旧逐任务 JSON 的 open 状态作为当时证据保留，不冒称今天仍 open。

[运行说明](../../../backend-yunka/README.md) · [迁移门槛](../../YUNKA-MVP-MIGRATION.md) · [最终清单](FINAL-MANIFEST.json)
''')

p = Path(U + 'TASKS.md')
s = p.read_text()
s = s[:s.index('## 下一原子任务派发说明')] + '''## YU-33 最终文档与清单收口

固定 parent：`1da771dac46c1b10c2ea54a0fb4559316c20179b`。本轮仅变更 README、架构/迁移/来源说明、当前任务台账、最终 manifest、已关闭问题与遗留风险清单；旧逐任务证据和基线 manifest 保留 as-of 语义。

已按源码纠正 25 个生成 Delivery plans、本地成员认证入口、可选 legacy API Key、单 SQLite/MCP 已验证范围及密码安全合同。框架 #149/#150/#151 当前为 closed，但本项目的固定 gitlink 与适配未改变；YU-00F 实际私有输入不可用的限制继续保留。

最终文件：[FINAL-MANIFEST.json](FINAL-MANIFEST.json)、[RESIDUAL-RISKS.md](RESIDUAL-RISKS.md)、[YU-33-EVIDENCE.md](YU-33-EVIDENCE.md)。最终提交号、CI/run/artifact、文档检查和 main 回读由独立 `YU-33-FINAL-RECEIPT.json` 绑定，避免在待验证提交里把未来 CI 结果写成已完成事实。

结束条件：承载这些文档的精确提交通过完整 YU-30 回归、YU-31 runtime smoke、YU-32H RED/GREEN 与文档检查，artifact head 与哈希一致、零工作区漂移，再同步任务分支并 non-force 合并 main。仅文档存在或父提交 PASS 不满足结束条件。

## 调度边界

YU-33 为本次 Upgrade 的最后一个原子任务；完成上述门禁与 main 回读后收口并停止。没有自动派发 YU-34、生产切换或其他产品功能。后续工作由 RESIDUAL-RISKS 的责任角色与授权流程单独确定。
'''
p.write_text(s, encoding='utf-8')

write(U + 'YU-33-EVIDENCE.md', '''
# YU-33 文档事实漂移修复与交付验收

固定 consumer parent：`1da771dac46c1b10c2ea54a0fb4559316c20179b`。固定 framework：`057ebcf88a87303eb633eb6e604d306f633dfac0`。任务分支：`codex/yu-33-final-closeout`。只更新文档/JSON 清单；不部署、不改业务、不迁移数据。

## 实际读取与来源

通过 GitHub 回读 main 与 TASKS；下载固定提交的 consumer/framework Git bundles，核对 ZIP SHA-256、bundle 清单及 checkout SHA。该读操作不采集凭据或 runtime DB。YU-32H 最终 full/runtime ZIP 已在本轮重新校验实际字节哈希，两个 head.txt 为固定 parent，四个 worktree.txt/patch 为空；GitHub run 34009115037 的两个最终 job 回读成功。

YU-00F 私有 FRAMEWORK-REQUIREMENTS、FRAMEWORK-PROBLEMS 和 manifest 仍不在 verified source；不将它们计为本轮已读输入。历史证据文件的存在和哈希不等于其每个历史运行都在本轮重跑。

## RED：文档与当前源码真实冲突

| 项目 | 固定 parent 的文档 | 可执行事实 |
| --- | --- | --- |
| 生成合同 | 迁移页 13 个，backend README 19 个 | contracts/generated/operation-plans.json 有 25 个 |
| 身份入口 | local API Key 必填；production 仅 BFF assertion | local member session/JWT/BFF routes 已装配；legacy key 可选且 production 禁用 |
| 功能状态 | 迁移页仍称 YU-18…29 成员能力尚未实现 | local credential/login/member/role/UI 与最终真实 E2E 存在并已验收 |
| 数据事实源 | ARCHITECTURE 只列旧 backend DB 与正式 Windows Vault | 当前入口为 backend-yunka，默认隔离 DB/Vault |
| 来源 pin | SOURCE-MANIFEST 将 9a51562 与未采用生成装配作为当前 | gitlink 为 057ebcf，25 plans 与 generated Assembly 已采用 |
| MCP/并发 | 只描述 API Key 并把共享 DB 一律视为未验证 | YU-31 覆盖真实成员共享 DB 的受控场景，不能外推为生产集群保证 |
| 框架 Issue | 逐任务 JSON 当时状态 open | #149/#150/#151 本轮回读 closed；固定依赖仍未升级 |

RED 是既有文档和源码/真实证据的冲突，不是缺 Go 工具或运行环境失败。历史逐任务证据不重写为今天的状态；通过当前清单与历史分层解决漂移。

## 本轮交付

根 README 改为当前本地成员/隔离演示入口；backend README 记录环境变量、首次初始化限制、错误与恢复边界、MCP、Outbox、工具链和完整密码安全合同。架构和迁移页分开当前实现与未执行正式切换；来源页保留历史原文并显式标注。最终 manifest 记录固定 source/tree、非文档 Git 对象指纹、25 operations、关键源文件与历史证据哈希、工具链和原始运行身份。RESIDUAL-RISKS 给出状态、责任角色与完成标准，未擅自指派人员或接受风险。

## GREEN 与最终提交门禁

文档检查须验证：所有本轮 Markdown 本地文件链接存在；manifest 的输入文件哈希与仓库一致；operation ID 集合精确一致；framework pin 不变；相对固定 parent 的变更严格限于声明文档；所有非文档 Git 对象身份保持不变；历史 EVIDENCE/基线 JSON 未被改写。

本地环境 Go 1.23.2 不能冒充固定 1.25.13，直接 GitHub DNS 受限；因此本地仅执行文档/来源检查，产品验收使用既有 GitHub Actions。最终提交必须独立通过 YU-30 四项、YU-31 runtime、YU-32 full/runtime（含 YU-32H RED/GREEN），下载实际 artifacts 验证 SHA/head/空工作树之后才 non-force 合并 main。

**自引用规则：** FINAL-MANIFEST 中 source SHA 是未变动的可执行基线，不冒称未来提交。最终文档提交的精确 SHA/tree、运行 ID、artifact digest、文档检查结果、main 回读和清理由提交外 `YU-33-FINAL-RECEIPT.json` 记录，可作为 PR 回执与当前交付文件保存。不能为了把回执塞回原提交而制造“新提交沿用旧 PASS”的循环。

截至文档形成时最终提交尚待上述门禁；实际结果以该精确提交的 CI 与最终独立回执为准。通过后仅表示本轮 Upgrade 工程收口，不表示完成正式数据切换或全面生产安全认证。
''')

key_paths = ['.gitmodules','.yunka/project.json','backend-yunka/Makefile','backend-yunka/go.mod','backend-yunka/go.sum','web/package.json','web/package-lock.json','backend-yunka/contracts/proto/iot_delivery.proto','backend-yunka/contracts/generated/operation-plans.json','backend-yunka/contracts/generated/manifest.json','backend-yunka/internal/bootstrap/application.go','backend-yunka/internal/bootstrap/local_auth_bff.go','backend-yunka/internal/localtransportauth/http.go','backend-yunka/internal/localcredential/password.go','backend-yunka/internal/locallogin/throttle.go','backend-yunka/cmd/yu29-fixture/main.go','backend-yunka/cmd/yunka-bootstrap/main.go','backend-yunka/cmd/iot-delivery-mcp/main.go','web/scripts/yu29-e2e.mjs','.github/workflows/yu30-regression.yml','.github/workflows/yu31-runtime-smoke.yml','.github/workflows/yu32-independent-review.yml']
historical = sorted(str(p) for p in Path(U).glob('*') if p.is_file() and p.name not in ['TASKS.md','FINAL-MANIFEST.json','RESIDUAL-RISKS.md','YU-33-EVIDENCE.md'])
allowed = ['README.md','backend-yunka/README.md','docs/ARCHITECTURE.md','docs/YUNKA-MVP-MIGRATION.md','docs/SOURCE-MANIFEST.md',U+'TASKS.md',U+'RESIDUAL-RISKS.md',U+'YU-33-EVIDENCE.md',U+'FINAL-MANIFEST.json']
rows = subprocess.check_output(['git','ls-tree','-r',PARENT],text=True).splitlines()
protected = [row for row in rows if row.split('\t',1)[1] not in allowed]
manifest = {
 'schema_version':'1.0','task_id':'YU-33','as_of_date':'2026-09-06','repository':'hvritual/iot-delivery-system',
 'fixed_parent':PARENT,'executable_source_sha':PARENT,'source_tree':git('rev-parse',PARENT+'^{tree}'),
 'framework_sha':FRAMEWORK,'framework_mutated':False,'deployment_performed':False,
 'task_branch':'codex/yu-33-final-closeout','allowed_changed_paths':allowed,
 'unchanged_git_objects_sha256':hashlib.sha256(('\n'.join(protected)+'\n').encode()).hexdigest(),
 'fingerprint_rule':'SHA256 of git ls-tree -r fixed_parent lines, excluding allowed_changed_paths, joined by LF with terminal LF. All other paths, including historical docs and gitlink, must remain identical.',
 'generated_delivery_operation_count':len(operations),'generated_delivery_operation_ids':[o['operationId'] for o in operations],
 'internal_contracts_not_generated_rpc':['localbootstrap','localmemberadmin','locallogin','localprojectroleadmin','configapplication (three config revision operations)'],
 'toolchain':{'go':'1.25.13','node':'22.16.0','protoc':'3.21.12','protoc_gen_go':'1.36.11','protoc_gen_go_grpc':'1.6.2','ci_os':'ubuntu-24.04','browser':'real Chrome/Chromium; actual build is run-specific'},
 'evidence_scope':'Historical evidence hashes establish inventory/integrity, not independent re-execution of every historical claim.',
 'milestones':[
  {'task':'YU-00','source_sha':'4cd1209f41575a67ab143b83e1df6501089085b8','kind':'historical baseline'},
  {'task':'YU-30','source_sha':'2f533dc233584c3318b9788f7016dcfb131b99b7','run_id':33971774693,'kind':'historical named-source receipt'},
  {'task':'YU-31','source_sha':'20520f69d7eaf3c27c0fb3e9d79f03b4ecb059bd','runtime_run_id':34003340202,'regression_run_id':34003340165,'kind':'historical final source'},
  {'task':'YU-32','source_sha':'5e57424b034ae8da0ac27d6fb920b457005ea253','run_id':34005096838,'kind':'historical final source'},
  {'task':'YU-32H','source_sha':PARENT,'run_id':34009115037,'kind':'input preflight; job metadata and archive bytes rechecked'}],
 'input_artifacts':[
  {'run_id':34009115037,'job_id':101421676641,'artifact_id':9981974745,'name':'yu32-full-34009115037-1','sha256':'b045f6d37383ca92b8c385516d15b0b526356fe58a6684102a5a315ddc6983e9','head':PARENT,'worktree_empty':True},
  {'run_id':34009115037,'job_id':101421676716,'artifact_id':9981918738,'name':'yu32-runtime-34009115037-1','sha256':'b1fe09ff2a4e19618265fb6f882aa46659cfad0bb047868c97941d1ec664ccb1','head':PARENT,'worktree_empty':True}],
 'framework_issue_readback':[
  {'repository':'hvritual/yunka.io','number':149,'state':'closed','state_reason':'completed','closed_at':'2026-09-05T11:26:48Z','fix_adopted_by_yu33':False},
  {'repository':'hvritual/yunka.io','number':150,'state':'closed','state_reason':'completed','closed_at':'2026-09-05T11:47:26Z','fix_adopted_by_yu33':False},
  {'repository':'hvritual/yunka.io','number':151,'state':'closed','state_reason':'completed','closed_at':'2026-09-06T02:10:34Z','fix_adopted_by_yu33':False}],
 'unavailable_inputs':['YU-00F private FRAMEWORK-REQUIREMENTS.md','YU-00F private FRAMEWORK-PROBLEMS.md','YU-00F private evidence manifest'],
 'source_file_inventory':[{'path':p,'sha256':sha(p)} for p in key_paths],
 'historical_evidence_inventory':[{'path':p,'sha256':sha(p)} for p in historical],
 'current_document_inventory':[{'path':p,'sha256':sha(p)} for p in allowed if p!=U+'FINAL-MANIFEST.json'],
 'final_receipt_policy':{'file':'YU-33-FINAL-RECEIPT.json','location':'detached task delivery / PR receipt, not this commit','required_fields':['qualified_and_landed_sha','tree','fixed_parent','framework_sha','ci_runs','artifact_sha256','artifact_heads','worktree_drift','documentation_checks','main_readback'],'reason':'A commit cannot embed its own final SHA and later CI outcome. Input receipt does not certify final commit.'},
 'completion_gate':'Final exact-SHA full regression + runtime + documentation/integrity checks, actual artifact readback, then non-force main fast-forward. No deployment or automatic next task.'
}
Path(U+'FINAL-MANIFEST.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('YU-33 documentation generated; exact final SHA qualification still required.')
