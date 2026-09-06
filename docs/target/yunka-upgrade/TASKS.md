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
| **YU-01** | 2 | 60–90m | 更新 gitlink到 `057ebcf`，把消费者 `yunka.io/framework|gateway|pkg` 迁至公开模块身份；不改行为 | YU-00 + YU-00F | 编译前依赖图、`go mod tidy` 零意外扩散、仅机械 import/module delta |
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

## YU-30 执行认证与收口

已核验源码 `2f533dc233584c3318b9788f7016dcfb131b99b7` 的 GitHub Actions run `33971774693`：canonical-full / go-diagnostics / web-diagnostics / browser-e2e 四项均成功。完整回执见 `YU-30-CI-RECEIPT.json`，边界见 `YU-30-EVIDENCE.md`。本收口提交必须再取得精确 SHA 的 CI 全绿并 non-force 合并到 main，才能完成派发切换。

注意：audit 为零新增 proven debt，仍保留 `AUDIT-AUTH-001` 的一项既有消费者债务；ChangeSet 为当前 canonical operations 的正/反/恢复验证，不是历史全量变更认证。YU-30 不等于 YU-32 安全/架构终审。

## YU-31 运行时认证与收口

固定 parent：`0501bd4b2295c624b817e28a94eb1f62b08b0d4c`。新增真实进程 HTTP/gRPC/stdio MCP、同一 SQLite 真实身份授权、EOF/SIGTERM/SIGINT、失败启动、重启持久化与进程回收认证。真实复现并最小修复开发模式无条件依赖旧 API key 的消费者组合问题；不是 Yunka 缺陷。源码 `9d552633e2b50110d3a74b4d1d3c0b02f4099bb4` 已通过运行时测试 `34002432487`，详细链路和回执见 `YU-31-EVIDENCE.md` / `YU-31-CI-RECEIPT.json`。

本收口还收紧退出原因、shutdown error log 和 SQLite checkpoint busy 结果检查；最终提交必须再通过精确 SHA 的 YU-31 runtime-smoke 与全部四项 YU-30 回归，再 non-force 合并 main 才完成本轮。测试源码、文档状态或旧 SHA 的 PASS 均不能代替这一步。`AUDIT-AUTH-001` 既有消费者债务仍留给 YU-32。

## YU-32 独立安全/架构审查与收口

固定 parent：`20520f69d7eaf3c27c0fb3e9d79f03b4ecb059bd`。独立审查按“候选 → 反证/复现 → 归属 → 修复/保留风险”执行；YU-00F 私有框架分析文档未出现在公开消费者仓库或当前 verified checkout，因此明确记为输入限制，没有从记忆重建。

确认并修复一项既有消费者架构债务 `AUDIT-AUTH-001`：`internal/delivery/application/adapter.go` 直接依赖 Yunka `gateway/authz`。SOD 领域 sentinel 保持不变，授权错误归一化迁移到 `internal/deliveryauthz` 边界，并新增 Application import 禁止回归。候选源码 `113a83f50f856b77a38a8db008e6fef7cffe2bcb` 已通过 YU-32 Actions run `34004666384` 的 `full-regression` 与 `runtime-smoke`；audit 回读为 `existing=[]`、`new=[]`，原 `AUDIT-AUTH-001` 精确进入 `fixed`。完整回执见 `YU-32-CI-RECEIPT.json` / `YU-32-EVIDENCE.md`。

审查反证了当前 JWT role snapshot 授权、邮箱/显示名并号、transport 直接持久化、跨租户 RoleBinding、撤权缓存、跨 context CSRF 和已覆盖 Linux runtime 关闭泄漏等候选问题。保留一项消费者安全加固风险：local password 登录尚无明确在线猜测限速/锁定和最小密码长度产品合同；YU-32 不自行发明代理/IP/多实例策略，YU-33 必须在最终运行说明与遗留风险中准确记录。

本收口提交是文档/回执派发变更，必须在其自身精确 SHA 上再次通过 `.github/workflows/yu32-independent-review.yml` 的 `full-regression` 与 `runtime-smoke`、且 artifact 证明零漂移后，才能 non-force 合并 `main`。旧 SHA 的绿色结果不能替代最终提交门禁。

## YU-32H 密码安全遗留项修复

用户在 YU-32 合并后明确要求修复审查发现的问题。固定 parent 为 `5e57424b034ae8da0ac27d6fb920b457005ea253`；本补充任务不执行 YU-33、不部署、不改框架。

此前保留的本地密码安全合同缺口已落实：统一 15 字符新密码策略；账号/来源双预算与临时冷却；同一 SQLite 持久化原子计数；登录与本人改密共用预算；拒绝信任代理来源头；429/Retry-After/BFF/UI 对齐。管理员重置与本人改密的原有 CAS 错误及旧会话失效语义保持不变。

修复源码 `0a83eabea5766a0c4fc64fc84c914140036fd3df` 的 Actions run `34008694043` 已通过完整 YU-30 回归和 YU-31 runtime smoke，并具有真实旧版本 RED→当前 GREEN。回执见 `YU-32H-CI-RECEIPT.json`，修复过程及边界见 `YU-32-EVIDENCE.md` 的 YU-32H 节；具体运行策略见 `backend-yunka/README.md`。

本次文档收口仍须其自身精确 SHA 的两项 YU-32 CI 全绿且 artifact 零漂移后才能合并。YU-33 后续输入须采用合并了 YU-32H 的 main，不得沿用“尚无限流/最小长度”的历史描述，也不得删去存量密码轮换、单 SQLite/代理预算及生产边界限制。

## YU-33 最终文档与清单收口

固定 parent：`1da771dac46c1b10c2ea54a0fb4559316c20179b`。本轮仅变更 README、架构/迁移/来源说明、当前任务台账、最终 manifest、已关闭问题与遗留风险清单；旧逐任务证据和基线 manifest 保留 as-of 语义。

已按源码纠正 25 个生成 Delivery plans、本地成员认证入口、可选 legacy API Key、单 SQLite/MCP 已验证范围及密码安全合同。框架 #149/#150/#151 当前为 closed，但本项目的固定 gitlink 与适配未改变；YU-00F 实际私有输入不可用的限制继续保留。

最终文件：[FINAL-MANIFEST.json](FINAL-MANIFEST.json)、[RESIDUAL-RISKS.md](RESIDUAL-RISKS.md)、[YU-33-EVIDENCE.md](YU-33-EVIDENCE.md)。最终提交号、CI/run/artifact、文档检查和 main 回读由独立 `YU-33-FINAL-RECEIPT.json` 绑定，避免在待验证提交里把未来 CI 结果写成已完成事实。

结束条件：承载这些文档的精确提交通过完整 YU-30 回归、YU-31 runtime smoke、YU-32H RED/GREEN 与文档检查，artifact head 与哈希一致、零工作区漂移，再同步任务分支并 non-force 合并 main。仅文档存在或父提交 PASS 不满足结束条件。

## 调度边界

YU-33 为本次 Upgrade 的最后一个原子任务；完成上述门禁与 main 回读后收口并停止。没有自动派发 YU-34、生产切换或其他产品功能。后续工作由 RESIDUAL-RISKS 的责任角色与授权流程单独确定。
