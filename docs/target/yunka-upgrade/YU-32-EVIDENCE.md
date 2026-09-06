# YU-32 independent security / architecture review evidence

> The original review below is historical as of `5e57424b034ae8da0ac27d6fb920b457005ea253`. The local-password retained risk was subsequently remediated under the user-authorized YU-32H follow-up; see the dated follow-up section below for current status and its exact-source evidence.

- Fixed consumer parent: `20520f69d7eaf3c27c0fb3e9d79f03b4ecb059bd`.
- Fixed framework: `057ebcf88a87303eb633eb6e604d306f633dfac0`.
- Task branch: `codex/yu-32-independent-security-architecture-review`.
- Scope stops before YU-33; no deployment.

## Review basis and limitation

This review uses the canonical YU-30/YU-31 source tree, exact-SHA CI/runtime receipts, current source, and the executable Yunka audit. The private YU-00F `FRAMEWORK-REQUIREMENTS.md`, `FRAMEWORK-PROBLEMS.md`, and evidence manifest are not present in the public consumer repository or current verified checkout. Repository search also returns no copy. They are therefore recorded as an unavailable input rather than reconstructed from memory. The fixed framework version and all consumer evidence that is actually present remain in scope.

The review method is candidate -> falsification / counter-evidence -> confirmed, refuted, or retained-risk. A source pattern is not called a defect solely because it looks unusual; successful historical tests are not reused when current source changes.

## Candidate disposition

| Candidate | Status | Evidence / decision |
| --- | --- | --- |
| `AUDIT-AUTH-001`: delivery Application imports Yunka `gateway/authz` | **CONFIRMED -> REPAIRED** | The fixed parent imports `github.com/hvritual/yunka.io/gateway/authz` from `internal/delivery/application/adapter.go` and constructs `authz.Denied` for SOD errors. This is exactly the framework audit invariant: authorization implementation belongs at the root security boundary. The repair moves SOD error classification to `internal/deliveryauthz`, preserves the domain sentinel, removes the direct Application import, and adds a source-level regression. Executed audit now reports no current existing/new proven debt and classifies this item as fixed. |
| Cross-domain repository / framework-platform bypass in handwritten Applications | **REFUTED** | YU-30 audit had no `AUDIT-APP-001` or `AUDIT-INFRA-001`; source review found no transport persistence access. The repaired exact-SHA audit was rerun and again reported no current existing/new proven debt. |
| JWT human authorization trusts `Principal.Roles` | **REFUTED** | `principalauthz.Resolver` sends JWT principals only to `humanauthz.Resolver`; that resolver explicitly ignores `Principal.Roles` and rereads active Organization/User/RoleBinding/Permission facts from SQLite on every request. Development API-key roles are a separate AuthMethod path. |
| OIDC identities merge by email/display name | **REFUTED** | `identitybinding.Resolver` resolves the durable external identity by exact verified issuer+subject. Email/display name are profile snapshots only; absence provisions a distinct canonical User rather than matching an existing profile. |
| HTTP/BFF/MCP transports persist business facts directly | **REFUTED** | Production source under `httpapi`, `bffhttp`, `localbffhttp`, `mcpserver`, and delivery transports contains no direct `database/sql`/Exec/Query persistence path. YU-30 no-bypass and transaction packages remain part of the executable gate. |
| Cross-tenant/project RoleBinding or project access can bypass durable scope | **REFUTED** | YU-24 guards validate tenant, Project ownership, active User and project binding contract; YU-31 real HTTP/gRPC/MCP smoke re-proved absent grant denial, one-project assignment visibility, unbound-project invisibility and immediate revocation. |
| Role revoke / disable / credential reset leaves stale local authorization | **REFUTED** | Durable human grants are reread per request; YU-31 exact runtime evidence proves role revoke removes authorization on an existing MCP connection while authentication remains valid, and credential reset invalidates the old JWT/session on all three transports. |
| Cross-browser CSRF can be replayed between authenticated contexts | **REFUTED** | YU-29/YU-30 browser certification holds reviewer identity, grant, path, body and revision constant while changing only the CSRF value: another context's token is rejected with zero mutation, then the correct token commits exactly one revision. |
| Runtime shutdown leaves listeners/processes or persistence in an ambiguous state | **REFUTED for exercised Linux scenarios** | YU-31 exact runtime smoke checks reaping, process groups, listener reuse, restart persistence, SQLite checkpoint not-busy and exact failed-start bind reason. This is not generalized to every OS/crash/power-loss scenario. |
| Local password login has explicit online-guessing throttling / password-minimum policy | **RETAINED SECURITY HARDENING RISK** | Current `locallogin` performs constant-work synthetic verification and security audit, but no request/account rate limiter or lockout state exists; credential policy accepts any non-empty password up to 4096 bytes. This is consumer-owned. No current YU-18..31 acceptance criterion defines a minimum password length, lockout semantics, proxy/IP trust model or multi-instance limiter, so YU-32 does not invent those product rules. YU-33 must document this residual risk and required production control rather than calling the system fully hardened. |

