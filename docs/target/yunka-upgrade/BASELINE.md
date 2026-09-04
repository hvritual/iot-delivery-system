# YU-00 消费者源码与功能基线

> 文档类型：当前任务证据（EVIDENCE）
>
> 核验日期：2026-09-04
>
> 事实范围：仅 `hvritual/iot-delivery-system` 固定提交 `4cd1209f41575a67ab143b83e1df6501089085b8`
> 后续状态与实现不得从本文件推断；每个原子任务必须记录自己的固定 parent、代码差异和验证证据。

## 1. 基线结论

YU-00 已将本轮升级的正式消费者源基线固定为：

| 项目 | 固定值 | 核验方式 |
| --- | --- | --- |
| 远端 | `https://github.com/hvritual/iot-delivery-system.git` | 云端 `origin` |
| 正式源分支 | `main` | 仅用于定位本次固定提交；后续不跟随移动 HEAD |
| 正式源提交 | `4cd1209f41575a67ab143b83e1df6501089085b8` | `git rev-parse HEAD` |
| 源提交 parent | `971b67f43ca8b14a81c96a41277325c2da9613e6` | `git show -s --format=%P` |
| 源 tree | `0b2cf8a056c6db33bf22e94dba0ab21e5bfa8fb0` | `git rev-parse HEAD^{tree}` |
| 当前 Yunka gitlink | `9a51562aa7bcef42f6861bd91abd30aae13ed6ef` | `git ls-tree HEAD third_party/yunka` |
| Yunka URL | `https://github.com/hvritual/yunka.io.git` | `.gitmodules:1-3` |
| 目标 Yunka 提交 | `057ebcf88a87303eb633eb6e604d306f633dfac0` | 用户锁定；由 YU-00F 提供框架要求与问题卡 |
| 快照导入提交 | `959cbc4745a2aaff0889529ae014610569ae08dc` | 与正式源 tree 相同；不合并到 `main` |

`main@4cd1209f...` 与无父导入提交 `959cbc...` 的 tree 相同，但二者不是同一个历史基线。本轮以保留原始历史的 `main@4cd1209f...` 为唯一正式 parent。

## 2. 任务边界

本任务只建立消费者事实与后续原子任务台账：

- 允许修改：`docs/target/yunka-upgrade/` 下 YU-00 新文档。
- 禁止修改：`backend-yunka/`、`web/`、`backend/`、`third_party/yunka`、所有生成文件和既有历史基线文档。
- 不执行：合并 `main`、部署、删除远端分支、修改 Yunka 框架源码、导入本地未提交的 `web/next-env.d.ts`。
- 新增分析/迁移文档只保存在当前云端工作区并形成本地提交与 patch；在中控审核和单独公开发布授权前不 push。
- `docs/baseline/**` 保留其原有 as-of 语义；其中 Windows 路径、旧阶段状态和旧验证结果不是本轮云端事实。
- YU-00F 正在独立制作框架要求矩阵与框架问题卡。本任务不复制其主产出，只记录消费者证据和依赖关系。

## 3. 当前产品能力事实

### 3.1 业务与交付模型

真实 `backend-yunka` 已实现以下消费者能力，重构必须保持行为，不得用新示例替代：

- 五类驾驶舱板块：`backend-yunka/internal/delivery/model.go:5-13`。
- Project、Release、Sprint、Milestone：`model.go:47-128`；对应服务入口见 `service.go:58-223`。
- Epic、Task、Subtask、Defect 与 depends-on/blocks/related：`model.go:23-45`。
- WorkItem 排期、进度、规划、方案、ADR、证据、IoT 绑定、研发 TraceLink、评论、活动与复盘：`model.go:167-280`。
- IoT 设备、固件、客户、环境、灰度批次关联；PR、构建、测试、缺陷、发布关联：`model.go:174-215`。
- 相似事项、搜索、保存视图、成员周视图、加权项目进度和排期健康：`service.go:572-878`、`service.go:1042-1202`。
- expectedRevision CAS：评论 `service.go:1004-1040`、关卡 `service.go:1217-1287`、关闭 `service.go:1290-1333`、上下文 `service.go:1335-1390`、事项更新 `service.go:882-1002`。
- 研发执行人与生产验证人职责分离：`service.go:1234-1256`、`service.go:1308-1319`。

### 3.2 传输与合同

