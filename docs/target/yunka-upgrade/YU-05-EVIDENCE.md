# YU-05 Delivery 事件运行时 typed capability 与生命周期证据

> 文档类型：任务证据（EVIDENCE）
> 核验时间：2026-09-04
> 本地固定 parent：`3d2205ab79f202aa1f1f6a37cd948936c2ec6b96`
> 对应远端 parent：`2b7ed49c7bbbb7e620599bde02f0604766eadd0e`
> parent tree：`b26ae7511d2883a20533766c9ed8620c070ae4b4`
> 工作分支：`codex/yu-05-app-owned-runtime-capabilities`
> Yunka gitlink / materialized HEAD：`057ebcf88a87303eb633eb6e604d306f633dfac0`

## 结论与范围

SQLite Outbox、通知读模型和 Obsidian 投影现在是 `delivery/management` Application 的显式 typed capability；它们与 YU-04 的 SQLite transaction factory 由同一个 `delivery-event-runtime` provider 导出。生成 Assembly 只在 bootstrap 快照中解析四个静态 Go contract，并把普通强类型字段传入 Application factory。运行期不保存 `CapabilitySet`，不存在字符串查找、反射注入、全局 registry 或第二套 transaction/event runtime。

`delivery-event-runtime` 是唯一的 App-owned 事件资源生命周期 owner。原来四个互相独立、按名称排序的 runtime components 已移除；数据库、Outbox dispatcher、截止提醒、notification/projection subscriptions 和 local broker 现在按显式依赖顺序启动/检查/逆序关闭。HTTP/gRPC 仍由 runtimehost components 托管，generated Assembly/runtimehost 的进一步结构调整留给 YU-06。

本任务没有改变业务 DTO、OperationPlan、权限、事务/Outbox 原子性、通知渠道、投影内容或 REST/gRPC/MCP 行为；没有修改 `third_party/yunka/**`、旧 `backend/**` 或 `web/**`。

## 消费者缺口与真实 RED

YU-04 parent 把以下资源分别作为 runtime components 传给 `core.App`：

- `delivery-sqlite`
- `delivery-sqlite-outbox-broker`
- `delivery-sqlite-outbox-dispatcher`
- `delivery-due-reminders`

目标框架会确定性按组件名称排序启动，再整体逆序关闭。旧名称顺序导致提醒 worker 先于 SQLite health component 启动，而关闭时 dispatcher/broker/SQLite 先关闭、提醒 worker 最后停止；notification/projection subscription 也只附着在 broker component 的手写清理闭包中。该问题是消费者生命周期建模债务，不归因于框架。

先新增真实 Application 诊断测试；在不改生产实现时运行：

```text
--- FAIL: TestApplicationOwnsDeliveryEventPipelineAsTypedModule
diagnostics modules = [{Name:delivery ...} {Name:sqlite-transaction ...}],
want typed delivery-event-runtime with complete lifecycle
```

测试最初曾错误地把 HTTP 外层 `diagnostics.Report` 直接解码为 `core.DiagnosticsReport`，得到空 modules；这是测试读取错误，已修正且没有计作 RED。上面的失败是在正确读取 `report.core.modules/components` 后取得的有效行为 RED。

## GREEN 设计

### Typed capability

protobuf application declaration新增三个 canonical capability：

| 名称 | Go contract | 用途 |
| --- | --- | --- |
| `delivery.outbox` | `internal/deliveryruntime.Outbox` | 同时提供 durable Store 与 caller-UoW TransactionalStore |
| `delivery.notifications` | `internal/deliveryruntime.Notifications` | 本地耐久通知读模型 |
| `delivery.projection` | `internal/deliveryruntime.Projection` | Obsidian materialized-view exporter |

`sqlite.transaction-factory` 保持 YU-04 合同不变，但改由同一个事件运行时 provider 导出，确保 root Executor 和 Application dependency 引用相同 factory。当前 module manifest 仍不能生成 `Provides`，所以 `internal/deliveryruntime/runtime.go` 使用目标文档允许的手写 descriptor；descriptor 自身不做 I/O，只返回已为该 App 准备的 module instance。

当前 Application adapter 仍在 bootstrap composition root 中预构造，factory 对四个生成依赖执行非空与实例一致性校验。这保留现有 HTTP/MCP 入口并确保生成依赖没有成为装饰性声明；把全部 consumer runtime construction 移入 generated binder 属于 YU-06，而不是本任务扩张范围。

### App-owned lifecycle

正常路径：

```text
Start:    database ping -> dispatcher start -> reminder start
Health:   database ping + dispatcher health + reminder health
Shutdown: reminder stop -> dispatcher shutdown
          -> notification subscription close -> projection subscription close
          -> broker close -> database close
```

runtimehost 的 HTTP/gRPC components 在 module 之后启动，并在 module 之前关闭。若 reminder 启动失败，`core.App` 会调用同一 module 的逆序 cleanup；测试证明 dispatcher、subscriptions、broker 和数据库仍全部关闭。重复 Shutdown 幂等。

