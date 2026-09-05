# YU-18 local credential schema/repository and password hashing evidence

> Document class: **EVIDENCE**
> Task: `YU-18`
> Fixed consumer parent: `4e79ec550fad19b66c57fc159ca818257efa91ff`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-19`

## Result

YU-18 adds an application-owned local member credential persistence capability without adding a login endpoint, session model, JWT issuer, administrator bootstrap flow, BFF auth route or UI.

The credential is deliberately separate from `identitycore.User`. `users` remains the canonical member identity record; the new credential row can exist only for the tenant-bound `(organization_id, user_id)` identity.

The new capability consists of:

- `internal/localcredential/migration.go` — repeatable, schema-verifying SQLite migration;
- `internal/localcredential/policy.go` — versioned Argon2id policy set and constant-time verification;
- `internal/localcredential/sqlite.go` — tenant-bound, optimistic-CAS credential repository that joins an existing Yunka SQLite root transaction when present;
- executable YU-18 regression tests for migration integrity, hashing, storage leakage, CAS, transaction rollback and policy upgrade detection.

No Yunka framework source, protobuf contract, generated artifact, public transport or UI is changed.

## Fixed-parent gap

### Expected

A later local member authentication flow needs an application-owned credential contract with these properties before any login/session work can begin:

1. credentials are associated with an existing canonical User rather than creating a second identity namespace;
2. plaintext password is never persisted or returned from the repository;
3. password hashing uses a purpose-built slow password hash with per-credential random salt;
4. the stored hash records enough non-secret parameters to verify old hashes after policy upgrades;
5. write replacement is optimistic-CAS capable and can participate in the existing root UnitOfWork;
6. migration truth is independently verified rather than trusting a migration-ledger row.

### Observed at fixed parent

The fixed parent had the canonical `identitycore` organization/User/RoleBinding model and security audit/redaction infrastructure, but no local credential schema, repository, password-hash policy or upgrade marker. The only local authentication compatibility path was the existing development API-key path, which the global task contract explicitly forbids using as proof of production human authentication.

This is a missing consumer capability, not a Yunka framework defect.

## Credential schema

`iotd_local_user_credentials` is a separate table keyed by:

```text
PRIMARY KEY (organization_id, user_id)
FOREIGN KEY (organization_id, user_id)
    REFERENCES users(organization_id, id)
    ON DELETE RESTRICT
