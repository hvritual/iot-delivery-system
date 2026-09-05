# YU-23 local-member transport authentication and durable authorization evidence

> Document class: **EVIDENCE**
> Task: `YU-23`
> Fixed consumer parent: `922cd3d82488b91462c2b47d8410ba09eadedf47`
> Fixed Yunka gitlink: `057ebcf88a87303eb633eb6e604d306f633dfac0`
> Scope stop: before `YU-24`

## Result

YU-23 connects the YU-22 verified local-member identity to the existing protected application execution chain without creating a second authorization model.

The resulting human path is:

```text
local access JWT / verified opaque session
        -> YU-22 signature/session/revision/revocation verification
        -> AuthMethodJWT Principal(UserID, TenantID, Roles=[])
        -> principalauthz
        -> humanauthz SQLite durable grants
        -> OperationGuard durable scope/binding verification
        -> existing Yunka Executor/application operation
```

HTTP and gRPC accept local access JWTs. The development-only stdio MCP process may use either a local access JWT or an opaque session and resolves that credential again for every tool invocation. MCP remains development-only; YU-23 does not expand the stdio process into a production remote MCP surface.

No local login/current/logout/change-password/admin-member HTTP route is added. Those remain YU-26 work.

## Fixed-parent RED

### Expected

After YU-21/YU-22, a verified local member needed to enter every existing protected consumer transport with the same identity and authorization facts:

1. transport authentication must establish only a server-verified `AuthMethodJWT` Principal;
2. `Principal.Roles` must not become a local-member authorization authority;
3. HTTP/gRPC/MCP must resolve the same current durable RoleBinding/permission/scope state;
4. revoked/invalid local credentials must be rejected before application invocation;
5. service-account credentials, local-member credentials and development API keys must not select identity by interceptor/header precedence;
6. development compatibility must remain explicit and must not convert JWT humans into `local-admin`;
7. MCP must not freeze a Principal at process startup when the underlying local session can later be revoked.

### Observed at the fixed parent

The fixed parent had the YU-22 verified local JWT/session capability, but the transport roots were still separate:

- HTTP accepted the existing BFF assertion path or development API-key path; it had no local access-JWT adapter.
- gRPC accepted Yunka service credentials or development API-key fallback; it had no end-user local access-JWT adapter.
- MCP authenticated one development API key at process startup and reused one static Principal for all later tool calls.
- development `configuredAuthorization` selected the legacy `localauth` authorizer for the entire environment, so a JWT human could not use the same durable human grant chain used in production.

This was a real consumer composition gap. It did not require a Yunka framework defect.

## Yunka framework boundary checked

The fixed Yunka gRPC authentication contract deliberately keeps service identity separate from end-user identity:

```text
x-yunka-service-authorization
```

is the framework service-credential metadata, and the framework explicitly states that it must not turn a forwarded user token into a trusted service Principal.

YU-23 therefore does not modify Yunka. Consumer local members use standard end-user `authorization: Bearer <JWT>` metadata, while service accounts continue to use the framework-owned service metadata.

## Local transport verifier

New consumer package:

```text
backend-yunka/internal/localtransportauth
```

`Verifier` wraps the existing `locallogin.Manager` only for authentication adaptation. It does not resolve permissions or roles.

### Access JWT

`VerifyAccessToken` delegates to YU-22 `locallogin.Manager.VerifyAccessToken` and then requires:

- authenticated Principal;
- `AuthMethodJWT`;
- canonical non-empty TenantID;
- canonical non-empty UserID;
- `Subject == local-user/<UserID>`.

The returned Principal always has:

```text
Roles = nil
```

### Opaque session

`VerifySessionToken` delegates to YU-22 server-side opaque-session verification and builds the same Principal shape from the verified session's OrganizationID/UserID.

It does not accept caller-supplied tenant, user or role fields.

## HTTP authentication

When a standard `Authorization` header is present, the new HTTP middleware owns local-member authentication:

```text
Authorization: Bearer <local access JWT>
```

It verifies the token before calling the protected handler and writes a trusted Principal plus HTTP runtime metadata into context.

When the header is absent it delegates to the existing BFF/development compatibility middleware.