- protobuf typed DSL 声明 12 个 DeliveryService RPC：`backend-yunka/contracts/proto/iot_delivery.proto:14-173`。
- 提交中的 `operation-plans.json` 确有 12 个生成计划：`backend-yunka/contracts/generated/operation-plans.json:5-352`。
- HTTP 兼容路由覆盖 dashboard/items/projects/releases/sprints/milestones/views/member-week/notifications：`backend-yunka/internal/httpapi/handler.go:44-64`。
- MCP 注册 17 个工具，覆盖项目、事项、相似度、评论、关卡、关闭、规划对象、成员周视图、进度、排期健康和保存视图：`backend-yunka/internal/mcpserver/server.go:41-58`。
- HTTP、生成 gRPC 与 MCP 均以 `Operations`/Executor 为预期入口，但扩展操作仍使用手写 `operationplan.Plan`：`backend-yunka/internal/delivery/application/operations.go:16-24,488-550`。

### 3.3 持久化、事务、事件与投影

- SQLite repository 是当前本地主数据实现：`backend-yunka/internal/delivery/sqlite_repository.go:95-495`。
- bootstrap 在同一 SQLite 上装配业务 repository、audit、identity、Outbox、notification 与 config revision：`backend-yunka/internal/bootstrap/application.go:184-292`。
- 根执行器使用 SQLite transaction factory：`backend-yunka/internal/bootstrap/application.go:270-287`。
- Delivery mutation 在事务上下文中先 stage Outbox，再保存聚合：`backend-yunka/internal/delivery/service.go:1578-1590`、`outbox_stager.go:27-43`。
- Obsidian 是单向投影；本地通知收件箱及可选 Webhook、企业微信、SMTP adapter 已存在；截止提醒调度器由 runtime component 托管：`backend-yunka/internal/bootstrap/application.go:225-310,671-693`。
- 不可变配置修订 change/compare/rollback 为内部手写操作，change/rollback 同时写 audit 与 Outbox：`backend-yunka/internal/configapplication/operations.go:111-287`。

### 3.4 当前身份与授权

- identity schema 已有 organization、user、external identity、service account、role/team/binding 等持久模型入口：`backend-yunka/internal/identitycore/migration.go:20-66` 及后续迁移常量。
- production 人类链目前是 OIDC/BFF assertion → external identity binding → JWT Principal：`backend-yunka/internal/bffhttp/middleware.go:65-132`。
- development 使用环境 API key，并把凭据映射为固定 `local-development` tenant 与角色型伪用户 ID：`backend-yunka/internal/localauth/auth.go:23-40,139-165`。
- durable human GrantResolver 只接受 `AuthMethodJWT`：`backend-yunka/internal/humanauthz/resolver.go:44-65,144-149`。
- principal resolver 只将 JWT 分到 human grants、service-token 分到 service grants：`backend-yunka/internal/principalauthz/resolver.go:30-41`。
- OperationGuard 对人类同样要求 JWT，并回读 active user/role/project/object scope：`backend-yunka/internal/deliveryauthz/guard.go:128-227,269-307`。
- 浏览器 BFF session 当前仅为进程内 Map，携带 `VerifiedLogin`：`web/lib/server/session.ts:15-28,33-114`。

## 4. 已证实的消费者差距

以下均为源代码静态事实，不等同于运行时结论，也不自动归因 Yunka 框架：

