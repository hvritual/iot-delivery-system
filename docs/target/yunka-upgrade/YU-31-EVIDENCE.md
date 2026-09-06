# YU-31 runtime, transport and closure certification

- Fixed consumer parent: `0501bd4b2295c624b817e28a94eb1f62b08b0d4c`.
- Fixed framework: `057ebcf88a87303eb633eb6e604d306f633dfac0`.
- Task branch: `codex/yu-31-runtime-smoke-closure`.
- Status of this initial commit: executable certification authored; not yet run.
- Scope stops before YU-32; no deployment or framework edits.

## Basis

YU-30 passed the preceding exact-SHA CI. Existing runtime diagnostics and listener
closure tests use development API-key fixtures. YU-29 exercises real browser
identity but does not certify the actual gRPC and stdio MCP processes, EOF/signals,
failed process startup and reaping together. This is a certification coverage gap,
not a claim that a runtime or framework defect has already been reproduced.

## Execution contract

`bash backend-yunka/scripts/run-yu31-smoke.sh` builds the existing production entry
points plus the existing YU-29 account fixture with the race detector, then runs
`go test -race -tags=yu31 -count=1 -timeout=4m -v ./tests/runtime` on Linux.
All required executables must exist; there is no skipped-prerequisite PASS.
Normal YU-30 full regression remains separately required on the same SHA.

The test starts actual `yunka-bootstrap` and `iot-delivery-mcp` executables. HTTP
and gRPC use loopback TCP; MCP uses newline-delimited JSON-RPC over actual stdio.
No transport mock, bufconn, preconstructed Principal, development API-key, or
handwritten User/RoleBinding SQL grant is used. The only direct fixture identity
write is the organization prerequisite already owned by the YU-29 fixture.

Both accounts are created through AdministratorBootstrap and MemberAdministration.
Login/current and project-role/reset commands use the existing local BFF. MCP is
still development-only as a deployment surface; its accounts and grants are real
local-member SQLite identities. No production remote-MCP claim is made.

## Assertions

Health/diagnostics must report ready, healthy capabilities and event runtime, and
complete HTTP/gRPC lifecycle inventory. All three transports must agree on absent,
assigned, scoped, revoked and invalid-credential outcomes against one SQLite file.
An HTTP-created work item must have the identical ID/revision through gRPC and MCP.
Outbox publication, audit and Obsidian projection must be observed before closure.
EOF and SIGTERM close real MCP processes; SIGTERM/SIGINT close the runtime. Each
child must be reaped, its Linux process group absent, and its listeners reusable.
The same addresses/database are restarted and committed identity/object state
reread. Occupied-listener startup must fail without a retained process/listener.
Production stdio MCP must remain rejected. Process logs must not contain fixture
passwords, JWT keys, bearer tokens or opaque session values.

Artifacts contain command output, exact source/framework IDs and worktree drift,
never the secret manifest, SQLite file or process environment. No screenshot/UI
claim is made by this wire/process smoke; browser regression remains YU-29/30.

## Retained boundary

The YU-30 existing `AUDIT-AUTH-001` consumer debt remains for YU-32. This task is
runtime certification, not independent architecture/security adjudication.
