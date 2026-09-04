# YU-03 当前 12 RPC 派生物语义等价证据

> 文档类型：任务证据（EVIDENCE）
> 核验时间：2026-09-04T11:12:14Z
> 固定 parent：`4b2840a93f379242c2b05e04613f59b09590f52c`
> 工作分支：`codex/yu-03-generated-equivalence`
> Yunka gitlink / materialized HEAD：`057ebcf88a87303eb633eb6e604d306f633dfac0`

## 结论与范围

固定 target generator 已对当前 12 个 protobuf RPC 执行两轮 canonical `generate` 与 full `check`。两轮均成功，全部 generator-owned 内容与 YU-02 parent 保持逐字节一致，因此没有可提交的 generated delta，也没有为了制造变化而手改派生文件。

本任务在 AX2 判定为 `developer-code/editable` 的 `backend-yunka/internal/delivery/generated_rpc_equivalence_test.go` 新增非生成回归测试。它用 protobuf generated descriptor 验证 12 个 RPC/request/response 与 plan RPC binding 的双射，并用固定 framework 的 `operationplan.CanonicalJSON` 验证 `operation-plans.json` 与 12 个生成 Go policy plan 的全字段规范化等价。protobuf operation options 与生成 RPC/REST/application/assembly 源码的权威 source-to-derived 校验仍由 `make yunka-check` 负责；该测试不替代 canonical generator/check。

未修改：`contracts/proto/**`、权限字典、任何手写业务或运行时装配、`third_party/yunka/**`、旧 `backend/**`、`web/**`。未进入 YU-04 typed capability/UoW 或 YU-07 之后的业务合同化任务。

## RED 纪律

本任务没有业务或生成行为变化：YU-02 parent 上的派生物已经是固定 target generator 的精确输出。因此行为 RED 为 `NOT_APPLICABLE(no_behavior_delta)`。没有通过破坏生成文件、删工具或制造环境失败来伪造 RED；本任务的目标是重建、回读和封板既有语义。

## 固定输入与规范化基线

| 输入 | SHA-256 |
| --- | --- |
| `contracts/proto/iot_delivery.proto` | `861140d7cc61805b1f465cd05cbaf8a0988600a0d58e19abff46c74bde96cbb5` |
| `contracts/generated/manifest.json` | `d146c5b52b334c7512c3e9079a76fe9e73fffbea164da6f718170d83739c2671` |
| `contracts/generated/operation-plans.json` | `ef1bfc7fed448ffe5ae381cfd19b73dd00da14fdcb6ff854a1f96cebee6608a6` |

YU-02 parent 与当前结果均通过固定 `pkg/operationplan` 的 `Decode -> Normalize -> Validate -> CanonicalJSON` 处理；不是仅以 `jq -S` 或文本排序代替语义规范化。结果为：

```json
{
  "base": "4b2840a93f379242c2b05e04613f59b09590f52c",
  "operationCount": 12,
  "baseDigest": "ef1bfc7fed448ffe5ae381cfd19b73dd00da14fdcb6ff854a1f96cebee6608a6",
  "currentDigest": "ef1bfc7fed448ffe5ae381cfd19b73dd00da14fdcb6ff854a1f96cebee6608a6",
  "equivalent": true
}
```

该规范化比较覆盖完整 Plan：identity/domain/application/use case、request/response、public/tenant binding、authentication/permission/mode、transaction/idempotency、composition/required operations/permission closure、application requirements、RPC 与全部 HTTP bindings。计数、operation ID 与 RPC binding 均要求唯一。

## 12 RPC 语义矩阵

共同字段：domain/application 为 `delivery/management`；`public=false`、`tenantRequired=false`；认证为 `api-key,jwt,service-token`；permission mode 为 `all`；composition 为 `local`；idempotency 为 `none`；只有标准 gRPC binding，没有 HTTP binding。当前 schema 没有为这些计划声明 per-operation timeout/retry，本任务不扩张合同。