| ID | 已观察事实 | 目标差距 | 证据 |
| --- | --- | --- | --- |
| CG-01 | `go.mod` 仍依赖 `yunka.io/framework|gateway|pkg` 并用本地 replace 指向 gitlink | 升级到目标提交的公开模块身份 `github.com/hvritual/yunka.io/...`，`app` 例外仍属框架自身 | `backend-yunka/go.mod:13-15,48,51-55` |
| CG-02 | 12 个生成 RPC 之外存在 13 个独立扩展 Operation ID | 每个真实操作进入 canonical protobuf typed DSL、生成计划、权限字典和跨 transport 一致链 | `operations.go:206-241,381-485` |
| CG-03 | Dashboard/List 在 `service != nil` 时对生成 Operation ID 重新构造扩展 plan，且权限改为 `delivery.items.read` | 同一 Operation ID 只使用生成计划与规范权限 | `operations.go:46-95`；对照 proto `iot_delivery.proto:17-41` |
| CG-04 | `extensionPlan` 仅声明 API key/JWT，13 个扩展没有 canonical service auth，权限使用 legacy alias | typed DSL 统一 auth、permission、scope、transaction 和 transport 语义 | `operations.go:536-550`; `permission-dictionary.v1.json:77-86` |
| CG-05 | development 授权由环境 API key 的角色表提供，不是持久成员授权 | 真实本地账号 → User/Tenant Principal → SQLite GrantResolver/OperationGuard | `localauth/auth.go:67-100,170-248` |
| CG-06 | BFF assertion 人类身份复制 API-key channel 的 legacy roles | 角色授权只从持久 RoleBinding/Grant 回读，channel credential 不授予人类角色 | `bffhttp/middleware.go:74-88,113-120` |
| CG-07 | 人类授权分支由 `AuthMethodJWT` 判型 | 本地密码认证须签发并验证受支持的内部短期 JWT，再进入同一 durable human 分支；不得注入未验证 JWT | `humanauthz/resolver.go:44-49,144-149`; `principalauthz/resolver.go:34-40` |
| CG-08 | OperationGuard 的 durable 人类分支只接受 JWT | 本地会话需要实际验证并映射到 canonical UserID/TenantID，停用/撤权时回读生效 | `deliveryauthz/guard.go:196-227` |
| CG-09 | 浏览器 session 是进程内 OIDC `VerifiedLogin`，写请求 Origin 来自 OIDC redirect 配置 | 登录路径不依赖 OIDC；服务端不透明 session、CSRF、local origin 独立配置 | `web/lib/server/session.ts:3,15-28,33-114`; `web/app/api/[...path]/route.ts:13-31` |
| CG-10 | 浏览器 API client 未发送 `X-CSRF-Token` | 所有非安全方法必须携带当前 session CSRF token | `web/src/api.js:1-9,24-128`; `web/lib/server/session-guard.ts:9-17` |
| CG-11 | runtime forwarder 在存在 login 时强制 local API key，并继续上送 API key | 本地成员登录不依赖共享 channel admin key；BFF 仅签发验证后的成员 assertion/JWT | `web/lib/server/runtime-forwarder.ts:4-13`; `web/lib/server/runtime-proxy.ts:48-73` |
| CG-12 | 前端路由只有 OIDC login/callback/logout/session，没有业务内成员登录、当前成员、改密和管理入口 | 新增明确区分于 OIDC 的本地成员入口与管理 UI | `web/app/auth/**`; `web/app/page.tsx` |
| CG-13 | 配置修订已有内部能力但没有外部 transport | 保持内部能力，不扩张为新外部 UI | `configapplication/operations.go:111-287`; `permission-dictionary.v1.json:63-65` |

## 5. 手写计划覆盖结论

本轮不能只迁移 12 个生成 RPC：

- 12 个 protobuf 生成 OperationPlan；
- 13 个不同的 Delivery 扩展 Operation ID；
- 2 个生成 Operation ID 的手写 plan 替代路径（dashboard/list）；
- 3 个 config revision 手写 OperationPlan。

完整逐项清单在 `CAPABILITY-MAP.md`。任何后续任务删除 `extensionPlan` 前，必须先证明相应 canonical typed plan 已生成、字典已覆盖、REST/gRPC/MCP 行为一致且拒绝路径无业务/Outbox 变更。

## 6. 云端基线验证

| 验证 | 结果 | 解释 |
| --- | --- | --- |
| `git rev-parse HEAD` / tree / gitlink | PASS | 与用户提供的独立 API 核验一致 |
| `npm ci` | PASS | 安装 463 packages；未修改受跟踪源码 |
| `npm test` | PASS | 16 files / 45 tests |
| `npm run typecheck` | PASS | TypeScript 无错误 |
| `npm run build` | PASS | Next 16.2.5 production build；8 个路由 |
| `go test -count=1 ./...` | NOT RUN | 云端镜像没有 `go`；环境失败不记作 RED 或 PASS |
| runtime/browser E2E | NOT RUN | YU-00 禁止业务实现和运行时副作用；后续原子任务执行 |

命令、时间与环境详见 `RUN-MANIFEST.json`。

## 7. YU-00 完成定义

YU-00 仅在以下条件同时满足时完成：

1. 正式源 commit/tree/gitlink 固定且可复核；
2. 12 个生成 RPC、全部 Delivery 扩展计划和 config revision 手写计划均有清单；
3. 十二阶段全部拆为 30–90 分钟可独立验证的原子任务；
4. 当前能力、目标差距、依赖、可改/禁改范围和验收命令明确；
5. 只产生文档提交，不修改消费者实现、生成物或框架；
6. 提交后由中控创建下一独立云端任务，本任务不越界继续实现。
