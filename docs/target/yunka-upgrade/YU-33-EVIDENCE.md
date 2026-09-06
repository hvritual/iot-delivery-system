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
