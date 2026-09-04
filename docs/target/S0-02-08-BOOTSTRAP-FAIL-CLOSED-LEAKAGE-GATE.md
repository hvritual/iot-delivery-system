# S0-02-08 — Bootstrap fail-closed and leakage gate

## 已验证

- `IOT_DELIVERY_RUNTIME_ENVIRONMENT` 只能是 `development` 或 `production`；缺失和未知值均拒绝。
- `IOT_DELIVERY_BOOTSTRAP_MODE` 默认 `disabled`，仅 `development + example` 可以创建样例；重启通过既有 Operations/Yunka Executor/事务 Outbox 链保持幂等。
- production 在任何 SQLite、Vault、HTTP 或 gRPC 监听副作用前拒绝 example bootstrap、所有 legacy local API-key 环境变量、不安全 service credential development 开关、缺失 BFF pair 和无效 BFF assertion key。
- 完整 production 配置只装配 BFF-only HTTP：已签名 assertion 的 method/path/body/timestamp/nonce/trace 校验保持不变；无 assertion 的 local API-key fallback 与 gRPC local fallback 关闭。BFF-only principal 没有临时角色，S0-03 绑定前业务操作默认拒绝；明文 gRPC service credential 继续按既有默认拒绝。
- `yunka-bootstrap` 与 `iot-delivery-mcp` 共用 `bootstrap.StartupPolicyFromEnvironment`；MCP 是明确的 development-only 入口。配置错误使用固定类别消息，不回显环境变量值。哨兵密钥回归测试覆盖错误和其日志格式化输入不泄漏。

## 扫描边界

门禁测试 `backend-yunka/credential_leak_gate_test.go` 通过 `git -C .. ls-files -s -z` 取得全部受版本控制的第一方仓库清单，并扫描 root README/配置、`backend/`、`backend-yunka/`、`web/` 和 `docs/`。范围自检要求 root README、旧 backend 源码、Yunka 后端源码、web session 源码和 BFF 文档均实际进入扫描。唯一的路径级排除是 mode `160000` 的 `third_party/yunka` gitlink；含 NUL 的二进制内容按显式二进制规则跳过。未跟踪文件和 Git 历史不属于扫描输入。

## 规则

测试拒绝 AWS access key、GitHub/GitLab access token、Slack token，以及赋值为 20 个以上可见字符的 API key/access token/secret/password 字面量。每个规则用 `FindAll` 遍历文件中的全部匹配并逐项应用精确 allowlist；任一未允许匹配立即以文件和规则失败，绝不回显匹配正文。规则刻意偏向高置信度，避免将环境变量名称、协议头名称和文档描述误报为凭据。

## allowlist

allowlist 只有一个精确条目：`credential_leak_gate_test.go` 中用于证明 GitHub-token 规则生效的明显虚构测试字符串。allowlist 同时精确匹配该文件与该字符串；不按目录、扩展名、变量名或宽泛关键词忽略任何内容。S0-02-08 的 `S0_02_08_*_SENTINEL_DO_NOT_LOG` 虚构哨兵仅出现在测试中，且不满足高置信度凭据形状。

## 未覆盖

这是静态、模式型门禁，不能证明运行时从真实 Vault、生产数据库、CI secret store、OIDC provider 或第三方日志系统中不存在泄漏；本任务没有连接这些系统。它也不替代人工凭据轮换、历史提交扫描或部署期 secret manager 策略。Yunka runtime 的 TLS server wiring 未在本切片接入，因此 production service credential 继续默认拒绝明文连接，不以 insecure flag 绕过。真实灾备初始化必须走后续受审计的专用恢复程序，不得借由 example seed 执行。
