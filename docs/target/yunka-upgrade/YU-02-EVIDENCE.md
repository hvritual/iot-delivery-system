# YU-02 项目配置、工具链与生成流程证据

> 文档类型：任务证据（EVIDENCE）
> 核验时间：2026-09-04T10:46:22Z
> 固定 parent：`80597dd00eb9263c93e5c7f71b2d0d733dbd53ac`
> 工作分支：`codex/yu-02-generation-workflow`
> Yunka gitlink / materialized HEAD：`057ebcf88a87303eb633eb6e604d306f633dfac0`

## 边界与结论

本任务只固化消费者的目标 CLI 配置核验、protobuf include、固定工具链调用和生成/check 入口。未改动 Yunka 源码、业务语义、旧 backend、Web、运行时装配或任何手工生成物。

`.yunka/project.json`（v2 profile）、`.yunka/providers.json`（schema v1）、`.yunka/protobuf-go.json`（generator-owned schema v1）和 `.yunka/dev.json`（schema v2）均已被 `yunka context --root backend-yunka --json` 解析为当前目标 CLI 的 profile/provider/protobuf/dev locations。`yunka ownership inspect` 将 project/providers/dev 标记为 `developer-config/editable`，将 protobuf output manifest 标记为 `protobuf-go-generator/generated-only`，因此没有为了产生变更而重写这些已对齐文件。

目标 project profile 不提供持久化的外部 protobuf include 字段；`contracts/sources.json` 的公开 inventory seam 也会拒绝解析后逃离 consumer root 的 include。故保留目标 CLI 的公开 `--proto-path` seam，但只在 Makefile/硬门脚本中固定：操作者使用 `make yunka-generate` 或 `make yunka-check`，不再临时记忆该参数。没有 vendor 或手抄 `yunka/dsl/v1/options.proto`。

## 真实 RED

工具位置来自 `/workspace/scratch/3845cec7410c/yu01-tools`，版本为 `libprotoc 3.21.12`、`protoc-gen-go v1.36.11`、`protoc-gen-go-grpc 1.6.2`。在已提供 compiler/plugins、但**没有** `--proto-path` 时执行：

```bash
PROTOC_GEN_GO="$tool_root/bin/protoc-gen-go" \
PROTOC_GEN_GO_GRPC="$tool_root/bin/protoc-gen-go-grpc" \
  "$tool_root/yunka-057ebcf" generate --root backend-yunka --full \
  --protoc "$tool_root/protoc-21.12/bin/protoc" --format agent-json

PROTOC_GEN_GO="$tool_root/bin/protoc-gen-go" \
PROTOC_GEN_GO_GRPC="$tool_root/bin/protoc-gen-go-grpc" \
  "$tool_root/yunka-057ebcf" check --root backend-yunka --full \
  --protoc "$tool_root/protoc-21.12/bin/protoc" --format agent-json
```

两条命令均以 exit `1` 失败，稳定的真实错误为：

```text
generate/check protobuf Go: protobuf-go: protoc failed: exit status 1:
yunka/dsl/v1/options.proto: File not found.
iot_delivery.proto:6:1: Import "yunka/dsl/v1/options.proto" was not found or had errors.
```

这证明的是 protobuf import 解析行为，不是缺少 protoc 或插件的环境失败。

## 最小 GREEN 与重复性

新增 `backend-yunka/Makefile` 的 `yunka-revision-check` 会验证 repository gitlink、materialized/clean submodule 和固定 include 文件；`yunka-cli-check` 再验证 Go 1.25.13，`yunka-toolchain-check` 再验证 protoc 3.21.12 与两个固定插件版本。CLI 只能从固定 submodule 源运行，入口是：

```text
go -C <repository>/third_party/yunka/app run ./cmd ... --root <absolute backend-yunka path>
```

绝对 project root 防止 `go -C` 改变 CLI cwd 后把 `--root .` 错解为 Yunka app。`yunka-generate` 是唯一可写生成入口；`yunka-check` 和 `yunka-verify` 都是 full、只读 check，CI 不会自动修复漂移。

固定 source-CLI 的两轮 generate/check 都产生以下 Green stages：

```text
SKIPPED   domains   no managed domains
GENERATED protobuf-go files=2
GENERATED providers bindings=0
GENERATED contract  services=1 messages=35 applicationFiles=5 out=contracts/generated
CHECKED   modules   modules
GENERATED assembly  bindings=1 codeOut=internal

OK        protobuf-go files=2
OK        providers bindings=0
OK        contract  services=1 messages=35 applicationFiles=5
OK        modules   modules
OK        assembly  bindings=1
```

每轮后对完整已知生成所有权路径集执行下列检查均为零；它显式覆盖 protobuf manifest/outputs、contract outputs、assembly、application/policy/REST/RPC 的 `zz_yunka_*` 所在目录，以及 module outputs：

