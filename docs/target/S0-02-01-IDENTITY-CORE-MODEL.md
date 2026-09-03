# S0-02-01 Identity Core Model

## Scope and ownership

This slice adds the durable, normalized identity core for the `backend-yunka`
shared SQLite database. The domain package is
[`backend-yunka/internal/identitycore`](../../backend-yunka/internal/identitycore),
deliberately distinct from `yunka.io/framework/core/identity`: it models
delivery-system records only and neither authenticates a caller nor creates an
execution principal.

The four records use stable, caller-supplied text IDs; names and email are not
identity keys. All timestamps are UTC text values produced by SQLite.

| Record | Meaning | Database identity and relationship |
| --- | --- | --- |
| `Organization` | Future organization boundary, including the single-organization default deployment. | Immutable semantic `id`; unique `slug`. |
| `User` | Human operator attributable in future audit records. | `id` belongs to one organization; `email` is nullable and deliberately not unique. |
| `ExternalIdentity` | A future login binding to one internal user. | Globally unique `(issuer, subject)`; composite FK requires `user_id` to be in the same `organization_id`. Optional `email_snapshot` and `display_name_snapshot` are explicit non-sensitive profile fields only. |
| `ServiceAccount` | Separate non-human identity for future CI, MCP, and scheduler callers. | Separate table and unique `(organization_id, name)`; it has no credential column. |

All records default to `active`; only `active` and `disabled` are valid
persisted statuses. Identity references use `ON DELETE RESTRICT`, so deletion
cannot silently break future audit references.

## Migration contract

`identitycore.ApplyMigrations` applies the forward-only migration
`S0-02-01_identity_core_v1` within one SQLite transaction. It creates and
checks the generic `iotd_schema_migrations` ledger, then creates the four
identity tables and records the migration. A second bootstrap reads the ledger
and performs no duplicate schema work. The migration is called immediately
after the shared delivery SQLite repository opens in
[`application.go`](../../backend-yunka/internal/bootstrap/application.go), so a
failure closes that repository and returns an error; it cannot silently start
the runtime.

Rollback is transactional before commit: a schema or ledger error rolls back
the whole migration transaction. There is intentionally no automatic down
migration after commit; recovery of an already-applied migration is an
authorized SQLite backup/restore operation, not a bootstrap side effect.
This ledger records only this identity migration and does not claim that
delivery, outbox, or inbox schemas were historically migrated by it.

## Constraint boundary

SQLite enforces required nonblank IDs/names, status values/defaults, unique
slugs, duplicate-email allowance within the same organization, global external-binding uniqueness,
same-organization external binding, service-account naming, and restrictive
foreign keys. The tests read the SQLite catalog and exercise those constraints
with a temporary database.

The following remain intentionally for S0-02-03 repository/application work:

- issuing stable IDs and preventing application-level replacement of an
  existing ID;
- validating OIDC issuers and subjects, mapping a verified login, and deciding
  whether an identity may be created or disabled;
- `email_snapshot`, `display_name_snapshot`, and `last_seen_at` update policy;
- all principal propagation, BFF/session behavior, service credentials,
  permissions, and audit writes.

## Explicit non-goals

This slice creates no organization, user, external identity, or service
account. It stores no raw OIDC claims, ID token, access token, refresh token,
token, secret, password, API key, or credential hash;
implements no OIDC validation or binding workflow; changes no Yunka source or
gitlink; and performs no real database, Vault, deployment, or network action.
