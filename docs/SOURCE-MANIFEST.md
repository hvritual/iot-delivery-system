# 来源与复用边界

| 来源 | 固定版本 | 允许和采用的范围 | 明确不采用的范围 |
| --- | --- | --- | --- |
| [hvritual/yunka.io](https://github.com/hvritual/yunka.io) | `9a51562aa7bcef42f6861bd91abd30aae13ed6ef` | `yunka init` 项目档案、`runtimehost → kernel → core.App` 运行时、HTTP/gRPC 生命周期/健康/诊断/关闭、以及受清单管理的 Protobuf Go/gRPC 与合同生成检查；本仓库以 Git 子模块固定此版本。 | 未采用 Yunka DSL 的 operation plan/生成应用装配、网关代码生成、声明式鉴权链路、平台化 MySQL Provider、事件外送与生产部署治理。 |
| [hvritual/goclaw-team-runtime](https://github.com/hvritual/goclaw-team-runtime) | `3a75c7376d73e41f33e2b94eb3bb1ca4c30219fd` | 交付工作项的状态/证据闭环思想、Multica 的桌面信息架构、侧栏/内容区布局和测试优先的验证方式。前端以本仓库独立的 Next App Router、Tailwind、shadcn `base-nova` 实现。 | 不引入其 agents、runners、队列、单写入文件存储、Obsidian 插件、组件源码、样式源码或未验证的团队控制台运行时。 |

## 许可与发布门槛

GoClaw 源仓库标示为 MIT；本项目当前没有复制其源文件，仍保留上述来源记录以便审计。Yunka 在固定提交处未发现仓库许可证文件，因此本项目仅按用户授权作内部实现与评估使用。

若要将本仓库公开发布、分发二进制或把 Yunka 代码纳入外部交付物，必须先获得 Yunka 的明确许可证或权利确认；在此之前不得把它标记为可公开再分发的依赖。
