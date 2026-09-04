# S0-02-03 Identity Binding

## Scope

`backend-yunka/internal/identitybinding` translates only an
`oidcverify.VerifiedClaims` value and a trusted caller-supplied
`organization_id` into an internal `identitycore.User`. It accepts no raw
token, arbitrary claims map, HTTP input, or principal. It does not assign
roles or permissions: every provisioned user starts with no authorization.

The global login key is the exact `(issuer, subject)` pair. Email and display
name are not identity keys and are never used to find, merge, rebind, or audit
an identity. The organization is never inferred from email, display name, or a
claim.

## Resolution state machine

```text
validated organization_id + VerifiedClaims
  -> organization exists and active?
     -> no: reject
  -> external (issuer, subject) exists?
     -> no: atomically create active User + active ExternalIdentity
     -> yes: same organization and all of Organization/User/ExternalIdentity active?
        -> no: reject without any update or replacement
        -> yes: return its immutable User ID and refresh permitted profile data
```

An existing external key belonging to another organization is rejected. There
is no automatic recovery: disabling any of the three records prevents login,
does not create a substitute record, and does not refresh `last_seen_at` or
profile fields. Disabling is a retained, observable idempotent status update:
the first active-to-disabled call sets `updated_at`; later calls return success
without changing that timestamp. No identity record is hard deleted.

## Atomicity and concurrency

New User and ExternalIdentity rows are inserted in one SQLite transaction.
Therefore a failed external-identity insert rolls back the new User as well.
The database `UNIQUE (issuer, subject)` constraint is authoritative. Only the
SQLite `SQLITE_CONSTRAINT_UNIQUE` extended code triggers one convergence read;
PRIMARY KEY and other constraints return their original error. If the
convergence read finds a disabled or cross-organization binding, its current
sentinel error is returned. There is no production retry loop or second User
provision. This slice does not include a multi-process concurrency test.

## First-version IdP profile synchronization

On a successful existing binding, `ExternalIdentity.last_seen_at` and its
`updated_at` always advance. A nonblank changed `email` or `display_name`
updates the User's mutable profile and matching ExternalIdentity snapshots;
the User's `updated_at` advances only for such a profile change. Missing or
blank optional profile values leave existing nonblank User data and snapshots
intact, while still recording the successful login in `last_seen_at`. These
fields do not affect the immutable User ID, organization, issuer, or subject.
New users without a display name receive the neutral internal display value
`Unnamed user`; it is not derived from IdP profile data.

## Deliberate follow-on boundaries

This slice neither verifies OIDC tokens nor invokes BFF, HTTP, gRPC, MCP,
session, Principal, authorization, service identity, or audit code. Error
sentinels identify invalid input, not-found records, cross-organization use,
and disabled records for later security mapping, without incorporating token
material or raw claim payloads.
