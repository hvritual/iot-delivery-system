# YU-04 SQLite typed capability 与单一根事务证据

> 文档类型：任务证据（EVIDENCE）
> 核验时间：2026-09-04
> 本地固定 parent：`7b1503e5e83d913874ee0287e8ecaf5a13810f8c`
> 对应远端 parent：`7c75287e31490f123b783c74b866eabf41f6bd2a`
> parent tree：`bd8a2b7f8e520303c3fd4e2632ddb350faa54feb`
> 工作分支：`codex/yu-04-sqlite-typed-capability`
> Yunka gitlink / materialized HEAD：`057ebcf88a87303eb633eb6e604d306f633dfac0`

## 结论与范围

消费者现在通过目标框架公开的 typed capability seam，把既有 SQLite `TransactionFactory` 显式提供给生成的 `delivery/management` Application 依赖。protobuf application declaration 是 capability 要求的唯一合同源；`manifest.json`、`assembly-plan.json`、protobuf Go 文件与 Assembly Go 文件均由固定 target generator 重建，没有手改生成物。

当前 `module.yunka.json` schema 尚不能表达 `Descriptor.Provides`，因此本任务按目标框架文档允许的方式，在消费者 `internal/localtx` 增加最小手写 descriptor wrapper，并经 `BootstrapOptions.AdditionalModules` 注入。wrapper 只捕获已经构造的 factory，不执行 I/O、不查找运行时服务、不注册全局状态。

生产装配只创建一次 `localtx.SQLiteFactory`：同一对象既由 capability provider 导出，也交给唯一 root `operation.Executor` 管理事务。生成 Assembly 在 bootstrap capability snapshot 中按静态的 package/type/name 三元组解析依赖并传入 Application factory；业务运行期不持有 `CapabilitySet`，没有 service locator，也没有第二个事务执行器。

本任务没有迁移 SQLite 生命周期，没有改 Outbox、notification、projection、runtimehost 结构或业务合同；这些分别留给 YU-05/YU-06 及后续任务。未修改 `third_party/yunka/**`、旧 `backend/**` 或 `web/**`。

## 公开 seam 与所有权

| 路径 | ownership | 作用 |
| --- | --- | --- |
| `contracts/proto/iot_delivery.proto:15-22` | `developer-contract/editable` | 声明 `sqlite.transaction-factory` typed capability |
| `internal/localtx/capability.go:15-55` | `developer-code/editable` | 定义消费者 typed contract 与手写 `Provides` wrapper |
| `internal/bootstrap/application.go:276-284` | `developer-code/editable` | 单次构造 factory，并交给 root Executor |
| `internal/bootstrap/application.go:386-394` | `developer-code/editable` | 通过 `AdditionalModules` 注入同一 factory 的 descriptor |
| `contracts/delivery/v1/iot_delivery.pb.go` | `protobuf-go-generator/generated-only` | protobuf application option 派生物 |
| `contracts/generated/manifest.json` | `yunka-generator/generated-only` | capability 合同 manifest |
| `contracts/generated/assembly-plan.json` | `yunka-generator/generated-only` | Application capability 装配计划 |
| `internal/assembly/zz_yunka_assembly_gen.go` | `yunka-generator/generated-only` | 生成 typed dependency 字段、解析与注入 |

目标 `yunka ownership inspect` 已逐项返回上述分类。没有改 `modules/delivery/zz_yunka_module_gen.go`，也没有将 provider 冒充为可由当前 module manifest 生成的 managed module。

## RED → GREEN

### RED

先在 YU-03 parent 上新增 `TestApplicationFactoryRejectsMissingSQLiteTransactionCapability`，未改生产实现时真实运行：

```text
--- FAIL: TestApplicationFactoryRejectsMissingSQLiteTransactionCapability
application factory accepted missing SQLite transaction capability: application=*application.Adapter
FAIL github.com/hvritual/iot-delivery-system/backend-yunka/internal/bootstrap
```

这是缺失 typed dependency 仍被接受的行为失败。随后编写集成测试时曾把目标 API 字段误写成旧形状，以及把 `CapabilitySet` 误传为 `nil`；两次都是测试编译错误，均未计作 RED。

### GREEN

- 生成 Assembly 对 capability 缺失失败关闭；错误保留 `sqlite.transaction-factory` 上下文。
- Application factory 再次防御空 typed dependency。
- provider descriptor 拒绝空 factory。
- 真实 SQLite 内存数据库测试从 capability snapshot 解析 factory，root operation 开启一次事务，声明的 child operation 加入同一事务句柄；child 写入后返回错误，root 回滚，表中副作用为零。
- 计数 wrapper 断言整次 root/child 执行只调用一次 `Begin`。

