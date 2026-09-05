# Yunka 升级与业务内成员账号密码：原子任务台账

> 正式源码基线（as-of，仅用于追溯导入源树）：`4cd1209f41575a67ab143b83e1df6501089085b8`
>
> 目标 Yunka：`057ebcf88a87303eb633eb6e604d306f633dfac0`
> 调度原则：一个原子任务一个云端侧边栏会话；每项预计 30–90 分钟、独立提交、独立验证。中控固定每个任务的精确 parent，不跟随移动 HEAD。

## 全局任务合同

每个任务必须记录：`task_id`、精确 parent、允许/禁止路径、预期行为、真实 RED 证据、GREEN 证据、回归命令、生成漂移、提交 SHA、未解决问题。不得把环境缺工具当 RED，不得用 markdown 测试、mock 或 development `localauth` 证明真实身份授权。

除文档专用任务外，默认禁止修改 `backend/` 与 `third_party/yunka` 内框架源码；生成文件只能通过目标 Yunka canonical generator 更新。框架问题只能在消费者公开扩展点绕过并验证，不能降低验收标准。

## 十二阶段与原子任务

| ID | 阶段 | 预计 | 目标与范围 | 依赖 | 原子验收 |
| --- | ---: | ---: | --- | --- | --- |
| **YU-00** | 1 | 60–90m | 固定源 commit/tree/gitlink；完整消费者能力、手写计划与任务台账 | `main@4cd1209f` | 五份文档；文档-only commit；前端基线验证 |
| **YU-00F** | 1 | 独立任务 | 固定框架要求矩阵、问题候选与 evidence manifest；本任务不重复 | Yunka `057ebcf` | 由会话 `6a9a8f55-fa44-83eb-b298-bc2e410761e0` 独立交付 |
| **YU-01** | 2 | 60–90m | 更新 gitlink 到 `057ebcf`，把消费者 `yunka.io/framework|gateway|pkg` 迁至公开模块身份；不改行为 | YU-00 + YU-00F | 编译前依赖图、`go mod tidy` 零意外扩散、仅机械 import/module delta |
| **YU-02** | 2 | 45–75m | 对齐 `.yunka/project.json`、provider/protobuf/dev manifest 与目标 CLI；保留 app/runtime 入口 | YU-01 | `yunka context --json`、ownership/audit 基线可读；不生成业务新语义 |
| **YU-03** | 2 | 60–90m | 用目标 generator 重建 YU-03 当时源码基线的 12 RPC 全部派生物，禁止手改 | YU-02 | 两次 generate 后零 drift；`yunka check --full`；12 plans 等价 |
| **YU-04** | 3 | 60–90m | 以公开 typed capability 接入 SQLite/transaction factory；必要时手写 descriptor wrapper | YU-03 | 无 service locator；root UoW 单一；rollback 集成测试 |
| **YU-05** | 3 | 60–90m | Outbox、notification、projection 作为 App-owned typed capability/lifecycle 接入 | YU-04 | start/health/reverse shutdown；无 request state 泄漏 |
| **YU-06** | 3 | 45–75m | 重组 generated Assembly/runtimehost/HTTP/gRPC 装配并关闭旧 module-identity 路径 | YU-04/05 | health/diagnostics/runtime closure；现有 HTTP/gRPC smoke |
| **YU-07** | 4 | 60–90m | 为 Project 及 ListProjects 建 canonical protobuf typed contracts | YU-03 | plan-first ChangeSet；create/list plans、权限、scope、REST/MCP 映射一致 |
| **YU-08** | 4 | 60–90m | 为 Release/Sprint/Milestone list 操作合同化并消除 YU-07 后剩余的 3 个 planning extension plans | YU-07 | 生成可重复；规划对象 create/list 行为回归；跨项目拒绝 |
| **YU-09** | 5 | 60–90m | 合同化 item get/search/similarity，消除 3 个独立扩展 plan | YU-08 | exact/similar/filter 行为；project/object scope；REST/MCP 一致 |
| **YU-10** | 5 | 60–90m | 修复 Dashboard/List 同 ID 双 plan；合同化组合 Update + UpdateContext requires_operations | YU-09 | canonical permission 不再用 alias；同一根 UoW；失败整体回滚 |
| **YU-11** | 5 | 60–90m | 评论、上下文/ADR、关卡、关闭的 CAS/证据/职责分离跨 transport 回归 | YU-10 | 冲突分类一致；拒绝业务/Outbox 零变更；两身份职责分离 |
| **YU-12** | 6 | 60–90m | 合同化 saved view save/list、member week | YU-10 | 3 个 plans 生成；视图 owner 改为 canonical UserID，不以显示名授权 |
| **YU-13** | 6 | 60–90m | 合同化 project progress/schedule 与 notifications list | YU-12 | 3 个 plans 生成；项目范围过滤；HTTP/MCP 等价 |
| **YU-14** | 6 | 45–75m | 驾驶舱、IoT/TraceLink、提醒与 Obsidian 投影完整回归 | YU-13 | UI/DTO/SQLite/投影字段零丢失；提醒幂等 |
| **YU-15** | 7 | 60–90m | 验证所有写路径 root UoW + transactional Outbox；清理旁路 | YU-14 | 成功原子提交；应用失败/审计失败/Outbox 失败回滚；拒绝零业务事件 |
| **YU-16** | 7 | 45–75m | 审计覆盖 accepted/denied/failure/rollback 并脱敏 | YU-15 | 真实 Principal；密码/token/session/CSRF 不进日志或 audit payload |
| **YU-17** | 7 | 60–90m | 保持 config revision change/compare/rollback 内部能力并对齐目标 Executor | YU-15/16 | 3 手写 plans 有明确内部 canonical 注册；CAS、audit、Outbox 回归；无新 UI |
| **YU-18** | 8 | 60–90m | 新增独立 local credential schema/repository，关联既有 User；采用当期 OWASP 推荐专用慢哈希 | YU-06/16 | migration 可重复；明文不落库/日志；哈希参数和升级策略测试 |
| **YU-19** | 8 | 60–90m | 一次性管理员初始化，永久关闭匿名再初始化 | YU-18 | 并发/重复最多一个管理员；停用后不能重新开放；事务与 audit |
| **YU-20** | 8 | 60–90m | 管理员创建、停用、重置成员凭据的应用操作与 CAS | YU-19 | 独立账号凭据；Owner/邮箱不作身份合并；失败无业务/Outbox 变更 |
| **YU-21** | 9 | 60–90m | 登录验证密码，创建服务端不透明 session，签发并验证内部短期 JWT | YU-18/20 | 不伪造 OIDC issuer/subject；未验证 JWT 不产生 Principal；真实 UserID/TenantID |
| **YU-22** | 9 | 45–75m | 当前成员、退出、本人改密与 session version/revocation | YU-21 | 改密/退出令旧 session 及时失效；并发 CAS；安全 audit |
| **YU-23** | 9 | 60–90m | 将本地成员认证接入同一 HTTP/gRPC/MCP durable human auth chain | YU-21/22 | SQLite GrantResolver + OperationGuard；不再共享 `local-admin`；三 transport 同判定 |
| **YU-24** | 10 | 60–90m | 管理项目角色分配/撤销，复用 RoleBinding 与项目对象 scope | YU-20/23 | 项目级最小权限；跨租户拒绝；撤权旧 session 下一请求即失效 |
| **YU-25** | 10 | 45–75m | 停用、密码重置、角色撤权的集中 session 有效性检查 | YU-24 | 两真实账号；停用/重置/撤权旧会话及时失效；无缓存越权 |
| **YU-26** | 11 | 60–90m | 新增本地 auth BFF routes：login/current/logout/change-password/admin members | YU-22/25 | 登录无需 OIDC 配置；OIDC 若保留则显式分流；cookie/错误/no-store 正确 |
| **YU-27** | 11 | 45–75m | 前端 API client 自动发送 CSRF；Origin 与 OIDC 解耦 | YU-26 | 所有 unsafe method 有 token；缺失/错误 token 为 403；safe method 不误拒 |
| **YU-28** | 11 | 60–90m | 登录、当前成员、退出、改密、成员管理和项目角色 UI | YU-26/27 | 无 OIDC 配置可用；权限控制界面但不以 UI 授权；可访问性/错误状态测试 |
| **YU-29** | 11 | 60–90m | 两个真实账号、两个独立浏览器 context 的 E2E | YU-28 | 独立权限；停用/改密/重置/撤权及时生效；CSRF；职责分离 |
| **YU-30** | 12 | 60–90m | 全量 Go/前端/生成/ownership/audit/ChangeSet 回归 | YU-01…29 | double generate、full check、Go tests/race/vet、前端 test/typecheck/build，零 drift |
| **YU-31** | 12 | 45–75m | 真实 runtime health/diagnostics/closure 与 REST/gRPC/MCP smoke | YU-30 | 同一 SQLite 身份授权链；shutdown 完整；无僵尸资源 |
| **YU-32** | 12 | 45–75m | 独立安全/架构审查与问题闭环 | YU-30/31 + YU-00F | 候选→复现/反证→证实；consumer debt 不归因框架；绕过不称修复 |
| **YU-33** | 12 | 45–75m | 更新运行说明、最终 manifest、提交与遗留问题清单 | YU-32 | 文档与 executable truth 一致；提交 parent/evidence 完整；同步任务分支并合并 `main`；不部署 |

