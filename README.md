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
