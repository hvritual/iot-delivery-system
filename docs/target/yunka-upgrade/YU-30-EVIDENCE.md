# YU-30 full regression evidence

> Document class: **EVIDENCE**  
> Task: `YU-30`  
> Fixed consumer parent: `f66019e605affe1eedbf20801fab4f22a0903621`  
> Task branch: `codex/yu-30-full-regression`  
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`  
> Scope stop: before `YU-31`

## Result

YU-30 is **not GREEN and not certified in the current execution environment**.

This task is an execution/certification task rather than a feature-authoring task. The current tool container cannot materialize the fixed canonical repository checkout, so none of the required generator, Go, frontend, browser E2E, ownership, audit, ChangeSet or no-bypass commands can truthfully be reported as passing.

The round therefore adds one deterministic consumer regression entry point:

```text
backend-yunka/scripts/run-yu30-regression.sh
```

but deliberately does **not** advance `TASKS.md` to YU-31 and does **not** treat the runner's existence as execution evidence.

## Fixed-parent RED review

### Candidate A — the canonical tree has already failed a YU-30 product gate

Not established.

YU-30 accepts only a real command failure, generator drift, `yunka check --full` failure, Go/frontend/E2E test failure, or ownership/audit/ChangeSet/no-bypass gate failure as RED.

No such command was able to execute against the canonical fixed-parent tree in this environment. Therefore this round does not manufacture a consumer RED from missing checkout/network/toolchain materialization.

### Confirmed execution blocker — canonical source is unavailable to the local toolchain

The execution container was inspected before writing any product fix.

Available tools include:

```text
Node      v22.16.0
npm       10.9.2
Go        go1.23.2 linux/amd64
Chromium  140.0.7339.80
```

No `iot-delivery-system` checkout exists in the accessible workspace.

A direct repository probe returned:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
fatal: unable to access 'https://github.com/hvritual/iot-delivery-system.git/': Could not resolve host: github.com
```

Additional direct-IP / external DNS bypass attempts did not provide usable GitHub transport. Historical pinned tool paths from the earlier YU-03 execution workspace are also absent from the current container.

This is an **environment blocker**, not product RED.

## Canonical toolchain contract reread

The fixed-parent `backend-yunka/Makefile` remains authoritative for generation/check execution.

It requires:

```text
Yunka gitlink          057ebcf88a87303eb633eb6e604d306f633dfac0
Go                     go1.25.13
protoc                  libprotoc 3.21.12
protoc-gen-go           v1.36.11
protoc-gen-go-grpc      1.6.2
```

and exposes:

```text
make yunka-toolchain-check
make yunka-generate
make yunka-check
```

The current container's Go `1.23.2` is therefore not accepted as a substitute for the reviewed Go `1.25.13` generation/check workflow.

The fixed-parent GitHub tree was also reread through the repository API and confirms:

```text
third_party/yunka -> 057ebcf88a87303eb633eb6e604d306f633dfac0
```

with the canonical generated artifacts, authorization tests, audit tests, ownership tests, no-bypass tests and YU-29 E2E harness present in the tree.

Static existence is inventory evidence only; it is not execution PASS evidence.

## Canonical YU-30 regression runner

`backend-yunka/scripts/run-yu30-regression.sh` centralizes the required execution sequence without weakening any existing gate.

The runner first requires:

```text
HEAD descends from fixed YU-29 parent
third_party/yunka has the exact fixed gitlink
repository worktree is clean
make yunka-toolchain-check succeeds
```

It then runs the canonical generator/check twice:

```text
make yunka-generate
make yunka-check
clean-tree assertion

make yunka-generate
make yunka-check
clean-tree assertion
```

A first-pass generated drift is therefore a real failure rather than being silently overwritten. The second pass proves repeatability only after the committed tree is already canonical.

### Focused governance packages

The runner executes complete packages rather than using permissive `go test -run <regex>` filters:

