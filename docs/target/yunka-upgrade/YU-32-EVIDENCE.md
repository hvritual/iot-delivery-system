# YU-32 independent security / architecture review evidence

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
| `AUDIT-AUTH-001`: delivery Application imports Yunka `gateway/authz` | **CONFIRMED -> REPAIRED** | The fixed parent imports `github.com/hvritual/yunka.io/gateway/authz` from `internal/delivery/application/adapter.go` and constructs `authz.Denied` for SOD errors. This is exactly the framework audit invariant: authorization implementation belongs at the root security boundary. The repair moves SOD error classification to `internal/deliveryauthz`, preserves the domain sentinel, removes the direct Application import, and adds a source-level regression. Final audit must show no current existing/new proven debt. |
| Cross-domain repository / framework-platform bypass in handwritten Applications | **REFUTED** | YU-30 audit had no `AUDIT-APP-001` or `AUDIT-INFRA-001`; source review found no transport persistence access. The final exact-SHA audit is required again after the repair. |
| JWT human authorization trusts `Principal.Roles` | **REFUTED** | `principalauthz.Resolver` sends JWT principals only to `humanauthz.Resolver`; that resolver explicitly ignores `Principal.Roles` and rereads active Organization/User/RoleBinding/Permission facts from SQLite on every request. Development API-key roles are a separate AuthMethod path. |
| OIDC identities merge by email/display name | **REFUTED** | `identitybinding.Resolver` resolves the durable external identity by exact verified issuer+subject. Email/display name are profile snapshots only; absence provisions a distinct canonical User rather than matching an existing profile. |
| HTTP/BFF/MCP transports persist business facts directly | **REFUTED** | Production source under `httpapi`, `bffhttp`, `localbffhttp`, `mcpserver`, and delivery transports contains no direct `database/sql`/Exec/Query persistence path. YU-30 no-bypass and transaction packages remain part of the final gate. |
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

## Required executable closure

The final exact YU-32 SHA must pass both jobs in `.github/workflows/yu32-independent-review.yml`:

- `YU-32 / full-regression`: targeted boundary tests plus the complete YU-30 gate, including double generation/check, Yunka ownership/audit/ChangeSet, Go test/race/vet, frontend test/typecheck/build/audit, real YU-29 browser E2E, and final zero drift;
- `YU-32 / runtime-smoke`: the complete YU-31 real HTTP/gRPC/stdio MCP process/closure smoke.

The final audit JSON must classify the fixed-parent `AUDIT-AUTH-001` as fixed and report no current existing or new proven debt. Artifact head SHA and worktree drift must be read back before merge.

## Framework disposition

No new Yunka framework defect has been reproduced. The one confirmed architecture violation is a consumer import/composition error and is repaired without changing `third_party/yunka` or its gitlink. Existing framework Issue #151 concerns the default ChangeSet control-artifact path and is not evidence for any YU-32 security defect.

YU-33 final documentation is not started by this review until the exact final SHA is green and non-force merged to main.