```

Persisted fields are limited to:

- tenant/User identity;
- optimistic `revision`;
- password-hash policy version;
- algorithm and Argon protocol version;
- memory / iteration / parallelism parameters;
- random salt and derived password hash;
- created / updated timestamps.

There is no plaintext password column, session, token, CSRF value, browser cookie or authentication assertion column.

The repository also verifies the referenced User before hashing/writing, so the tenant/User association does not depend only on SQLite foreign-key runtime settings.

### Migration integrity

`ApplyMigrations`:

- requires the canonical identity `users` schema first;
- creates the credential table transactionally;
- verifies all 13 canonical columns every run;
- verifies the two-column tenant/User foreign key using `PRAGMA foreign_key_list`;
- records exactly one `YU-18_local_user_credentials_v1` ledger row;
- repeats idempotently;
- rejects a forged migration ledger if the existing physical table does not match the expected schema.

## Password hashing policy

The current storage policy is Argon2id:

```text
algorithm      = argon2id
argon version  = 0x13
policy version = 1
memory         = 19 MiB (19,456 KiB)
iterations     = 2
parallelism    = 1
salt           = 16 random bytes
hash           = 32 bytes
```

The 19 MiB / t=2 / p=1 profile is the current OWASP Password Storage Cheat Sheet minimum Argon2id recommendation reviewed during YU-18. The policy validator also recognizes the documented equivalent memory/time trade-off profiles while rejecting parameters below the supported OWASP floor.

Every password receives a new cryptographic-random salt. Verification derives a candidate with the exact stored policy version and uses constant-time comparison.

### Versioned upgrade model

The hash policy version is independent from the Argon protocol version. `PolicySet` retains old verifiable policies and selects one current policy.

`VerifyPassword` returns:

```text
Match
NeedsRehash
Revision
```

A password that matches an older known policy remains verifiable and returns `NeedsRehash=true`. Updating that password with the current policy produces a new salt/hash and increments the credential CAS revision. YU-21 can later perform successful-login rehash without guessing the original work factor.

Unknown policy versions or stored parameters that do not exactly match the known policy fail closed as unsupported/corrupt credentials.

## Repository and UnitOfWork behavior

`SetPassword(ctx, organizationID, userID, password, expectedRevision)` uses these semantics:

- `expectedRevision = 0`: create only;
- existing row during create: `ErrRevisionConflict`;
- positive expected revision: update only when the stored revision matches, then increment revision;
- missing or cross-tenant User: `ErrUserNotFound`;
- no plaintext password is supplied as an SQL argument;
- create uses `ON CONFLICT (organization_id, user_id) DO NOTHING`, normalizing duplicate-create CAS to `ErrRevisionConflict` rather than leaking a SQLite uniqueness error.

When called inside an active Yunka execution frame, the repository requires the existing transaction handle to be `*sql.Tx` and performs User lookup, credential write and metadata read through that same handle. It does not create a nested UnitOfWork.

A forced root failure after `SetPassword` therefore rolls back the credential row.

## Plaintext / log / audit boundary

YU-18 deliberately keeps credential persistence below the application audit operation layer:

- the production `localcredential` package imports neither the audit package nor `log` / `log/slog`;
- public repository results expose only metadata, match/rehash status and revision; salt/hash material stays package-private;
- password-derived error messages use stable classifications and do not include the supplied password;
- tests use plaintext sentinels and query stored salt/hash columns to prove the sentinel is absent;
- YU-16 remains the audit redaction boundary for password/token/session/CSRF values when future application operations record security facts.

YU-18 itself creates no audit event because there is not yet a credential-management application operation. YU-19/YU-20 own atomic business/security audit recording around credential writes.

## Dependency choice

The repository toolchain is fixed at Go `1.25.0` / `go1.25.13`.

During YU-18, `golang.org/x/crypto v0.56.0` was rejected after its module contract was verified to require Go `1.26.0`. The consumer therefore pins `golang.org/x/crypto v0.55.0`, whose module contract requires Go `1.25.0`, and uses `argon2.IDKey` from that official Go cryptography module.

The task branch was rebuilt from the fixed parent after an intermediate manual `go.sum` edit was found by compare review to have altered unrelated historical checksum lines. The rebuilt branch retains the fixed-parent `go.sum` byte-for-byte rather than carrying that corruption.

Because this connector session cannot execute the repository's Go 1.25 toolchain/module download path, the new x/crypto checksum entries were not materialized by a real `go mod tidy`/`go mod download` run. This is recorded as an unexecuted module-resolution gate, not as PASS and not as behavioral RED.

## Executable regression authored

`internal/localcredential/localcredential_test.go` covers:

- identity schema is required before local credential migration;
- migration is repeatable and records one ledger row;
- a malformed schema cannot be hidden by a forged ledger entry;
- same password for two users gets different salts and different stored hashes;
- current Argon2id metadata is persisted explicitly;
- correct password matches; wrong password does not;
- missing/cross-tenant User is rejected;
- create/update CAS semantics and credential revision increment;
- old password no longer matches after replacement;
- root transaction rollback leaves zero credential rows;
- old policy remains verifiable and explicitly requests rehash;
- below-floor hash policy is rejected;
- hashing/random-source failures do not echo password sentinels.

`internal/localcredential/yu18_contract_test.go` additionally locks:

- exact-column schemas without the tenant/User foreign key are rejected;
- production credential persistence source has no audit/logging dependency.

## Verification ledger

| Gate | Result |
| --- | --- |
| Fixed parent | PASS: task branch derives from `4e79ec550fad19b66c57fc159ca818257efa91ff` |
| Framework boundary | PASS by branch diff: Yunka gitlink/framework source unchanged |
| Protobuf/generated boundary | PASS by branch diff: no protobuf/generated change |
| Credential schema/source review | PASS: separate tenant/User credential table; no plaintext/session/token columns |
| User association review | PASS: composite FK plus repository-side tenant/User lookup |
| Hash policy review | PASS by source + current OWASP review: Argon2id 19 MiB / t=2 / p=1 default |
| Go-version dependency review | PASS by upstream module metadata: x/crypto v0.55.0 requires Go 1.25; v0.56.0 requires Go 1.26 |
| `go.sum` corruption check | PASS after branch rebuild: fixed-parent `go.sum` is no longer changed by YU-18 |
| YU-18 executable tests | AUTHORED, NOT EXECUTED in this connector session |
| `go test ./...` | NOT RUN: no repository execution environment with the declared Go toolchain/dependency materialization is available in this connector session |
| `go test -race ./...` | NOT RUN for the same environment reason |
| `go vet ./...` | NOT RUN for the same environment reason |
| `go mod tidy` / module checksum materialization | NOT RUN; x/crypto go.sum additions remain to be generated by a real module-resolution environment |
| GitHub CI | no usable task status checks available at evidence authoring time |
| Canonical generation/full check | NOT RUN; protobuf/generated inputs were not changed |

Environment/tooling absence is neither behavioral RED nor PASS. The fixed-parent RED is the absence of a local credential persistence/hash contract.

## Framework issue disposition

No Yunka compiler, generator, identity, transaction or execution defect was reproduced. YU-18 uses the existing public execution transaction handle exactly as intended. No new Yunka framework Issue is created and framework source remains unmodified.

## Residual boundary and next task

YU-18 intentionally does **not**:

- create the first administrator;
- expose anonymous bootstrap;
- define password login error behavior;
- issue a session or JWT;
- add BFF auth endpoints;
- add member-management UI.

The next independent task is `YU-19`: implement a one-time administrator initialization flow using the YU-18 credential repository, make anonymous re-initialization permanently impossible after successful bootstrap, and commit administrator identity/role/credential/bootstrap-state/audit atomically.