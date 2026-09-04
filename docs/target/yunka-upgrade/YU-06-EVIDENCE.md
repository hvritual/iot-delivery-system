# YU-06 generated Assembly/runtimehost 装配闭环证据

> 文档类型：任务证据（EVIDENCE）
> 核验时间：2026-09-04
> 固定 parent：`6f35d677336323c5d5e4c3d867777b0221c49560`
> parent tree：`95eeec3462df6cc5736ecebba6185873662da12f`
> 工作分支：`codex/yu-06-assembly-runtimehost-closure`
> Yunka gitlink / materialized HEAD：`057ebcf88a87303eb633eb6e604d306f633dfac0`

## 结论与边界

消费者现在通过生成 Assembly 的公开 `BindRuntimeWithCapabilities` seam 完成唯一一次运行时构造。显式 `platform.Provider`、canonical `delivery` 模块、附加的 `delivery-event-runtime` provider、typed capability 解析、业务 Application factory、root Executor、手写 HTTP compatibility routes、生成 gRPC transport 和两个 runtimehost component 全部处于同一个 `build → register → start` 闭环。

本任务关闭的是消费者仍使用预构造 `Factories + Executor` 兼容分支以及 HTTP 在 `App.Start` 后注册的旧装配路径。没有删除或覆盖生成的 canonical `delivery` module，也没有把合法的 `delivery-event-runtime` provider 冒充成同名替换模块。精确旧 import 身份 `yunka.io/framework|gateway|pkg` 已由 YU-01 清零，本任务重新验证其没有回流。

本任务不改变业务 DTO、OperationPlan、权限、事务/Outbox 语义、REST/gRPC/MCP 外部行为或部署配置；不修改 `third_party/yunka/**`、生成物、旧 `backend/**` 或 `web/**`。当前 protobuf 没有 HTTP annotations，因此 generated runtime inventory 的 `routeCount=0` 是真实边界；20 个手写 HTTP compatibility patterns 已在启动前注册，但不会被伪造为 generated route inventory。

## 固定框架合同

固定框架版本的公开合同要求并支持以下顺序：

1. 生成 Assembly 创建 module catalog，并让 kernel 构造同一个 `core.App`。
2. modules 导出不可变 typed capability snapshot。
3. `BindRuntimeWithCapabilities` 只在 bootstrap 期间解析 capability 并返回 `ApplicationFactories + Executor`。
4. 生成 Assembly 构造 Application 并注册 gRPC transport。
5. kernel 启动 Platform、modules，再启动按名称排序的 runtime components。

框架会拒绝 binder 与预构造 `Factories/Executor` 并存，也会拒绝 binder 缺少显式 Platform。`CapabilitySet` 的身份是 logical name、Go package 与 Go type；它只允许强类型 key 解析，不是字符串 service locator。`AdditionalModules` 是 additive seam，同名 `delivery` descriptor 会 fail closed，不能用来覆盖生成模块。

因此 YU-06 不需要修改框架，也没有产生新的框架问题卡。问题根因是 consumer composition root 没有使用已有公开 seam。

## 真实 RED

### RED-1：旧兼容装配路径

在未修改产品代码的固定 parent 上新增 AST 门禁，检查生产 `generatedassembly.BootstrapOptions` 不得继续提供预构造 `Factories/Executor`，且必须委托 `BindRuntimeWithCapabilities`。测试确定失败：

```text
--- FAIL: TestBootstrapUsesGeneratedCapabilityBinderForAllTransportRegistration
    runtime_binding_test.go:38: bootstrap still supplies prebuilt Factories/Executor outside the generated runtime binder
```

这不是编译失败或缺工具失败；它直接识别旧消费者路径。

### RED-2：Platform/diagnostics 未闭合

在固定 parent 的一次性干净 worktree 中只加入真实运行时诊断测试，启动同一 SQLite 应用并读取 host-owned `__yunka/diagnostics`。测试确定失败：

```text
--- FAIL: TestApplicationUsesPlatformCapabilityHealthAndCompleteHostedInventory
    runtime_diagnostics_test.go:53: diagnostics health checks =
    map[string]core.HealthStatus{
      "module.delivery-event-runtime":"healthy",
      "runtime.grpc-server":"healthy",
      "runtime.http-server":"healthy",
    }, want healthy "composition.capabilities"
```

