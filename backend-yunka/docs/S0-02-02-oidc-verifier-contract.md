# S0-02-02 OIDC ID Token verifier contract

`internal/oidcverify` is the inbound OIDC trust boundary. Its configuration is
provider-neutral and declares exactly one `Issuer` URL and one `Audience`.
Production setup uses `strings.TrimSuffix(Issuer, "/") +
"/.well-known/openid-configuration"` and requires HTTPS. It does not name,
assume, or embed configuration for any identity vendor.

## Support and rejection matrix

| Input or configuration | Result |
| --- | --- |
| HTTPS issuer, matching discovery issuer and `jwks_uri`, RS256-signed token, exact `iss`, configured audience (string or array member), non-empty `sub`, and future `exp` | Accept; return `issuer`, `subject`, and optional `email`/`name` snapshots only. |
| Token `iss` differs by any character | Reject. |
| Missing audience or audience without the configured value | Reject. |
| Missing or elapsed `exp` | Reject. |
| Unknown signing key, modified signature, unsigned token, or non-RS256 algorithm | Reject. |
| HTTP issuer, HTTP discovery/JWKS endpoint, or a supplied discovery URL in ordinary configuration | Reject at construction. |
| HTTPS discovery/JWKS URL with query parameters, no fragment or userinfo | Supported. The issuer itself may not contain a query or fragment. |
| Localhost or loopback-IP HTTP issuer and supplied local discovery/JWKS endpoint with `AllowInsecureHTTPForTests: true` | Supported only as an explicit hermetic-test boundary. |
| Non-loopback HTTP host, including with the test flag | Reject at construction. |

## Trust boundary

The verifier retrieves provider metadata, checks its issuer for an exact match,
then uses the advertised JWKS endpoint to validate the JWS signature. It does
not parse an unverified token as identity. The returned `VerifiedClaims` type
contains no raw token and no arbitrary provider-claim map; callers must not log
or persist either raw input or unfiltered claims.

`Now` exists for deterministic tests. Production callers should leave it nil,
which uses the system clock. The test-only HTTP/discovery fields are not an
application integration surface and must not be enabled for deployment. The
same fail-closed HTTP client is supplied to discovery and the go-oidc remote
JWKS key set; it refuses redirects and has a five-second request timeout, so a
JWKS refresh cannot outlive that bounded transport deadline.

## Non-goals and following stages

This slice intentionally does not add a HTTP/gRPC middleware, BFF callback or
session, internal `User`/`ExternalIdentity` binding, request `Principal`
propagation, service-account authentication, authorization, audit persistence,
or deployment configuration.

S0-02-03 consumes `VerifiedClaims` to perform the authorized internal identity
binding. S0-02-04 owns BFF login, callback, provider-error handling, and the
request-state validation of nonce/state. Principal propagation belongs to
S0-02-06. No later stage should accept a raw ID Token in place of this verified
result.
