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