Credential families are fail-closed. A local JWT is rejected if the request also carries:

- `X-API-Key`;
- BFF assertion header; or
- BFF assertion signature header.

`X-Trace-ID` is not treated as a competing credential because it is correlation metadata, not identity proof.

A revoked or invalid local JWT is rejected before the protected handler/application operation is invoked.

## gRPC authentication

The consumer gRPC selection order is explicit:

```text
x-yunka-service-authorization present
        -> serviceauth / Yunka CredentialVerifier
otherwise authorization present
        -> localtransportauth local-member JWT
otherwise development only
        -> legacy API-key fallback
```

The outer service selector rejects mixed service and end-user credential families instead of choosing by interceptor order.

Service credentials are mutually exclusive with:

- end-user `authorization` metadata; and
- development `x-api-key` metadata.

The local-member gRPC interceptor itself also rejects `authorization + x-api-key`.

A verified local JWT enters the handler as the same no-Roles JWT Principal used by HTTP.

## MCP authentication

The old MCP server stored one static Principal in the server struct.

YU-23 adds:

```text
PrincipalResolver func(context.Context) (identity.Principal, error)
NewWithPrincipalResolver(...)
```

`toolContext` invokes the resolver for every tool invocation.

Therefore a local JWT/session is not converted into an indefinitely trusted process Principal. If the credential/session becomes invalid, the next tool call is normalized to `unauthenticated` before the application operation runs.

The old `New(operations, principal)` API remains as a compatibility wrapper for development API-key callers and existing tests.

### MCP credential selection

The development-only stdio command supports exactly one explicit MCP credential family:

- `IOT_DELIVERY_MCP_ACCESS_TOKEN`; or
- `IOT_DELIVERY_MCP_SESSION_TOKEN`; or
- `IOT_DELIVERY_MCP_API_KEY`.

The historical global `IOT_DELIVERY_LOCAL_API_KEY` is only a fallback when none of those explicit MCP credentials is configured. A global development API key does not turn an explicitly selected local JWT/session into a mixed MCP credential.

Local access/session MCP modes also require `IOT_DELIVERY_LOCAL_AUTH_JWT_KEY` so the process can reverify the local credential.

The stdio process remains development-only for all credential modes.

## Authorization composition

Before YU-23, development mode replaced the production durable authorizer with `localauth.NewAuthorizer()`.

YU-23 changes selection to Principal type rather than environment-wide human authority:

```text
AuthMethodJWT          -> humanauthz durable SQLite resolver
AuthMethodServiceToken -> serviceauthz durable SQLite resolver
AuthMethodAPIKey       -> development compatibility resolver, development only
```

`principalauthz.NewWithDevelopmentCompatibility` adds the API-key resolver only as an explicit third branch. JWT humans can never fall back from durable human grants to local API-key grants.

## OperationGuard composition

Production continues to use:

- local member-administration OperationGuard; then
- delivery OperationGuard.

Development now keeps the same durable guards for JWT humans and service principals.

The historical API-key bypass is preserved only for the explicit local development Principal shape:

```text
Authenticated = true
AuthMethod = api-key
TenantID = local-development
Subject == UserID
UserID begins local-api-key/
```

A JWT Principal carrying forged `Roles = [local-admin, system-administrator]` still enters the durable human resolver/guard and receives no authority from those role strings.

## Cross-transport durable proof authored

`TestYU23HTTPGRPCAndMCPLocalMembersResolveTheSameDurableGrantAndGuard` creates a real SQLite identity state and a real active `system-administrator` organization RoleBinding for one User.

The test obtains the same User identity three ways:

- HTTP local access JWT;
- gRPC local access JWT;
- MCP opaque session verification.

It asserts that every Principal is:

```text
Authenticated = true
AuthMethod = jwt
TenantID = org-a
UserID = user-a
Roles = empty
```

Then every Principal is passed through the same `humanauthz.Resolver` for `identity.users.manage`.

All three must resolve the same grant:

```text
permission = identity.users.manage
role       = system-administrator
scope      = organization:org-a
```

The same three Principals are then passed through the same GrantAuthorizer and real YU-20 member `OperationGuard` for the canonical member-create policy.