## Confirmed debt repair: `AUDIT-AUTH-001`

The fixed parent behavior is not a false positive. `delivery/application/adapter.go` directly depended on Yunka's gateway authorization implementation only to normalize these delivery domain denials:

- `ErrProductionPrincipalRequired`;
- `ErrImplementationSourceRequired`;
- `ErrImplementerCannotVerifyOwnChange`.

Those are domain segregation-of-duties invariants, but the transport-stable permission-denied envelope is an authorization-boundary concern. The repair therefore:

1. removes the Application import of `gateway/authz`;
2. moves the normalization into `deliveryauthz.NormalizeDomainDenial`;
3. preserves `errors.Is(..., delivery sentinel)` and `authz.IsDenied(...)` behavior;
4. adds `TestYU32ApplicationPackagesDoNotImportGatewayAuthorization` so a future direct import fails ordinary Go tests;
5. upgrades the canonical Yunka governance gate from “zero new debt” to “zero current existing + zero new proven debt”.

This is a root-boundary correction, not an audit suppression. No Yunka source or audit rule is changed.

## Executed candidate closure

The repaired source `113a83f50f856b77a38a8db008e6fef7cffe2bcb` passed GitHub Actions run `34004666384` (attempt 1, completed success):

- job `101409611501` — `YU-32 / full-regression`: targeted application-boundary tests plus the complete YU-30 canonical gate passed;
- job `101409611587` — `YU-32 / runtime-smoke`: the complete YU-31 real HTTP/gRPC/stdio MCP process/closure smoke passed.

Artifacts were downloaded through the connected GitHub artifact API and independently SHA-256 checked:

- `9980627666` / `yu32-full-34004666384-1` / `sha256:bd9dbe778cffbda3fef052c7888029e37886a8e8a5f37c2b63ec02f272b83246`;
- `9980577143` / `yu32-runtime-34004666384-1` / `sha256:694e195da4cf821962ffc63c2489df82f986075eebd5c57f0b6bd19b0a65496c`.

Both `head.txt` files equal the exact source SHA. All four `worktree.txt` / `worktree.patch` files are empty.

The executable Yunka audit is the decisive architecture-debt readback:

```json
{
  "existing": [],
  "new": [],
  "fixed": [
    "AUDIT-AUTH-001:backend-yunka/internal/delivery/application/adapter.go:github.com/hvritual/yunka.io/gateway/authz"
  ]
}
```

The full gate also completed `YU-30 REGRESSION PASS`; the frontend recorded 22 test files / 74 tests passed, the real YU-29 browser E2E passed, and the recorded dependency-security audit reported zero vulnerabilities for that execution.

An earlier exact-SHA run exposed one **test-harness RED**: `yu32_application_boundary_test.go` initially declared `package main` while the repository root tests use `package backendyunka`. The real product packages `deliveryauthz` and `delivery/application` passed in that run. The test package name was corrected and the complete gate rerun to GREEN; no product bypass was introduced.

## Final closeout gate

`YU-32-CI-RECEIPT.json` binds the executed repaired-source run above. This evidence/TASKS closeout is a separate commit and therefore must obtain its own exact-SHA success for BOTH YU-32 jobs before non-force merge. The source run is not reused to certify an unknown future documentation commit. The final artifact heads and worktree drift are read back before merge.

## Framework disposition

No new Yunka framework defect has been reproduced. The confirmed violation was consumer-owned and is repaired without changing `third_party/yunka`, the framework gitlink, protobuf, or generated artifacts. Existing framework Issue #151 concerns the default ChangeSet control-artifact path and is not evidence for a YU-32 security defect.

## Retained risk and review boundary

The local password subsystem still lacks an explicit product contract for online-guessing throttling/account lockout and minimum password length. This remains a **consumer security hardening risk**, not a proven framework defect. YU-33 must surface it in the final operating documentation instead of claiming complete hardening.

The YU-31 runtime proof remains scoped to the exercised Linux/loopback/process scenarios; it is not a Windows/macOS, TLS-production, sustained-load, power-loss or universal leak proof. YU-33 is documentation/manifest closeout only and is not started until YU-32 is merged.

## YU-32H follow-up: local-password risk remediation (2026-09-06)