这证明旧消费者没有显式 Platform/binder composition。一次性 RED worktree 随后删除，未写入任务成果。

最初云端 shell 没有 `go` 的失败仅是环境缺工具，不计 RED；随后使用校验过 SHA-256 的 Go `1.25.13` 官方工具包重跑以上测试。

## 最小 GREEN 实现

`internal/bootstrap/runtime_binding.go` 新增一次性 consumer binder，并只做以下构造工作：

- 从 immutable snapshot 解析 `sqlite.transaction-factory`、`delivery.outbox`、`delivery.notifications`、`delivery.projection` 四个 typed capabilities；
- 用上述唯一资源构造 `delivery.Service`、受审计 adapter、唯一 root Executor、配置修订 Operations 与业务 Operations；
- 样例 seed 仍通过认证 Operations、root transaction 和 transactional Outbox 执行，并发生在 dispatcher 启动前；
- 构造提醒 scheduler、Obsidian/notification subscriptions，并一次性交给 `delivery-event-runtime`；
- 在返回生成 Assembly 之前注册手写 HTTP compatibility routes；生成 gRPC 随后注册，最后才启动 App-owned components。

最终顺序为：

```text
runtimehost 准备 listener/mux
→ generated Assembly 构造 Platform/App/modules 与 capability snapshot
→ consumer binder 构造唯一 Service/Executor/Operations，并注册 HTTP
→ generated Assembly 构造 Application 并注册 gRPC
→ Platform、modules、gRPC/HTTP components 启动
```

`applicationRuntimeBinder` 不保存 `context.Context` 或 `CapabilitySet`，重复绑定会 fail closed。`Application.Close` 只委托同一个 `core.App`，不再形成第二条手工资源关闭路径。

`delivery-event-runtime` 在 descriptor 组合时先接管数据库、transaction factory、Outbox、notification inbox、projection、dispatcher 与 broker；在 binder 阶段一次性接收 Application-specific reminder/subscriptions。未绑定模块不能启动或报告健康，失败时仍能清理已准备的 dispatcher、broker 和数据库。正常关闭顺序为 ingress components 先停，随后 module 内部执行 reminder、dispatcher、逆序 subscriptions、broker、database；重复关闭幂等。

## 行为与生命周期证据

新增或加强的测试覆盖：

| 证据 | 验证内容 |
| --- | --- |
| `runtime_binding_test.go` | 禁止预构造 `Factories/Executor`；binder 恰好一次；HTTP 只在 binder 中注册；不保留 Context/CapabilitySet |
| `runtime_diagnostics_test.go` | 同一 App ready；Platform、event module、gRPC、HTTP 全部 healthy；唯一 RPC server；两个 host component 具有 Start/Health/Shutdown |
| `runtime_diagnostics_test.go` | `Close` 两次均成功，HTTP/gRPC 两个真实监听地址可立即重新绑定 |
| `runtime_diagnostics_test.go` | binder 中提醒配置失败时应用不返回，两个预留监听地址均可重新绑定 |
| `deliveryruntime/runtime_test.go` | 一次性 Application binding、依赖顺序、启动失败逆序清理、未绑定 fail closed、停止后拒绝 late bind |
| 既有 HTTP/gRPC smoke | 保持既有 transport 行为并通过完整 package regression |

真实 GREEN 定向结果：

```text
go test ./internal/bootstrap ./internal/deliveryruntime -count=1       PASS
go test -race ./internal/bootstrap ./internal/deliveryruntime -count=1 PASS
```

## 生成、归属与完整回归

本任务不修改 proto、module manifest 或任何生成物。目标生成器的重复生成、完整检查、生成物摘要、ownership、audit 和模块身份验证结果会在本任务提交前固定到本节；任何未通过项都会阻止合并 `main`。

实际验证结果：

```text
Go 1.25.13 / protoc 3.21.12 / protoc-gen-go 1.36.11 /
protoc-gen-go-grpc 1.6.2                         PINNED + PASS
make yunka-context                              PASS
make yunka-generate（连续两次）                 PASS；生成目录零 drift
make yunka-check                                PASS；--full 全项 OK
GOWORK=off go mod tidy -diff                    PASS；无输出
GOWORK=off go test ./... -count=1               PASS
GOWORK=off go vet ./...                         PASS
GOWORK=off go test -race ./... -count=1         PASS
git diff --check                                PASS
旧 yunka.io/framework|gateway|pkg import/module PASS；三种清单均零命中
```

