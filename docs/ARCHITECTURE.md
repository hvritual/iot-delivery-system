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