## 下一原子任务派发说明

**建议下一侧边栏任务：YU-24「管理项目角色分配/撤销，复用 durable RoleBinding 与项目 scope」**。

- 固定 parent：使用 YU-23 交付后同步到 `main` 的精确提交 SHA；不得跟随后续移动 HEAD。
- 输入：YU-20 成员生命周期与 `system-administrator` 管理边界、YU-23 verified local-member durable authorization chain、现有 `roles` / `role_permission_grants` / `role_bindings` / project ownership 与 scope model。
- 允许：消费者侧项目角色分配/撤销 application operations、必要的 RoleBinding revision/CAS 或等价并发控制、目标 User/Project/tenant durable 校验、仅使用现有 project-capable role/permission contract、transactional audit/Outbox、跨 transport application-level authorization regression、YU-24 证据文档。
- 禁止：修改 Yunka 源码、手改 generated 文件、以邮箱/显示名作为成员身份、创建跨租户或跨 project 的 RoleBinding、扩展为任意角色/权限编辑器、实现管理员重置/User disable/角色撤权后的 session 集中撤销或 session-version 策略（YU-25）、新增 BFF auth/role routes（YU-26）或 UI。
- RED：必须由真实 durable 路径证明管理员可把项目角色绑定到其他 tenant 的 User/Project、角色 binding scope 与 role contract 不匹配、重复/并发 assign/revoke 可绕过 CAS 或产生多条 active binding、普通成员可提升自己或他人权限、撤销 RoleBinding 后 YU-23 的下一次受保护请求仍继续获得该 grant，或 audit/Outbox 失败后 RoleBinding 仍提交；环境缺工具不算 RED。
- GREEN：只有 durable `system-administrator` 可管理同 tenant、真实存在且 active 的 User 与 Project 的项目角色；RoleBinding 必须使用 canonical UserID + project scope，且 role binding scope/permission allowed scopes 匹配；assign/revoke 有明确 CAS/幂等冲突语义并原子提交 audit/Outbox；跨租户/越权/陈旧 revision 拒绝且零业务事件；撤销后无需等待 token 过期，YU-23 durable GrantResolver/OperationGuard 在下一请求立即不再授予对应项目权限，但 session 本身是否集中失效仍留给 YU-25。
- 框架问题：如复现 Yunka 缺陷，只创建/更新框架 Issue，不修改框架源码；消费者 RoleBinding 管理不得表述为框架修复。
- 结束条件：独立提交、审查、同步任务分支并合并 `main` 后停止；不部署、不开始 YU-25。
