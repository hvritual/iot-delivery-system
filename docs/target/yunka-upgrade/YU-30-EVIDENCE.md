# YU-30 full regression evidence

> Document class: **EVIDENCE**
> Fixed consumer parent: `f66019e605affe1eedbf20801fab4f22a0903621`
> Task branch: `codex/yu-30-full-regression`
> Fixed Yunka: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before YU-31; no deployment

## Authoritative execution result

The implemented source at `2f533dc233584c3318b9788f7016dcfb131b99b7` passed all four required jobs in GitHub Actions run **33971774693**, attempt 1, completed on 2026-09-05. This replaces the earlier environment-blocked status; it is not a claim that the restricted assistant shell ran the tests.

Run: https://github.com/hvritual/iot-delivery-system/actions/runs/33971774693

`YU-30-CI-RECEIPT.json` binds the exact consumer SHA, framework SHA, job IDs, artifact IDs and SHA-256 digests. All four archives were downloaded through the connected artifact API and their bytes were independently hash-checked. Their `head.txt` values match the tested SHA, and all `worktree.txt` / `worktree.patch` files are empty.

The documentation closeout is a separate commit. It must obtain its own exact-SHA required-job success before non-force merge. This receipt certifies its named source commit, not an unknown future commit. TASKS dispatch advances only after that gate and main/branch readback.

## Executed matrix

| Gate | Observed result |
| --- | --- |
| Exact checkout, pinned framework and toolchain | PASS on GitHub-hosted Ubuntu 24.04 |
| Canonical generate/check pass 1 and pass 2 | PASS; zero worktree drift after each |
| Ownership check of changed handwritten Go | PASS |
| Generated ownership inventory | 15 files; every file generator-only and not safe-auto-edit |
| Yunka audit JSON debt delta | 0 new proven debts; 1 existing debt retained, not zero findings |
| Real ChangeSet CLI | Positive conformant; exact scope probe rejected; restored conformant |
| Application authorization / audit / transaction / no-bypass packages | PASS |
| `go mod tidy -diff` | Exit 0; no committed dependency drift |
| `go test ./...` | PASS |
| `go vet ./...` | Exit 0 |
| `go test -race ./...` | PASS |
| Frontend `npm test` | 22 files / 74 tests passed |
| Frontend typecheck / production build | Exit 0 / exit 0 |
| `npm audit --audit-level=high` | Exit 0; that run reported zero vulnerabilities |
| `npm run e2e:yu29` | Real browser PASS in canonical-full and independent browser-e2e jobs |
| Final whitespace and worktree drift | PASS; empty worktree evidence |

Toolchain: Go 1.25.13, Node 22.16.0, protoc 3.21.12, protoc-gen-go 1.36.11, protoc-gen-go-grpc 1.6.2. Browser evidence records Google Chrome 152.0.7977.64. No compiler, typecheck, race, authentication or authorization gate was disabled.

## Real RED and repair provenance

The first CI run, **33969280959** at `029df4c1fa30d081667fe79790119d81bd477e8a`, checked out both repositories and installed the required toolchain. It then exposed real failures rather than a missing-container hypothesis. Subsequent repairs and reruns are retained in branch history.

- Framework CLI composition: globally disabling workspace selection resolved the public pkg tag rather than the pinned framework workspace and produced missing diagnostic symbols. The consumer Makefile now explicitly selects `third_party/yunka/go.work`; consumer test processes still use `GOWORK=off`. Framework source was not edited.
- Go compilation, identity fixtures and source guards: small consumer fixes restored compile/type alignment, current durable permission/session fixtures and fail-closed no-bypass checks. Complete packages and race/vet were rerun; no empty regex match is counted as a gate.
- Frontend: the real run found 4 failing auth-shell tests and BodyInit/mock-signature type errors. Component semantics, test cleanup and request-body types were repaired; the final 74-test suite and production build pass.
- Dependency resolution: real Go module resolution and the npm lockfile were committed. The original npm installation reported high-severity findings; the updated dependency graph reports zero in the recorded final audit. This is a dated audit result, not a permanent security claim.
- Browser certification: cross-context CSRF now holds reviewer identity, grant, request path, body and revision constant while changing only the CSRF token. The wrong token is rejected with unchanged business state; the correct reviewer token commits exactly one revision. A role-denied administrator call is not used to prove CSRF protection.
- Next type generation: `next-env.d.ts` is mode-specific generated output. It is ignored rather than hand-maintained; strict typecheck first runs `next typegen`. No TypeScript check is skipped.

## Governance limits and retained improvements

**Existing consumer audit debt:** `AUDIT-AUTH-001` at `backend-yunka/internal/delivery/application/adapter.go` identifies a direct import of `github.com/hvritual/yunka.io/gateway/authz` in the application domain. The raw report classifies it as an existing proven violation relative to the fixed parent. It is neither fixed nor a Yunka defect in this round. The current gate is no-new-debt; YU-32 must review the boundary and close or explicitly adjudicate this debt with evidence. Do not describe YU-30 as zero-debt or final security certification.

**ChangeSet scope:** the actual CLI checks all current generated canonical operations at the tested HEAD, plus an exact negative out-of-scope file and restoration. It does not retrospectively certify every historical YU-01..29 edit or unrelated frontend/CI changes. Those paths retain their own diff and executable gates.

**Evidence retention:** Actions command artifacts currently expire after seven days and offline source after one day. Receipt hashes and run IDs make provenance explicit but do not replace artifact bytes. Long-lived evidence retention and permanent merge enforcement remain improvement candidates; this round performs an explicit exact-SHA gate, not a repository-ruleset installation.

## Environment and scope

The assistant shell's direct GitHub DNS restriction still exists. GitHub Actions provides the real execution environment; checksum-verified Git bundles provide offline source access. This resolves the workflow dependency on the restricted shell without pretending to repair its network or installing a lower toolchain. See `YU-30-ENVIRONMENT.md`.

No Yunka framework source, framework gitlink, protobuf or generated business artifact was changed by the repairs. No new framework defect was reproduced and no framework Issue was created. The retained audit debt is consumer-side. YU-31 runtime smoke/closure and YU-32 independent review have not been executed by this round.

## Earlier blocked attempt (historical, superseded)

At branch `b8138628574ff52b713a34e3680b0068c9daf5e6`, the restricted shell lacked a checkout and the pinned toolchain, and GitHub DNS lookup failed. Its runner was authored but not executed; that was an environment blocker, not product RED. The previous NOT RUN matrix applies only to that earlier attempt. It must not override the later exact-SHA CI results above.
