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