```text
go test .
go test ./internal/delivery
go test ./internal/audit
go test ./internal/bffhttp
go test ./internal/httpapi
go test ./internal/localbffhttp
```

This avoids the Go behavior where a `-run` expression matching zero tests may still return exit status zero.

These packages carry the current consumer gates for:

- root no-bypass / permission / credential-leak contracts;
- project ownership and generated-operation equivalence;
- transaction / Outbox and segregation-of-duties regressions;
- audit acceptance/denial/failure/rollback/redaction behavior;
- REST execution-boundary / local-BFF no-bypass behavior.

### Full Go regression

Using the same Go binary that passed `yunka-toolchain-check`, with `GOWORK=off`:

```text
go mod tidy -diff
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
```

### Frontend and real browser regression

The runner then executes:

```text
cd web
npm ci
npm test
npm run typecheck
npm run build
npm run e2e:yu29
```

`npm run e2e:yu29` remains the real two-account/two-browser-context Next + Yunka runtime + SQLite certification authored in YU-29; it is not replaced with a component mock.

The Go toolchain directory is prepended to `PATH` before E2E so the YU-29 fixture/runtime builds inherit the same reviewed Go binary instead of accidentally using a different system Go.

Finally the runner requires:

```text
git diff --check
clean repository worktree
```

and prints `YU-30 REGRESSION PASS` only after every preceding process exits successfully.

## Current execution matrix

| Gate | Status | Reason |
| --- | --- | --- |
| canonical checkout at fixed parent | BLOCKED | source not materialized; GitHub DNS/egress unavailable to local shell |
| `make yunka-toolchain-check` | NOT RUN | no checkout; current system Go is also not canonical 1.25.13 |
| generate/check pass 1 | NOT RUN | prerequisite blocked |
| generate/check pass 2 / zero drift | NOT RUN | prerequisite blocked |
| ownership gates | NOT RUN | prerequisite blocked |
| audit gates | NOT RUN | prerequisite blocked |
| ChangeSet/generated-equivalence gates | NOT RUN | prerequisite blocked |
| no-bypass gates | NOT RUN | prerequisite blocked |
| `go mod tidy -diff` | NOT RUN | prerequisite blocked |
| `go test ./...` | NOT RUN | prerequisite blocked |
| `go vet ./...` | NOT RUN | prerequisite blocked |
| `go test -race ./...` | NOT RUN | prerequisite blocked |
| `npm ci` | NOT RUN | no checkout |
| `npm test` | NOT RUN | no checkout |
| `npm run typecheck` | NOT RUN | no checkout |
| `npm run build` | NOT RUN | no checkout |
| `npm run e2e:yu29` | NOT RUN | no checkout |
| final generated/worktree drift | NOT RUN | prerequisite blocked |

No row above is reported as PASS.

## Historical evidence is not substituted for YU-30

Earlier YU-03 evidence records a real environment where the pinned Go/toolchain successfully executed canonical generation/check and Go test/race/vet for the then-current tree.

Later YU-15/YU-16 evidence records the authored transaction/audit regression surfaces.

Those artifacts establish provenance for the gates, but they do **not** certify the YU-29 fixed-parent tree and are not reused as a YU-30 PASS.

## Framework disposition

No new Yunka framework defect is reproduced in this round.

The inability to execute is environmental, and an unexecuted hypothesis is not sufficient evidence for a framework Issue. The fixed framework remains unmodified at:

```text
057ebcf88a87303eb633eb6e604d306f633dfac0
```

No framework Issue is created.

## Completion boundary

YU-30 remains the active task until the canonical runner can execute against a materialized checkout with the required pinned toolchain and all gates pass (or a real failing gate is reproduced and minimally repaired).

Accordingly this branch intentionally:

- does not advance `TASKS.md` to YU-31;
- does not claim YU-30 GREEN;
- does not start YU-31 runtime smoke/closure work;
- does not deploy.