Fixed follow-up parent: `5e57424b034ae8da0ac27d6fb920b457005ea253`. The user explicitly requested repair of the YU-32 findings. This follow-up therefore establishes and implements the previously absent local-password product contract rather than only carrying that risk into YU-33. The earlier review and its unavailable private YU-00F input remain historical facts, not newly verified inputs.

| Former gap | Implemented control and closure |
| --- | --- |
| No minimum password policy | One `ValidateNewPassword` gate for bootstrap, member creation, administrator reset and self change: at least 15 Unicode code points, valid UTF-8, maximum 4096 bytes; no trimming/truncation or mandatory character classes. Existing secrets remain verifiable; legacy hash upgrades require the verified unchanged secret and exact credential CAS. |
| Unbounded online password guessing | Durable account budget: 10 attempts per 5 minutes, then 15-minute cooldown. Login and current-password verification share the same account budget. Success does not erase concurrent reservations. |
| Process-local or rollback-prone counters | SQLite reservation commits before Argon2 and the business root transaction. Independent database connections share one serialized budget; failed business operations cannot refund it. Restarted Manager instances retain counters. Missing/corrupt storage and capacity exhaustion fail closed. |
| Undefined proxy/client identity | Only actual HTTP `RemoteAddr`; forwarded identity headers are ignored. IPv4-mapped addresses normalize; IPv6 uses /64. BFF users share the BFF source budget (120 attempts/minute, one-minute cooldown) while account budgets stay distinct. No claim of arbitrary trusted-proxy client-IP support. |
| Missing user-visible throttle behavior | HTTP 429, Retry-After and no-store; no credential/session issuance. Next forwards the bounded error response and Retry-After. New-password forms enforce and explain the policy; enrollment failures return 400. |

Counters are bounded to 4096 active account/source buckets and expire automatically. Locked traffic does not extend cooldown. Audit records only sanitized lock transitions; no supplied username, IP or password is copied into throttle audit metadata. Valid established sessions and administrator recovery operations are not automatically invalidated by throttling. Operational details and deployment limitations are in `backend-yunka/README.md` under YU-32H.

### Executed proof and regression repair

`run-yu32h-red-green.sh` copies two tests using existing APIs into the fixed historical parent and requires BOTH exact behavioral failures: short password enrollment accepted, and 11 unbounded password guesses. Compilation/environment failures cannot satisfy these markers. The same tests plus policy, cooldown, independent-connection concurrency, retention, corrupt-store, HTTP and shared password-change budget tests pass on the repaired source with the race detector.

Initial implementation `1797390722b02c31e2fdad7aaf4b5d962dfc0096` in run `34008329939` passed those new tests and real runtime smoke but failed the existing administrator-reset/self-change CAS test: eager centralized session verification returned `ErrSessionInvalid` instead of the required `ErrUserRevisionConflict`. The test was NOT weakened. The repair uses the existing active session-control record only to select a throttle bucket; root-transaction session/CAS/password checks remain authoritative. Identity-producing verification still requires the exact current credential revision. A new regression proves that the stale session cannot authenticate or issue a JWT, the administrator's replacement credential remains intact, and the failed CAS still consumes a durable reservation.

Repaired source `0a83eabea5766a0c4fc64fc84c914140036fd3df` passed BOTH full-regression and runtime-smoke in Actions run `34008694043`. `YU-32H-CI-RECEIPT.json` records exact source, job and artifact identities, independently verified archive digests, artifact head values, zero worktree drift, and the actual audit debt delta. The full job ran authentic RED/GREEN, double generation/full check, ownership/audit/ChangeSet, Go tests/vet/race, frontend tests/typecheck/production build/security audit and real YU-29 browser E2E. The runtime job reran the complete YU-31 HTTP/gRPC/stdio MCP and process-closure test. Local restricted-shell checks are not represented as execution of that pinned CI toolchain.

### Residual boundaries and final gate

The specifically retained absence of a minimum-password/online-guessing/shared-storage/proxy-trust contract is closed for this implementation. This is NOT complete production security certification: legacy password lengths cannot be recovered from hashes; controlled rotation, TLS, edge DDoS controls, compromised-password blocklists and deployments using separate database replicas require their own controls. The shared-budget proof applies to one authoritative SQLite, not an arbitrary distributed deployment. Cooldown is bounded temporary denial, not permanent account disablement.

The checks now make the new security contract executable; the existing lifecycle/CAS matrix also caught a real integration regression before merge. Future credential-boundary changes must preserve BOTH anti-guessing and reset/revocation/CAS tests.

No Yunka source/gitlink, generated/protobuf files or legacy `backend/` were changed. No new framework defect was reproduced. This documentation/receipt closeout is a new commit and MUST independently pass both YU-32 workflow jobs on its own exact SHA before non-force merge. YU-33 and deployment remain unstarted.