定向 GREEN：

```text
GOWORK=off go test ./internal/localtx ./internal/bootstrap \
  -run 'TestCapabilityProvidesOneRootSQLiteUnitOfWorkAndRollsBackJoinedChild|TestCapabilityDescriptorRejectsMissingFactory|TestGeneratedAssemblyRejectsMissingSQLiteTransactionCapability|TestApplicationFactoryRejectsMissingSQLiteTransactionCapability' \
  -count=1
PASS
```

## Canonical 生成与确定性

使用固定 Go 1.25.13、Yunka `057ebcf...`、protoc 21.12 和 YU-02 固化的插件运行两轮：

```text
make yunka-generate
make yunka-check
make yunka-generate
make yunka-check
```

每轮结果均为 protobuf-Go `2` files、providers `0` bindings、contract `1` service / `35` messages / `5` application files、modules checked、assembly `1` binding，full check 全部为 `OK`。两轮前后 4 个实际变化的派生文件摘要逐字节相同：

| 派生文件 | SHA-256 |
| --- | --- |
| `contracts/delivery/v1/iot_delivery.pb.go` | `397f660d26b7ea73e4d31ad23b9caae079c52b88112add9d09c91b8eae43edf9` |
| `contracts/generated/assembly-plan.json` | `e9bd010c812025bed25b4428df814eee70b157b82fdfa22dccf0d4bac308100f` |
| `contracts/generated/manifest.json` | `2f2515c21680d8d5bf43e69a0c4e342380696d856d6b2f1ac603f3a4e8bfe99a` |
| `internal/assembly/zz_yunka_assembly_gen.go` | `67ada5aa9055c5a5fcf045c06c46d445fe726ed670ac351c1280070d5cb3b78a` |

排序后的该摘要清单自身 SHA-256 为 `96fd2c9fb42d5ef3e46c18e710da9f21a37d07b8261bd0176989b23b46ecfea3`。

## 完整回归

以下均在 `backend-yunka/` 模块边界、`GOWORK=off` 下执行：

```text
go mod tidy -diff                    PASS
go test ./... -count=1               PASS
go vet ./...                         PASS
go test -race ./... -count=1         PASS
make yunka-check                     PASS
git diff --check                     PASS
```

当前无 `--base` 的 `yunka audit --format agent-json` 仍只返回 YU-03 已记录的稳定 finding：

```text
AUDIT-AUTH-001:internal/delivery/application/adapter.go:github.com/hvritual/yunka.io/gateway/authz
```

本任务没有修改该文件，也没有新增 audit finding。YU-03 已确认的 monorepo 子目录 `audit --base` 问题仍采用 parent/current 无 base finding ID 对照，不把绕过称作框架修复。

## ChangeSet 与框架问题判断

曾尝试以 `delivery.items.create` 启动 ChangeSet 并纳入 `internal/localtx/capability.go`、bootstrap 等跨切面路径；目标 CLI 按 AX7 单 operation canonical scope 返回 `YUNKA-DX-CHANGE-003`，指出 localtx 路径不属于该 operation 的可编辑范围。该结果符合固定框架的控制面边界，不是行为 RED，也不是框架缺陷；本任务没有强行扩大单 operation ChangeSet、没有伪造计划文件。

本任务未发现新的已证实框架问题。当前 module manifest 缺少 `Provides` 表达能力是固定文档已声明且提供 handwritten descriptor 公开绕过的已知能力边界；绕过仅完成消费者接入，不宣称框架能力已修复。

## 残余限制与下一任务

- SQLite 数据库的 App-owned lifecycle/health 仍沿用既有 runtime component；本任务只迁移 transaction factory 的 typed construction dependency。
- Outbox、notification 与 projection 仍未迁移为 typed capability；这是 YU-05 的明确范围。
- YU-06 才重组 generated Assembly/runtimehost/HTTP/gRPC 装配并关闭旧 module-identity 路径。
- 根 `go.work` 仍包含只作对照的旧 `backend/`，模块级回归不声称旧 backend 通过。

下一独立任务应为 **YU-05「Outbox、notification、projection typed capability 与 lifecycle 接入」**。它必须以本任务同步后的远端精确提交为 parent，只处理 App-owned capability/lifecycle、start/health/reverse shutdown 与 request-state 泄漏验证；不得顺带进入 YU-06 runtime 装配重组。