```bash
git diff --exit-code -- \
  .yunka/protobuf-go.json contracts/generated contracts/delivery/v1 \
  internal/assembly internal/delivery/application internal/delivery/policy \
  internal/delivery/transport/rest internal/delivery/transport/rpc modules/delivery
```

用目标 CLI `ownership inspect` 从上述路径筛出的 16 个 `generated-only` 文件，其排序 SHA-256 清单两次相同，摘要为：

```text
98e9b34044c67068263d4cb6d13e701469f19ec0a2efd2053f7e7fadcdfe92bc
```

没有必要的 generated delta；上述 canonical generate/check outputs 已由固定 generator 复现，因此本任务不提交生成物。这只是 YU-02 的配置/工具链验收，不等价于 YU-03 对 12 个 operation plans 的语义等价封板。

`yunka context` 还会描述 `contracts/generated/application-graph.json`，并在当前树报告 `state: missing`。重复 `yunka generate` 后该状态保持不变，而 `yunka check --full` 通过；目标 README 与 `app/cmd/graph` 将 W09 graph 定义为独立的 `yunka graph build` 输出，默认路径是 `.yunka/application-graph.json`，并非 top-level generate 的产物。本任务据此把它记录为 target context 的描述性、非阻塞缺口，不将“所有 context locations 均存在”作为已验证结论，也不越界生成新的 graph artifact。

## 固定身份与校验

```text
git ls-tree HEAD third_party/yunka
160000 commit 057ebcf88a87303eb633eb6e604d306f633dfac0 third_party/yunka

git -C third_party/yunka rev-parse HEAD
057ebcf88a87303eb633eb6e604d306f633dfac0

adc735215a3e3266d58303116a7bd36d5bb9f3608f66e762639e4ecb0fe8763f  yunka-057ebcf
e6a75cf6660945fbfec0d321808442ce3649cd7820079eece1098c881b315f55  protoc
2f1a7a93d8f7e9253f6d20c9e37e78e94effa7fd6feee305bfc8bbfa72b2ca59  protoc-gen-go
89a6d5ad10d2d2fc8f2edf3dc1740a7779fc055c2255843eab973ac6d6ed8c95  protoc-gen-go-grpc
```

source-CLI path 已由 `make -n yunka-generate` 展开并确认传递 absolute `--root`、固定 target gitlink guard、`--full`、固定 protoc 与 framework include；使用命令行注入 `YUNKA_CLI` 或 `YUNKA_PROTO_PATH` 的值也会被 Makefile 的固定定义覆盖。随后以 `/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go` 实际执行，以下均 PASS：

```text
GO=/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go make yunka-toolchain-check
GO=/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go make yunka-context
GO=/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go make yunka-verify
GO=/workspace/scratch/7cbedf9749a1/tools/go1.25.13/bin/go make yunka-generate   # repeated
```

默认 source-CLI 的重复 generate 从 `backend-yunka` 当前目录验证实际受管生成路径零 diff；空 diff 的 SHA-256 为：

```text
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

`yunka audit --root backend-yunka --format agent-json` 可读并只报告预存的 `AUDIT-AUTH-001`（`internal/delivery/application/adapter.go` 对 canonical authz 的直接 import）；本任务没有修改该路径或引入新的 audit finding。PowerShell 在本 Linux 容器不可用，因而没有执行 `run-s0-04-09-hard-gate.ps1`；脚本仅将其 target gitlink 更新到 `057ebcf...`，并将 protoc asset path 对齐为 `.tools/protoc-21.12/bin/protoc.exe`。

## 回归与残余限制

已通过：目标 CLI `context`、`ownership inspect`、真实 RED、固定 source-CLI 的两次 full generate/check、`yunka-toolchain-check`/`yunka-context`/`yunka-verify`、重复 generate 的真实路径零漂移、`make yunka-verify`（只读 full check）和 `git diff --check`。

使用固定 Go 1.25.13 与 `GOWORK=off`，依赖和 Go 回归已实测通过；两次 tidy 及其 diff 均未改变 `go.mod`/`go.sum`，二者的 digest 保持：

```text
88022e2fb66b10110f9cdeaa025d41b1cbe84a2da384b6fb4926bccfa93fb531
```

```text
GOWORK=off go mod tidy             # PASS, twice
GOWORK=off go mod tidy -diff       # PASS
GOWORK=off go test ./... -count=1  # PASS
GOWORK=off go vet ./...            # PASS
GOWORK=off go test -race ./... -count=1  # PASS
```

Web 未在本原子任务修改，故不将 Web dependency 状态列为本任务残余阻塞。PowerShell 仍因 Linux runner 没有 `pwsh` 而未执行；已完成静态最小审查与固定 gitlink/tool path 更新。
