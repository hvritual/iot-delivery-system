# Delivery protobuf contract layout

`backend-yunka/contracts/proto` is the canonical protobuf source root configured by `.yunka/project.json`.

The Delivery v1 contract is physically modularized while keeping one logical protobuf/gRPC API identity:

```text
delivery/v1/
├── delivery_service.proto  # DeliveryService + 25 Yunka operation declarations
├── common.proto            # Evidence / Decision / Comment / Activity / shared links
├── work_item.proto         # WorkItem and item read/write/search/gate DTOs
├── project.proto           # Project, progress and schedule DTOs
├── planning.proto          # Release / Sprint / Milestone DTOs
├── dashboard.proto         # Dashboard / BoardSummary DTOs
├── saved_view.proto        # SavedView / MemberWeek DTOs
└── notification.proto      # Notification DTOs
```

## Invariants

All Delivery files keep:

```proto
package iot.delivery.v1;
option go_package = "github.com/hvritual/iot-delivery-system/backend-yunka/contracts/delivery/v1;deliveryv1";
```

`delivery_service.proto` remains the only owner of `DeliveryService` and the Yunka `delivery/v1` domain declaration. File modularization must not change operation IDs, request/response full names, permissions, authentication, transaction/idempotency, composition, gRPC service identity or the Go package used by handwritten code.

`yunka_bootstrap.proto` is intentionally removed because it was only the initialization placeholder and its own contract said to delete it once real contracts exist.

Generated `*.pb.go`, `*_grpc.pb.go` and `contracts/generated/*` are framework/protoc outputs. Never hand-edit generated files; edit these proto sources and run the canonical generation/check gates.

## Dependency direction

Keep imports acyclic and narrow:

```text
common
  ↑
work_item
  ↑        ↑
dashboard  saved_view

project      planning      notification
```

`delivery_service.proto` imports the bounded-context schemas as the API assembly point. Shared types belong in `common.proto` only when more than one context actually needs them; do not turn `common.proto` back into a catch-all monolith.

## Validation

From `backend-yunka` with the pinned toolchain:

```bash
make yunka-generate
make yunka-check
```

A structural refactor is acceptable only when the canonical 25 `operation-plans.json` entries remain semantically identical, all real Delivery messages remain identical, generated output is reproducible, and the full YU-30/YU-31/YU-32 regression stack remains green.

## AI / ownership boundary

The current pinned Yunka supports recursive multi-file protobuf generation and check, so no consumer workaround is needed for this layout. It does **not** yet expose an enforceable `operation -> contract file/module -> minimal AI context/change scope` relation. The framework-level gap is tracked in `hvritual/yunka.io#160`; this repository deliberately does not add a second hand-maintained `contract_modules` source of truth.