第一次全量回归真实捕获既有 `no_bypass_guard_test.go` 只允许在 `application.go` 构造 `delivery.Service`，与本任务把组装职责迁入 binder 的目标冲突：

```text
internal/bootstrap/runtime_binding.go:71:13 constructs delivery.Service outside bootstrap assembly
```

门禁没有被泛化或删除。修复后它只允许精确文件 `internal/bootstrap/runtime_binding.go` 构造 Service，并新增反证测试：旧 `application.go` 路径仍必须被拒绝；其他任何生产文件仍必须经过 Operations boundary。随后定向、普通全量与 race 全量均通过。

15 个生成物的排序 SHA-256 清单摘要为：

```text
01e225868c9fa96678c232b86b5a11ad290dd8ba1d9d1fa46332bb19546269ae
```

关键生成物保持固定摘要：

| 文件 | SHA-256 |
| --- | --- |
| `contracts/delivery/v1/iot_delivery.pb.go` | `d08786f13fc161eb5007e38596f5e5a7718b1d8c05a5b94be030a4eb13102dad` |
| `contracts/generated/assembly-plan.json` | `0fd0b90d4eb81e3dce1ddc29eff75965c27b6faf75a5d8157284440a861b8ca1` |
| `contracts/generated/manifest.json` | `fe1a78e0a64399c4c43e9ad558d2719da59662bb12dde3b358ae0fbe466365ea` |
| `internal/assembly/zz_yunka_assembly_gen.go` | `080cc5144f08f6e1fa1db788e65b787556f123d68a82a5479eac9c800b9905b3` |
| `internal/delivery/transport/rpc/zz_yunka_management_operation_executor_gen.go` | `d4e7d24a761d60e339a77fa3e22be959b1ff7d634bf3726be5533cccbdf15055` |

Ownership 检查把所有本轮 Go 源码判定为 `developer-code/editable`；README 和仓库级证据文档没有 canonical generator ownership。抽查的 Assembly、Application port、RPC executor 与 contract manifest 均为 `yunka-generator/generated-only`，且没有被手改。

无 `--base` 的 audit 仍只有 YU-03 已记录的稳定 finding：

```text
AUDIT-AUTH-001:internal/delivery/application/adapter.go:github.com/hvritual/yunka.io/gateway/authz
```

本任务没有修改该文件，也没有新增 finding。独立只读审查再次核对 binder 顺序、capability 类型、失败清理、关闭幂等和生成目录，未发现阻塞项或越界改动。

## 变更归属

允许编辑的消费者文件：

- `backend-yunka/internal/bootstrap/application.go`
- `backend-yunka/internal/bootstrap/runtime_binding.go`
- `backend-yunka/internal/deliveryruntime/runtime.go`
- 对应的 `*_test.go`
- `backend-yunka/no_bypass_guard_test.go`
- `backend-yunka/README.md`
- 本证据文档

禁止且未修改：

- `third_party/yunka/**`
- `backend-yunka/internal/assembly/**`
- `backend-yunka/internal/delivery/transport/rpc/**`
- `backend-yunka/internal/delivery/application/ports_gen.go`
- `backend-yunka/contracts/generated/**`
- `backend/**` 与 `web/**`

## 残余边界与下一任务

- generated route inventory 只描述 protobuf authoring 产生的 routes；本轮保留 `routeCount=0`，后续合同化任务按 canonical protobuf 逐步消除手写 extension surface。
- `delivery-event-runtime` 仍是 consumer-owned handwritten descriptor wrapper，因为固定版本的官方 module manifest 尚不能表达 `Provides`；这是已验证公开扩展点，不是框架源码修改。
- 当前 development local API-key 身份链仍是已知临时边界；本地成员凭据、session、真实 human Principal 和持久授权属于 YU-18 以后，不在本轮冒充完成。
- 下一独立任务为 **YU-07「Project 与 ListProjects canonical protobuf typed contracts」**。它必须以 YU-06 合并后远端 `main` 的精确提交为 parent，先建立 plan-first RED，再修改 canonical proto 并由生成器产生派生物；不得手改生成文件或顺带进入 Release/Sprint/Milestone。
