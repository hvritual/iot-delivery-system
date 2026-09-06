# YU-31 runtime, transport and closure certification

- Fixed consumer parent: `0501bd4b2295c624b817e28a94eb1f62b08b0d4c`.
- Fixed framework: `057ebcf88a87303eb633eb6e604d306f633dfac0`.
- Task branch: `codex/yu-31-runtime-smoke-closure`.
- Scope stops before YU-32; no deployment.

## Executed source and final-commit gate

The repaired source `9d552633e2b50110d3a74b4d1d3c0b02f4099bb4` passed the real process/transport test in Actions run `34002432487`, job `101403572616`, on 2026-09-06. The archive bytes and exact `head.txt` were independently verified; its final worktree evidence is empty. `YU-31-CI-RECEIPT.json` records the immutable source and evidence identities.

The closeout additionally tightens failure-reason, shutdown-log and SQLite checkpoint-result assertions. The final commit must independently pass BOTH the YU-31 runtime workflow and all four YU-30 regression jobs on its own exact SHA before non-force merge. A prior source's successful run cannot certify those additional changes. A staged YU-32 dispatch is not permission to start it before this gate.

## Real RED: local-member MCP unnecessarily required a development API key

The initial runtime run `34002054447` at `ab85026e15186e9101d38810b4a0168cb1b2ec7c` established a healthy production HTTP/gRPC runtime and real member accounts, but the separate stdio MCP process exited before initialize returned. There was no development API key: MCP had explicitly selected a real local-member opaque session.

Inspection identified unconditional `localauth.FromEnvironment()` whenever the bootstrap environment was development. That constructor rejects an absent legacy key with `local API key environment is required`. Thus deployment mode, not the selected credential family, accidentally made the old identity mechanism mandatory.

This root cause was reproduced independently in run `34002387655`: the new targeted bootstrap test was run with the parent's `application.go` and failed for the exact missing-key error; restoring only the reviewed repair made the same test pass. The red/green artifact is retained. This is a consumer composition defect, not an environmental limitation or a Yunka defect.

The minimal product repair is confined to `internal/bootstrap/application.go`: construct the compatibility authenticator only when legacy configuration actually exists; enable BFF legacy fallback only when that authenticator exists. Explicitly configured malformed keys still fail. Existing API-key compatibility tests remain required. Missing credentials and unconfigured API keys still receive HTTP 401; no anonymous or forged Principal is created.

## Real transport and lifecycle coverage

`bash backend-yunka/scripts/run-yu31-smoke.sh` builds the existing `yunka-bootstrap`, `iot-delivery-mcp` and `yu29-fixture` executables with Go 1.25.13 and `-race`, then runs the Linux-tagged real-process test with `-race -tags=yu31`. Missing prerequisites fail rather than skip. All process groups and temporary directories belong to the harness.

| Assertion | Evidence required |
| --- | --- |
| Health/diagnostics | Ready core, healthy capabilities/event runtime, HTTP/gRPC lifecycle inventory |
| Independent identities | Real bootstrap administrator and application-created member; separate server sessions |
| Three transports | Real loopback HTTP, real TCP gRPC, newline JSON-RPC over actual stdio MCP processes |
| Durable project scope | Absent grant denies; assignment admits only the bound project; unbound project stays invisible |
| Durable mutation | HTTP-created item has the exact same ID/revision through gRPC and MCP |
| Background delivery | SQLite success audit present; Outbox published; Obsidian work-item projection materialized |
| Revocation | Existing MCP connection rereads grants; role revoke removes authorization without logging out |
| Credential reset | Old member JWT and opaque session fail on all three transports; administrator stays valid |
| Graceful closure | MCP EOF/SIGTERM and runtime SIGTERM/SIGINT exit successfully, without shutdown-error logs |
| Process/resource release | Wait/reap completed, process group and /proc child entry absent, all six known listeners reusable |
| Persistence | Same addresses/database restart with committed item and identity validity intact |
| SQLite closure | Read all checkpoint result columns and require not-busy; query success alone is insufficient |
| Failed startup | Deliberately occupied listener fails with the expected bind reason; no orphaned listeners/processes |
| Deployment restriction | Production stdio MCP fails for the explicit development-only policy |
| Log hygiene | Captured process stderr contains none of the generated passwords, keys, JWTs or opaque sessions |

No fake transport, bufconn, prebuilt Principal, development local-admin credential, or direct User/RoleBinding SQL seed is used. The only identity fixture SQL creates the organization prerequisite already owned by the YU-29 fixture. Observer SQL reads durable outcomes; checkpointing does not manufacture application facts.

MCP's development-only deployment setting is not a development identity: both MCP processes authenticate real local members and resolve current durable grants from the same SQLite file. This is not a claim of production remote-MCP support. Loopback plaintext transport smoke is not a TLS deployment certification. The prior browser E2E remains a separate required job.

## Evidence and scope controls

Normal regression workflows are read-only and pin tool/action/source identities. Artifacts contain command logs, source IDs and drift, not the private account manifest, database, process environment or tokens. Authentic local Git commits are transferred as hash-verified bundles; temporary writer workflows stay on an isolated transfer branch and are removed after use. No automated writer is added to main. The restricted local shell's DNS limitation is not represented as repaired; CI owns executable certification.

The final test contract checks semantic outcomes rather than arbitrary errors: startup failures must have the exact expected reason, all successful exit paths inspect shutdown logs, and SQLite checkpoint busy status is read from its result row. SQLite reference: https://www.sqlite.org/pragma.html#pragma_wal_checkpoint .

The full YU-30 suite is required on the same final commit: double generation/full check, actual ownership/audit/ChangeSet controls, module consistency, Go test/race/vet, frontend tests/typecheck/build/security audit and real browser E2E. No generated/protobuf file, framework source/gitlink, dependency or Web product code changes are part of YU-31.

## Retained limitations

`AUDIT-AUTH-001` remains an existing consumer architecture debt for YU-32; passing smoke is not its adjudication or a zero-debt claim. This Linux CI smoke proves the exercised scenarios, not Windows/macOS lifecycle behavior, sustained load, crash recovery under power failure or every possible goroutine/resource leak. YU-32 independent security/architecture review and YU-33 final operating documentation remain separate tasks.

No new framework defect was reproduced; no framework Issue was created. The optional-key failure and its repair are consumer-owned.
