# 当前来源与历史复用记录

## YU-33 当前版本

当前消费者输入为 `1da771dac46c1b10c2ea54a0fb4559316c20179b`，子模块固定为 `057ebcf88a87303eb633eb6e604d306f633dfac0`。已采用 generated operation plans、generated Assembly、typed capabilities、身份授权执行链与事务 Outbox；未采用 Provider-managed MySQL 或生产部署治理。

精确版本、文件哈希、工具链与证据索引由 [FINAL-MANIFEST.json](target/yunka-upgrade/FINAL-MANIFEST.json) 管理；最终收口提交由独立 YU-33-FINAL-RECEIPT.json 绑定。下方旧 pin 和未采用清单仅用于历史来源追溯，**不是当前能力清单**。

本轮不重新作许可证法律结论，也不把历史来源审查视为二进制或依赖再分发授权。公开消费者文档不包含 YU-00F 私有框架分析文件；对应输入限制保留在遗留项中。

## 历史记录（保留原文，旧基线语义）

### 历史来源与复用边界

| 来源 | 固定版本 | 允许和采用的范围 | 明确不采用的范围 |
| --- | --- | --- | --- |
| [hvritual/yunka.io](https://github.com/hvritual/yunka.io) | `9a51562aa7bcef42f6861bd91abd30aae13ed6ef` | `yunka init` 项目档案、`runtimehost → kernel → core.App` 运行时、HTTP/gRPC 生命周期/健康/诊断/关闭、以及受清单管理的 Protobuf Go/gRPC 与合同生成检查；本仓库以 Git 子模块固定此版本。 | 未采用 Yunka DSL 的 operation plan/生成应用装配、网关代码生成、声明式鉴权链路、平台化 MySQL Provider、事件外送与生产部署治理。 |
| [hvritual/goclaw-team-runtime](https://github.com/hvritual/goclaw-team-runtime) | `3a75c7376d73e41f33e2b94eb3bb1ca4c30219fd` | 交付工作项的状态/证据闭环思想、Multica 的桌面信息架构、侧栏/内容区布局和测试优先的验证方式。前端以本仓库独立的 Next App Router、Tailwind、shadcn `base-nova` 实现。 | 不引入其 agents、runners、队列、单写入文件存储、Obsidian 插件、组件源码、样式源码或未验证的团队控制台运行时。 |

### 历史许可与发布门槛

GoClaw 源仓库标示为 MIT；本项目当前没有复制其源文件，仍保留上述来源记录以便审计。Yunka 在固定提交处未发现仓库许可证文件，因此本项目仅按用户授权作内部实现与评估使用。

若要将本仓库公开发布、分发二进制或把 Yunka 代码纳入外部交付物，必须先获得 Yunka 的明确许可证或权利确认；在此之前不得把它标记为可公开再分发的依赖。