| RPC | Operation ID | Request -> Response | Permission | Transaction |
| --- | --- | --- | --- | --- |
| `GetDashboard` | `delivery.dashboard.get` | `GetDashboardRequest` -> `GetDashboardResponse` | `delivery.dashboard.read` | `read_only` |
| `ListItems` | `delivery.items.list` | `ListItemsRequest` -> `ListItemsResponse` | `delivery.work-items.read` | `read_only` |
| `CreateItem` | `delivery.items.create` | `CreateItemRequest` -> `WorkItemResponse` | `delivery.work-items.create` | `local` |
| `UpdateItem` | `delivery.items.update` | `UpdateItemRequest` -> `WorkItemResponse` | `delivery.work-items.update` | `local` |
| `CreateItemComment` | `delivery.items.comment.create` | `CreateItemCommentRequest` -> `CommentResponse` | `delivery.work-items.comment.create` | `local` |
| `UpdateItemContext` | `delivery.items.update-context` | `UpdateItemContextRequest` -> `WorkItemResponse` | `delivery.work-items.context.update` | `local` |
| `AdvanceGate` | `delivery.items.advance-gate` | `AdvanceGateRequest` -> `WorkItemResponse` | `delivery.work-items.gate.advance` | `local` |
| `CloseItem` | `delivery.items.close` | `CloseItemRequest` -> `WorkItemResponse` | `delivery.work-items.close` | `local` |
| `CreateProject` | `delivery.projects.create` | `CreateProjectRequest` -> `ProjectResponse` | `delivery.projects.create` | `local` |
| `CreateRelease` | `delivery.releases.create` | `CreateReleaseRequest` -> `ReleaseResponse` | `delivery.releases.create` | `local` |
| `CreateSprint` | `delivery.sprints.create` | `CreateSprintRequest` -> `SprintResponse` | `delivery.sprints.create` | `local` |
| `CreateMilestone` | `delivery.milestones.create` | `CreateMilestoneRequest` -> `MilestoneResponse` | `delivery.milestones.create` | `local` |

四层计数均为 12：manifest methods、`operation-plans.json` operations、生成 policy plan functions、生成 RPC executor methods。生成 REST 文件只有 package 声明，与“无 HTTP binding”一致。

## 双 generate 与完整所有权集合

使用 YU-02 固化的单一入口：

```text
working directory: backend-yunka/
GO=/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go
TOOLS_DIR=/workspace/scratch/3845cec7410c/yu01-tools
make yunka-generate
make yunka-check
make yunka-generate
make yunka-check
```

每轮结果相同：protobuf-Go `2` files、providers `0` bindings、contract `1` service / `35` messages / `5` application files、modules checked、assembly `1` binding。两轮之间以及相对 parent 都没有新增、删除、重命名或内容变化。

目标 ownership 规则确认的 16 个现存 `generated-only` 文件包含 protobuf manifest/outputs、五个 contract outputs、assembly、application/policy、REST/RPC 与 module outputs。按相对路径排序后，对“SHA-256 + path”清单再次取 SHA-256，两轮均为：

```text
98e9b34044c67068263d4cb6d13e701469f19ec0a2efd2053f7e7fadcdfe92bc
```

`make yunka-check` 两轮均通过，且不会自动修复工作树。`make yunka-verify` 仍只是该 read-only full check 的别名。

## 手写 extensionPlan 边界

`internal/delivery/application/operations.go` 当前有 15 次手写 `extensionPlan` 使用：13 个是 extension-only，另有两个与 canonical generated plan 共享 Operation ID 的兼容分支：

| Operation ID | 手写权限 | Canonical 权限 | 既有差异 |
| --- | --- | --- | --- |
| `delivery.dashboard.get` | `delivery.items.read` | `delivery.dashboard.read` | 手写分支缺少 `service-token` 认证和 canonical RPC metadata |
| `delivery.items.list` | `delivery.items.read` | `delivery.work-items.read` | 手写分支缺少 `service-token` 认证和 canonical RPC metadata |

生成器只拥有带 generated marker 的 `zz_yunka_*_gen.go` 及已声明派生路径，不扫描、收集或删除 `operations.go`。生成 gRPC 装配始终使用 12 个 canonical policy plans；HTTP/MCP compatibility 仍走既有手写路径。上述两个同 ID 双 plan 风险已在总台账分配给 YU-10，本任务既不掩盖，也不越界修复。其余 13 个 extension-only plans 留待 YU-07 至 YU-13 分批合同化。

## Application Graph 边界

`yunka context` 仍将 `contracts/generated/application-graph.json` 描述为 missing；重复 top-level generate 不产生该文件，full check 仍通过。目标框架把 graph 定义为独立 `yunka graph build` 输出，默认路径为 `.yunka/application-graph.json`。因此本任务未生成或提交 graph，也未把 context 的描述差异误记成 12 RPC 派生漂移。

## 回归结果

以下均在固定 Go 1.25.13、Yunka `057ebcf...` 和固定 protobuf 工具链下真实执行。Go 回归以 `backend-yunka/` 为模块边界并显式设置 `GOWORK=off`；仓库根 `go.work` 还包含仅供对照的旧 `backend/`，其依赖目标 Yunka 已不存在的 `compat/go-kit-kit-log`，因此本任务不把根 workspace 声称为通过：

```text
GOWORK=off go test ./internal/delivery -run '^TestGeneratedRPCOperationPlansRemainCanonical$' -count=1  PASS
GOWORK=off go mod tidy -diff                                                                    PASS
GOWORK=off go test ./... -count=1                                                               PASS
GOWORK=off go vet ./...                                                                         PASS
GOWORK=off go test -race ./... -count=1                                                         PASS
make yunka-check                                                                                 PASS
git diff --check                                                                                 PASS
```

