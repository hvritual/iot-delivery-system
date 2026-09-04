# S0-04-09 Stage 0 local end-to-end hard gate

## One-command local evidence

From `backend-yunka`, run:

```powershell
& .\scripts\run-s0-04-09-hard-gate.ps1
```

The script fails fast and runs the focused identity, authorization, audit,
configuration, concurrency, and SQLite durability proofs before the full Go
suite, `go vet`, and the pinned Yunka contract check. Before that check it
fails closed unless both the repository `HEAD` gitlink and the clean,
materialized submodule `HEAD` are exactly
`9a51562aa7bcef42f6861bd91abd30aae13ed6ef`. It then resolves and exports the
pinned `.tools/bin/protoc-gen-go.exe` and
`.tools/bin/protoc-gen-go-grpc.exe` as `PROTOC_GEN_GO` and
`PROTOC_GEN_GO_GRPC`, does not rely on PATH, and restores either caller value
in `finally`. `-VerifyOnly` runs the lock/tool/environment safety checks
without starting tests. The script invokes existing tests; it does not
recreate production logic in a script.

## Local coverage matrix

| Hard gate | Executable local proof |
| --- | --- |
| Production authorization | `configuredAuthorization(... RuntimeEnvironmentProduction ...)` in the bootstrap matrix uses SQLite human and service identities, the composed GrantResolver, and OperationGuard. It proves allow, default denial, project-scope denial, live revocation, stable REST/gRPC/MCP categories, and no business/Outbox write for denial. |
| Trusted identity | BFF assertion tests prove a verified human assertion is persisted as a trusted identity without assertion contents; rejected assertions are anonymous and leave no application mutation. Service credential tests create a service Principal only after local credential verification. |
| Audit | The recording-executor tests cover denial and post-rollback audit side effects. Configuration tests prove actor, operation, organization scope, target, diff paths, trace, request and correlation fields while the sanitizer tests reject or redact credential material. Service revocation proves the revocation state and its audit event commit together. |
| Configuration | Change, compare, and rollback use real immutable SQLite revisions through OperationPlan/Executor/UoW. Committed change and rollback each stage one Outbox event whose parsed envelope contains only organization, kind, key, revision and rollback-source metadata; the test proves a sensitive configuration-payload sentinel is absent. Conflict, denial, audit failure, and Outbox failure leave revision, audit-success, and Outbox counts unchanged. |
| Concurrent revision writes | The production matrix forces each REST, gRPC, and MCP pair to overlap at the real operation boundary. Exactly one expected-revision update commits, the loser receives `revision_conflict`, and SQLite, Outbox, and successful audit counts each reflect only the winner. |
| Durability | The configuration SQLite test closes and reopens the same database file and reads the immutable revision history again. |

## Boundary of this evidence

Passing this gate means **local Stage 0 hard-gate evidence passed**. Its SQLite
database, BFF assertions, gRPC bufconn, and MCP in-memory transport are local
and hermetic. It does not prove a deployed production environment, a real
external OIDC provider, a production database, Vault, network authentication,
operations monitoring, backup/recovery, or deployment readiness.
