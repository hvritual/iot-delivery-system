# Versioned permission dictionary

The machine-readable source is [`../contracts/authorization/permission-dictionary.v1.json`](../contracts/authorization/permission-dictionary.v1.json). Its `schemaVersion` is part of the authorization contract: any semantic change requires a new version and matching validation updates. For v1, the executable gate locks the exact resources and their scope sets, plus every operation's resource, permission, required scope, risk, and write flag. It does not claim JSON Schema validation or use a `$schema` meta-schema pointer; the executable gate uses a strict, versioned Go model that rejects unknown fields and multiple JSON values.

## Scope and default denial

The only canonical scopes are `organization:{organization_id}`, `project:{project_id}`, and `object:{resource_type}:{object_id}`. Evaluation may inherit only downward from a bound organization to an owned project and then an owned object. It never inherits upward, to another organization, or to an object with unknown ownership.

An absent or unknown scope, unknown resource ownership, cross-organization request, unregistered operation, or unregistered permission is denied. There are no wildcards, owner shortcuts, or authenticated-global grants.

## Current transport contract

All 13 generated gRPC OperationPlans are registered in the dictionary with their stable resource, action permission, required scope, risk and write marker. Each also records the equivalent REST path and, when an equivalent exists, the MCP tool. An empty MCP list means that no direct MCP synonym is currently exposed; it does not imply access.

Project listing uses `delivery.projects.read` at project scope; an organization-bound administrator or auditor receives that project authority only for projects owned by the bound organization. Project, release, sprint and milestone creation use their own permissions. They are no longer covered by a generic work-item write permission.

## Built-in human roles

| Role | Binding scope | Effective authority |
| --- | --- | --- |
| `system-administrator` | organization | All listed business and reserved management/audit permissions, only within its bound organization and descendants. |
| `project-administrator` | project | Project-local work, release/sprint/milestone, membership and role-binding management; cannot create projects or cross projects. |
| `release-approver` | project | Read plus gate advance and close only. |
| `contributor` | project | Read, create, update, comment and context update; never gate, close, or manage bindings. |
| `viewer` | project | Business read only. |
| `auditor` | organization | Audit read and necessary dashboard/work-item context; no business writes. |

Team, membership, role, role-binding and audit permissions are marked `reserved`: they have no current REST, gRPC, or MCP management endpoint. A service identity is not a human role and receives no default grant; a later task must grant it explicitly per operation and project.

`no-self-production-verification` is enforced by S0-03-07 at the shared delivery-service boundary. It records a complete, server-derived implementation principal at `development_completed`, then requires a different named JWT human principal from the same tenant for production validation and close. Service identities, development API keys, missing or malformed principals, inconsistent persisted principal sources, cross-tenant callers, and legacy records without that source fail closed.

## Development compatibility

`internal/localauth` remains a development-only compatibility mapper for legacy local API-key roles. The dictionary records four exact local effective-permission profiles and the gate compares them bidirectionally with `NewGrantResolver`. They are not one-to-one production role mappings: for example, `release-manager` composes create/update context permissions as well as release approval. Two explicit aliases exist only for ungenerated local extensions: `delivery.items.read` and saved-view `delivery.items.write`.

This is an intentional tightening from the older generic `delivery.items.write` behavior for planning entities, not a claim of complete behavior compatibility. These profiles and aliases are not production permissions or RoleBinding records, and the literal `local` scope must not be reused by production authorization.
