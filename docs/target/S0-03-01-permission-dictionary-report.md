# S0-03-01 — Versioned permission dictionary

## Scope completed

The normative dictionary is `backend-yunka/contracts/authorization/permission-dictionary.v1.json`, version `1.0.0`. It contains 12 active operations, 11 resources, 17 permissions (12 active and 5 reserved), 3 canonical scope types, and 6 built-in human roles.

The 12 generated gRPC OperationPlans are mapped one-for-one to resource/action permissions, required scope, risk/write attributes, and REST/MCP traces. Project, release, sprint and milestone creation now use separate declared permissions. Generated artifacts were regenerated from `contracts/proto/iot_delivery.proto`; no generated source was edited manually.

## Evidence and gates

`backend-yunka/permission_dictionary_test.go` strictly decodes the normative JSON and generated `operation-plans.json`: unknown fields and multiple JSON values are rejected, and every declared field is represented by the model. It validates schema version/IDs, the exact v1 resource set and resource scopes, exact generated-operation core metadata (resource, permission, required scope, risk, writes), resource/permission references and scope subsets, exact six-role permission and scope matrices, canonical scope hierarchy/default denial, no wildcards, exact separation-of-duties metadata, complete service-identity policy, exact development-only profiles/aliases, and REST/MCP traces. Mutation cases prove rejection of unknown or misspelled fields, missing rules, extra resources, resource-scope expansion, risk downgrade, required-scope drift, operation/permission resource mismatch, unknown constraint permission, extra role grants, expanded role scope, and local profile drift.

The RED sequence first failed because the dictionary was absent; after creation it failed because generated plans still held coarse permissions; the localauth compatibility test failed until its explicit mappings were changed. The targeted suite is now green.

## Explicitly not runtime enforcement

This task does not create Team/Membership/Role/Permission/RoleBinding tables or migrations, resolve grants, install an OperationGuard, close enumeration attacks, issue service identity grants, or enforce separation of duties. The dictionary's `reserved` permissions have no currently exposed management transport. Scope and deny rules are normative inputs for S0-03-04/05; their runtime enforcement is not claimed here.

The development-only local API-key mapper now has four explicit effective-permission profiles that are checked bidirectionally against `localauth.NewGrantResolver`. They are not claimed to be one-to-one production role mappings: `release-manager` deliberately composes contributor and approval permissions. Two aliases remain only for existing ungenerated local extensions (`delivery.items.read` and saved-view `delivery.items.write`). This intentionally tightens planning-entity access relative to the former generic `delivery.items.write`; it is not a claim of complete behavior compatibility.

## Boundary preserved

No web files, live service, database, Vault, or external network resource were used. `third_party/yunka` was initialized only from the pre-existing local Git object at locked gitlink `9a51562aa7bcef42f6861bd91abd30aae13ed6ef`; it is clean and unchanged.
