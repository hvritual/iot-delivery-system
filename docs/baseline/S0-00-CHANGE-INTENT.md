# S0-00 Change Intent — 源码基线与阶段 0 变更包

## 当前事实（2026-09-03）

- 仓库已初始化于 `F:\code\iot-delivery-system`，但尚无可解析的 `HEAD`；分支为 `codex/iot-delivery-system-mvp`。
- `backend-yunka/` 是本阶段唯一的目标实现。`backend/` 保留为只读迁移和回归行为证据，不是下一阶段的实现目标。
- `third_party/yunka` 是 Git 子模块，固定为 `9a51562aa7bcef42f6861bd91abd30aae13ed6ef`；S0 不更新它。
- 现有本地状态路径为 `backend/data/`、`backend-yunka/data/`、`backend-yunka/runtime-vault/`、`backend-yunka/.tools/`、`backend-yunka/.yunka/cache/`、`web/node_modules/`、`web/.next/` 和 `web/dist/`，均不纳入基线提交。`.yunka/cache/fast-feedback.json` 是 `sha256-tree-v1` fast-feedback 缓存，且其 engine 标记为未验证；同级的项目配置文件不属于该排除范围。

## 目标设计

建立一个可恢复、可审计的首个 Git 基线提交，包含源码、已知派生合同、运行说明和阶段 0 治理资产；后续工作以此提交为唯一源码锚点。

## 非目标与硬边界

- 不启动 S0-01 或任何后续工作包；`max_active_stories=1`。
- 不推送、不部署、不安装依赖、不运行常驻服务。
- 不读取、写入、迁移或删除正式数据库与正式 Obsidian Vault；不删除旧后端、备份或任何忽略内容。
- 不更新 Yunka 子模块；不把共享角色 API Key 描述为具名身份。
- 不声明未验证的生产运行、外部通知投递、许可证或可公开再分发权利。

## 阶段配置

| 配置 | 固定值 |
| --- | --- |
| 变更方式 | `change-spec` |
| 阶段完成语义 | `release-complete`（仅阶段 0） |
| 确认来源 | `human-confirmed` |
| 工程方式 | `strict XP` |
| 最大并发故事 | `1` |

## 验收门

1. 目标、旧/新后端、数据库和 Vault 的写入边界已记录。
2. 分支、暂存状态、未跟踪文件、子模块版本和运行入口已盘点。
3. 敏感文件、数据库、Vault、日志、缓存、构建物和生成物已扫描；未解释风险为阻塞。
4. 来源、许可状态、允许复用范围和仅作行为证据的内容已清单化。
5. `SOURCE-MANIFEST.json`、`LICENSE-PROVENANCE.json`、`RUN-MANIFEST.json` 与本文件可被解析或人工复核。
6. 安全的既有测试通过，且提交前仅暂存已盘点的明确路径。

## 回滚原则

首个提交只创建本地 Git 历史，不触及运行数据。恢复源码时可在同一工作树检出该提交；任何数据或 Vault 恢复必须使用其各自既有备份和独立授权，不能由 Git 基线替代。

## 未验证假设

- 当前环境中的 Go 与 Node 工具链足以重现既有测试；测试结果必须以本次运行输出为准。
- 本项目自有源码的外部发布许可尚未声明；本提交不构成授权或发布。