固定 parent checkout 与当前 checkout 分别执行无 `--base` 的 `yunka audit`，两者都只返回同一个稳定 Finding ID：`AUDIT-AUTH-001:internal/delivery/application/adapter.go:github.com/hvritual/yunka.io/gateway/authz`。因此本任务没有新增 audit finding。目标 `audit --base` 在 monorepo 子目录项目上存在下述已确认问题；本任务使用双 checkout 回读作为公开 seam 绕过，但 audit 仍不替代 plan 等价或生成门禁。

## 框架问题交接：YU03-FP-001

该卡是本任务的实证交接，供独立 YU-00F 框架问题总账吸收；没有修改 Yunka 源码，也不把绕过成功称作框架修复。

| 字段 | 内容 |
| --- | --- |
| `id` | `YU03-FP-001` |
| `version` | `1` |
| `status` | `confirmed` |
| `problem.title` | `yunka audit --base` 将子目录项目的基线路径错误解析为仓库根路径 |
| `expected` | 对 Git 仓库内 `backend-yunka` 项目执行 `audit --base 4b2840a...` 应读取 `backend-yunka/go.mod`、manifest 和 `internal/**` 基线，得到 `existing=1,new=0,fixed=0`。 |
| `observed` | exit 1；首个错误为 `git show 4b2840a...:go.mod` 不存在，Git 明确提示真实路径为 `backend-yunka/go.mod`。 |
| `evidence` | 同一 base 即当前 HEAD；`git diff --quiet 4b2840a... -- backend-yunka` 为零；带前缀的 go.mod/manifest Git objects 可读，无前缀对象不存在。 |
| `impact` | 当前 monorepo 子目录消费者无法直接使用 T2 `audit --base` 债务分类；不影响无 base audit、canonical generate/check 或本任务 12-plan 等价。 |
| `scope` | 已实证限定为 `audit --base` + project root 不等于 Git top-level；未把未执行的下游命令推断为已确认缺陷。 |
| `reproduction` | 固定 Yunka `057ebcf...`：`yunka audit --root <repo>/backend-yunka --base 4b2840a... --format agent-json`。 |
| `hypothesis` | audit baseline loader 将 project-relative path 直接作为 Git-root-relative path。 |
| `counter_evidence` | 相同项目不带 `--base` 时成功；框架 Git-root fixture 的 `BuildWithBase` 测试成功，说明 Git-root 项目不受影响。 |
| `root_cause` | `app/cmd/audit/command.go` 对 go.mod 使用固定 `go.mod`；manifest 只相对 project root；`CollectGoSourceAtCommit` 的 `--full-tree` source root 也缺少 `backend-yunka/` Git prefix。只修第一个路径仍会失败或静默得到空基线 source。 |
| `should_have_caught` | 需要 Git 根下嵌套项目 fixture，断言 `--root backend-yunka --base HEAD` 的 go.mod、manifest、Go source 三者都从同一 repo-relative prefix 读取。 |
| `systemic_gap` | CLI 没有 `--git-root`/`--base-path-prefix`，实现也没有统一计算 project root 相对 Git top-level 的前缀；当前文档未声明 root 必须等于 Git top-level。 |
| `workaround` | 在固定 parent checkout 和当前 checkout 分别运行无 base `yunka audit`，比较排序后的稳定 Finding IDs；保持其他生成/语义门禁不变。 |
| `workaround_verification` | 两个 checkout 均 exit 0，且都只返回相同的预存 `AUDIT-AUTH-001...` ID；当前工作树与生成物未被 audit 修改。 |

## 残余限制

- PowerShell hard gate 在 Linux 云端没有 `pwsh`，本任务不重复声称执行；其固定 revision/tool path 已在 YU-02 完成。
- 根 `go.work` 的旧 `backend/` 仍引用目标 Yunka 已删除的 `compat/go-kit-kit-log`；YU-03 的全量 Go 回归是 `backend-yunka` 模块级回归，不声称旧 backend 或根 workspace 通过。
- Application Graph 的 context 路径/提示与独立 graph 命令默认输出不一致，保持已知非阻塞事实；本任务不创建框架问题卡。
- `YU03-FP-001` 仅由双 checkout audit 绕过；固定框架本身仍未修复，后续应进入 YU-00F 问题总账，而不是在消费者内复制 baseline loader。
- 本证据只封板当前 12 个 canonical RPC plans，不把 15 次手写 extensionPlan 使用混入生成数量，也不证明后续本地账号密码、真实授权链或跨 transport 行为已经完成。