After the durable RoleBinding is disabled, all three resolve zero grants and all three authorization decisions deny.

This proves transport identity is not an authorization snapshot; current durable state is re-read.

## Revocation proof authored

`transport_test.go` covers real YU-22 login tokens and sessions.

For both HTTP and gRPC:

1. a valid token reaches the handler;
2. the Principal has no roles;
3. mixed credential families are rejected;
4. YU-22 logout revokes the server-side session;
5. the same already-issued JWT is rejected on its next request before handler invocation.

Opaque session verification similarly fails after logout.

MCP's dynamic resolver test proves the resolver is called once per tool invocation and that a second invocation fails as `unauthenticated` when the resolver reports revocation.

## Authentication audit classification

YU-23 does not reuse the BFF-specific accepted reason for local JWTs.

New audit method:

```text
RecordLocalAccessAuthenticationAccepted
```

records:

```text
event category = authentication
operation      = authentication.local_access_token
reason         = authentication.local_access_accepted
actor          = verified human UserID/TenantID
transport      = http or grpc
```

The existing BFF assertion accepted audit remains unchanged.

Authentication failure audits contain stable classifications only and do not persist bearer tokens.

## Compatibility and non-goals

YU-23 deliberately does not:

- add login/current/logout/change-password/admin-member BFF routes;
- remove the existing production BFF startup contract;
- create or manage project RoleBindings;
- implement centralized session invalidation for administrator reset, User disable or role revocation;
- create a production remote MCP server;
- add UI;
- modify protobuf/generated files;
- modify Yunka framework source.

Those remain owned by YU-24/YU-25/YU-26 and later tasks.

## Authored regression inventory

### bootstrap

- `TestYU23DevelopmentEnvironmentDoesNotTurnJWTHumanIntoLocalAdmin`
- `TestYU23DevelopmentAPIKeyCompatibilityStillUsesExplicitLocalProfile`
- development compatibility guard tests, including forged Principal rejection

### principalauthz

- JWT human routes only to human durable resolver
- service Principal routes only to service durable resolver
- development API key routes only to the explicitly installed development resolver

### localtransportauth

- real YU-22 HTTP access JWT verification
- real YU-22 gRPC access JWT verification
- mixed local-JWT/API-key rejection
- revoked JWT rejection before handler invocation
- opaque-session revocation reread
- cross-transport identical durable grant + OperationGuard behavior

### serviceauth

- service-token + local-JWT rejected
- service-token + development API-key rejected
- handler/fallback not invoked for mixed credentials

### mcpserver / MCP command

- Principal resolver executes for every tool call
- later resolver failure becomes `unauthenticated`
- access/session/MCP API-key credential-family selection
- explicit local-member MCP credential is not confused with global development API-key compatibility
- legacy global API-key remains fallback only
- all MCP modes remain development-only

## Execution status

The cloud execution environment was retried with:

```text
git ls-remote https://github.com/hvritual/iot-delivery-system.git HEAD
```

and returned:

```text
Could not resolve host: github.com
```

Therefore executable verification is recorded exactly as:

```text
YU-23 tests          AUTHORED, NOT EXECUTED
go test ./...        NOT RUN
go test -race ./...  NOT RUN
go vet ./...         NOT RUN
```

Environment/tool absence is not treated as RED and no PASS is fabricated.

## Generation/dependency drift

No YU-23 change touches:

- `go.mod`;
- `go.sum`;
- protobuf contracts;
- generated assembly/contracts;
- `third_party/yunka`;
- Yunka framework source.

No generation run is required by the authored diff. Full generation/executable gates remain part of later canonical certification tasks when the execution environment is available.

## Framework disposition

No new Yunka framework defect was reproduced.

YU-23 uses existing framework contracts as designed:

- Yunka Principal context;
- GrantResolver/GrantAuthorizer;
- OperationGuard;
- Executor security phase;
- separate gRPC service-credential metadata.

No framework Issue is created and the framework remains unmodified.

## Next boundary

YU-24 owns project role assignment/revocation management over the existing durable `RoleBinding` model.

YU-23 does not begin YU-24.
