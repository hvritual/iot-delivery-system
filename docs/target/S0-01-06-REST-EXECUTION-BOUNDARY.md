# S0-01-06 REST execution boundary

## Target state

Every `backend-yunka/internal/httpapi` route is constructed with the concrete
`*delivery/application.Operations` boundary. `NewHandler` and `Register` do
not accept a domain `*delivery.Service`, a repository, or a capability-shaped
service interface.

The request path is:

```text
REST -> Operations -> generated Plan or extension Plan -> Yunka Executor
     -> Adapter -> delivery.Service -> Repository and transactional Outbox
```

Generated plans cover the generated delivery write operations. Read extensions
and the saved-view extension still enter the same executor through an explicit
extension Plan before their adapter/service action. This keeps local API-key
authorization and the SQLite transaction boundary in one place.

## REST behavior retained

Existing URLs, JSON payloads, success status codes, and the front-end's normal
methods are retained. Items and their context use `PATCH`; comments, gates,
close, projects, releases, sprints, milestones, and saved views use `POST`.

The known gate and close action paths now accept only `POST`. `GET` or `PATCH`
to either action returns `405 Method Not Allowed` with `Allow: POST` before
JSON decoding or execution, so neither the item nor the transactional Outbox
can change.

## Deliberate non-goals

This slice does not alter gRPC or MCP transport wiring, protocol/generated
artifacts, authorization-model design, runtime seed behavior, or the mixed
item `PATCH` compatibility behavior. A mixed item `PATCH` still invokes its
work-item update and context update sequentially; it is therefore not one
combined atomic operation. That transaction-granularity decision remains
outside S0-01-06.
