# YU-16 audit outcome and redaction evidence

> Document class: **EVIDENCE**
> Task: `YU-16`
> Fixed consumer parent: `b39edc7c32dcca0dc4ded565c734dd06efabe78a`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-17`

## Result

YU-16 verifies the consumer audit chain for accepted authentication, failed authentication, denied authorization, and application transaction rollback, while proving that request credentials and session-class secrets are not persisted in audit payloads.

The implementation already contained the four required recorder paths before YU-16:

- `RecordAuthenticationAccepted` records an already verified BFF assertion as authentication success;
- `RecordAuthenticationFailure` records rejected credentials/assertions anonymously without promoting them to a Principal;
- `RecordAuthorizationDenied` records a completed authorization denial for a trusted Principal;
- `RecordApplicationRollback` records the application failure only after the root local transaction has returned and rolled back.

The real YU-16 gap was evidence quality, not a missing framework primitive or a missing recorder implementation. Existing rollback evidence used a development API-key Principal and an authorization-denial unit test used a fake executor. The global task contract explicitly forbids development `localauth` or mocks from proving real identity authorization. YU-16 therefore adds production-shaped executable regressions instead of duplicating the recorder implementation.

## Four outcome paths

| Outcome | Identity / decision path | Persisted audit fact | Side-effect rule |
| --- | --- | --- | --- |
| accepted | signed BFF assertion -> verifier -> identity binding -> JWT Principal | `authentication`, `human`, `success`, `authentication.assertion_accepted` | audit must not contain assertion, body, header, session or CSRF values |
| failure | invalid signed BFF assertion -> no Principal | `authentication`, `anonymous`, `not_evaluated`, `failure`, `authentication.assertion_rejected` | downstream handler not invoked; no credential value persisted |
| denied | durable SQLite human grants -> JWT Principal -> GrantAuthorizer -> OperationGuard -> denied before invocation | `authorization`, `human`, `denied`, `authorization.denied` | application not invoked; no WorkItem or Outbox event created |
| rollback | durable project-scoped contributor grant -> JWT Principal -> GrantAuthorizer -> OperationGuard -> root local transaction -> forced application failure | `delivery`, `human`, `allowed`, `failure`, `application.transaction_rolled_back` | business probe and staged Outbox event roll back first; failure audit persists afterwards |

## Production-shaped authorization proof

`backend-yunka/internal/audit/yu16_runtime_chain_test.go` does not use development roles as the authorization source.

The fixture:

1. opens the consumer SQLite delivery repository;
2. applies the real identity and audit migrations;
3. creates an active organization, active user and tenant-owned project;
4. constructs `humanauthz.NewGrantResolver` against SQLite;
5. constructs Yunka `GrantAuthorizer` from that durable resolver;
6. constructs the consumer `deliveryauthz.OperationGuard` against the same repository/database;
7. constructs Yunka `ExecutionSecurity` and the root transaction Executor;
8. decorates that Executor with `audit.NewRecordingExecutor`;
9. supplies a trusted `identity.AuthMethodJWT` Principal whose `Roles` contains only a forged compatibility value.

For the denial case no durable RoleBinding is inserted. `delivery.items.create` must therefore be rejected before application invocation. The regression asserts zero WorkItems and zero Outbox records and requires exactly one human authorization-denied audit entry.

For the rollback case the user receives one durable `contributor` RoleBinding at `project:project-yu16`. That role/permission/scope is resolved from the real identity tables; `Principal.Roles` is still ignored. The invoked local transaction writes a rollback probe and stages an Outbox event, then returns a forced application error. The regression requires both transaction writes to disappear while one post-rollback failure audit survives.

## Authentication and secret-redaction proof

`backend-yunka/internal/bffhttp/yu16_audit_redaction_test.go` exercises the actual BFF verifier + identity-binding middleware rather than constructing an accepted human Principal directly.

The accepted request carries secret sentinels in locations that must never be audit data:

- request JSON: `password`, `token`, `session`, `csrf`;
- `Authorization` header;
- `Cookie` session value;
- CSRF header;
- signed BFF assertion and signature transport.

The rejected request deliberately replaces the assertion signature with a secret sentinel and must remain anonymous. Both successful and failed authentication entries are then scanned across the persisted audit text surface (`id`, organization/project/actor/scope/target identifiers, reason, trace/request/correlation identifiers, diff summary and metadata). None of the password/token/session/CSRF/authorization/signature sentinels may appear.

The durable JWT authorization test additionally places authorization/session/CSRF values in runtime metadata and a password/token sentinel in application input/error text. The SecurityRecorder persists only its explicit transport/phase/failure-class metadata and trusted correlation identifiers; it does not serialize runtime attributes, request values, or application errors.

## Existing sanitizer and leakage controls retained

YU-16 relies on and re-verifies the existing consumer hardening rather than replacing it:

- audit metadata normalization recursively redacts keys containing password/passphrase/secret/token/credential/API key/client secret/cookie/authorization/session/CSRF/assertion/signature semantics;
- credential-shaped Bearer, Basic, JWT and service-token values are replaced rather than partially masked;
- top-level audit identifier fields reject credential-shaped values;
- diff summaries contain only stable change labels and field paths, never before/after values;
- BFF authentication audit records do not serialize assertion claims, request body, headers, nonce or signature;
- first-party BFF error responses already hide internal error bodies, and the audit/BFF source paths contain no logger call that receives raw request bodies or credential headers;
- the existing repository credential leakage gate remains unchanged.

No password, local credential schema, session implementation or CSRF feature is introduced here; those remain assigned to later YU tasks.

## Regression coverage authored

`backend-yunka/internal/audit/yu16_runtime_chain_test.go` adds:

- durable human JWT authorization denial through SQLite GrantResolver + GrantAuthorizer + OperationGuard;
- proof that forged `Principal.Roles` cannot authorize the denied operation;
- zero application invocation, WorkItem and Outbox side effects on denial;
- durable project-scoped contributor authorization for a local write plan;
- forced business + Outbox writes inside the root transaction followed by application failure;
- proof that both transactional writes roll back before the failure audit is persisted;
- audit actor/result/reason assertions and whole-text secret-sentinel scanning.

`backend-yunka/internal/bffhttp/yu16_audit_redaction_test.go` adds:

- real signed BFF assertion acceptance -> bound human JWT actor -> authentication success audit;
- bad-signature authentication failure -> anonymous failure audit;
- downstream non-invocation on rejected authentication;
- password/token/session/CSRF/Authorization/Cookie/signature sentinel scanning across persisted audit text.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent | PASS: branch created from `main@b39edc7c32dcca0dc4ded565c734dd06efabe78a` |
| Consumer/framework scope | PASS by compare inspection: only first-party consumer tests/evidence are changed; no Yunka source or gitlink modification |
| Generated/protobuf drift | PASS by changed-file inspection: no protobuf or generated artifact changed |
| Accepted authentication path source audit | PASS: BFF middleware verifies assertion, binds identity, creates JWT Principal, then records acceptance |
| Authentication failure path source audit | PASS: rejected assertion records anonymous failure and does not create a Principal |
| Durable human authorization source audit | PASS: `humanauthz` reads active SQLite RoleBindings and explicitly ignores `Principal.Roles` |
| Authorization denial path source audit | PASS: RecordingExecutor records `authorization.denied` only when Yunka returns a normalized denial |
| Rollback path source audit | PASS: RecordingExecutor records application rollback only after the invoked local transaction returns an error |
| Secret handling source audit | PASS: recorder persists a fixed metadata projection; audit JSON sanitizer and identifier validation reject/redact credential surfaces |
| YU-16 executable regressions | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: current execution container cannot resolve `github.com` to materialize the connected repository/dependencies |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| GitHub CI | NOT AVAILABLE unless a status appears after this evidence commit; repository previously had no Actions workflow/status checks |
| Canonical generation/full check | NOT RUN; no canonical source or generated artifact changed |

Environment/tooling unavailability is not treated as RED or PASS. The new tests are executable repository evidence for the next environment that can run the pinned Go toolchain and dependencies.

## Framework issue disposition

No new Yunka framework defect was reproduced in YU-16. The framework's Operation Executor, authorization error normalization, transaction handle, GrantAuthorizer and OperationGuard integration points were sufficient for the consumer audit proof. Yunka source and the fixed gitlink remain unchanged.

No new framework Issue is created. Existing Yunka issues remain outside this task unless independently reproduced by their own evidence.

## Residual boundary and next task

- YU-16 does not implement password storage, local sessions, CSRF issuance, member lifecycle or config revision behavior;
- configuration change/compare/rollback remains reserved for YU-17;
- YU-17 is the next independent task and must start from the final merged YU-16 `main` SHA;
- YU-17 must preserve the same rule: framework problems are Issues only, never framework-source edits inside the consumer task.