模块只保存 App/process-scoped typed resources。结构测试拒绝任何直接 `context.Context` 或 `modulecatalog.CapabilitySet` 字段；事件 handler 每次只消费调用方传入的 context，不把 Principal、trace、活动事务或 request-scoped repository 保存到 singleton。

## Ownership 与生成边界

目标 `yunka ownership inspect` 已逐项确认：

| 路径 | ownership |
| --- | --- |
| `contracts/proto/iot_delivery.proto` | `developer-contract/editable` |
| `internal/bootstrap/application.go` 及测试 | `developer-code/editable` |
| `internal/deliveryruntime/runtime.go` 及测试 | `developer-code/editable` |
| `contracts/delivery/v1/iot_delivery.pb.go` | `protobuf-go-generator/generated-only` |
| `contracts/generated/manifest.json` | `yunka-generator/generated-only` |
| `contracts/generated/assembly-plan.json` | `yunka-generator/generated-only` |
| `internal/assembly/zz_yunka_assembly_gen.go` | `yunka-generator/generated-only` |

所有 generated-only 文件均由固定 target canonical generator 产生，没有手改。YU-05 是跨 Application infrastructure/lifecycle 变更，不属于一个既有 Operation 的 AX7/ChangeSet 可编辑域；本任务没有伪造单 Operation Change Contract。

## 行为验证

新增与演进测试覆盖：

- `TestApplicationOwnsDeliveryEventPipelineAsTypedModule`：真实 runtimehost 诊断必须显示完整 typed module lifecycle，且四个旧 components 不得残留。
- `TestModuleExportsTypedCapabilitiesAndOwnsLifecycleInDependencyOrder`：四个 contract 可解析，正常启动/健康/逆序关闭顺序精确，二次 Shutdown 幂等。
- `TestAppCleansUpModuleInReverseOrderWhenReminderStartFails`：真实 `core.App` 启动失败路径仍逆序释放全部资源。
- `TestModuleDoesNotRetainRequestContextOrCapabilityResolver`：provider 不保存请求 context 或 bootstrap resolver。
- 既有真实 Application 回归继续覆盖 SQLite transactional Outbox、Obsidian 投影、本地收件箱、due reminder、Webhook/企业微信/SMTP adapter 和 runtime close。

## Canonical 生成与完整回归

固定 Go 1.25.13、Yunka `057ebcf...`、protoc 21.12 下，两轮 `make yunka-generate && make yunka-check` 均成功。每轮仍为 protobuf-Go `2` files、providers `0` bindings、contract `1` service / `35` messages / `5` application files、modules checked、assembly `1` binding；两轮前后 4 个实际变化的派生文件逐字节相同：

| 派生文件 | SHA-256 |
| --- | --- |
| `contracts/delivery/v1/iot_delivery.pb.go` | `d08786f13fc161eb5007e38596f5e5a7718b1d8c05a5b94be030a4eb13102dad` |
| `contracts/generated/assembly-plan.json` | `0fd0b90d4eb81e3dce1ddc29eff75965c27b6faf75a5d8157284440a861b8ca1` |
| `contracts/generated/manifest.json` | `fe1a78e0a64399c4c43e9ad558d2719da59662bb12dde3b358ae0fbe466365ea` |
| `internal/assembly/zz_yunka_assembly_gen.go` | `080cc5144f08f6e1fa1db788e65b787556f123d68a82a5479eac9c800b9905b3` |

排序后的摘要清单自身 SHA-256 为 `d6cf062556c5f98c28a8963dfc89f4b9397b9ae97c08d26ec2857ab090203c2d`。

模块级完整回归：

```text
GOWORK=off go mod tidy -diff            PASS
GOWORK=off go test ./... -count=1       PASS
GOWORK=off go vet ./...                 PASS
GOWORK=off go test -race ./... -count=1 PASS
make yunka-check                         PASS
git diff --check                         PASS
```

当前无 `--base` 的 `yunka audit --format agent-json` 仍只返回 YU-03 已记录的稳定 finding：

```text
AUDIT-AUTH-001:internal/delivery/application/adapter.go:github.com/hvritual/yunka.io/gateway/authz
```

本任务没有修改该文件，也没有新增 audit finding。没有发现新的已证实 Yunka 框架问题。

## 残余边界与下一任务

- 事件资源仍由 consumer composition root 从真实 SQLite、Vault 和通知配置构造；provider 从 App composition 时起取得唯一生命周期所有权。
- generated factory 目前通过实例一致性 guard 消费 typed dependencies；YU-06 才重组 generated Assembly/runtimehost construction flow，避免在 YU-05 同时迁移所有 HTTP/MCP 装配。
- Outbox 业务写入与事务失败/拒绝零副作用的全路径收敛仍属于 YU-15；本任务保留并回归既有行为，不提前宣称完成。
- 投影字段完整性与提醒幂等的专项封板属于 YU-14。

下一独立任务为 **YU-06「generated Assembly/runtimehost/HTTP/gRPC 装配重组与旧 module-identity 路径关闭」**。必须以本任务同步后的远端精确提交为 parent，重点验证 health/diagnostics/runtime closure 与现有 HTTP/gRPC smoke；不得顺带进入 YU-07 新业务合同。
