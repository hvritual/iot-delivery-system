import { once } from "node:events";
import { createPublicKey, createSign, generateKeyPairSync, randomBytes } from "node:crypto";
import { createServer, type Server } from "node:http";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

let provider: Server;
let issuer = "";
let expectedNonce = "";
let tokenMode: "valid" | "nonce" | "issuer" | "audience" | "expiry" | "signature" | "failure" = "valid";
let observedTokenRequest = "";
const signingKey = generateKeyPairSync("rsa", { modulusLength: 2048 }).privateKey;
const incorrectSigningKey = generateKeyPairSync("rsa", { modulusLength: 2048 }).privateKey;
const publicJwk = createPublicKey(signingKey).export({ format: "jwk" });
const testClientSecret = randomBytes(32).toString("base64url");
const mockAccessToken = randomBytes(32).toString("base64url");

beforeAll(async () => {
  provider = createServer(async (request, response) => {
    if (request.url === "/.well-known/openid-configuration") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({
        issuer,
        authorization_endpoint: `${issuer}/authorize`,
        token_endpoint: `${issuer}/token`,
        jwks_uri: `${issuer}/jwks`,
        id_token_signing_alg_values_supported: ["RS256"],
      }));
      return;
    }

    if (request.url === "/jwks") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ keys: [{ ...publicJwk, kid: "test-key", use: "sig", alg: "RS256" }] }));
      return;
    }

    if (request.url === "/token") {
      observedTokenRequest = await requestBody(request);
      if (tokenMode === "failure") {
        response.writeHead(502, { "content-type": "text/plain" });
        response.end("provider internal diagnostic that must never reach the browser");
        return;
      }
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({
        token_type: "Bearer",
        access_token: mockAccessToken,
        id_token: signIdToken(tokenMode),
      }));
      return;
    }

    response.writeHead(404).end();
  });
  provider.listen(0, "127.0.0.1");
  await once(provider, "listening");
  const address = provider.address();
  if (!address || typeof address === "string") throw new Error("mock OIDC provider did not expose a TCP address");
  issuer = `http://127.0.0.1:${address.port}`;
});

afterAll(async () => {
  provider.close();
  await once(provider, "close");
});

describe("OIDC BFF login", () => {
  it("redirects only with safe authorization request parameters", async () => {
    const route = await import("@/app/auth/login/route").catch(() => null);
    expect(route).not.toBeNull();

    const priorEnvironment = { ...process.env };
    Object.assign(process.env, {
      OIDC_ISSUER: issuer,
      OIDC_CLIENT_ID: "test-client",
      OIDC_CLIENT_SECRET: testClientSecret,
      OIDC_REDIRECT_URI: "http://127.0.0.1:5173/auth/callback",
      OIDC_ALLOW_INSECURE_TEST_HTTP: "1",
    });

    try {
      const response = await route!.GET(new Request("http://127.0.0.1:5173/auth/login"));
      expect(response.status).toBe(302);
      expect(response.headers.get("cache-control")).toContain("no-store");

      const location = new URL(response.headers.get("location")!);
      expect(location.origin + location.pathname).toBe(`${issuer}/authorize`);
      expect([...location.searchParams.keys()].sort()).toEqual([
        "client_id",
        "code_challenge",
        "code_challenge_method",
        "nonce",
        "redirect_uri",
        "response_type",
        "scope",
        "state",
      ]);
      expect(location.searchParams.get("client_id")).toBe("test-client");
      expect(location.searchParams.get("response_type")).toBe("code");
      expect(location.searchParams.get("scope")).toBe("openid email");
      expect(location.searchParams.get("code_challenge_method")).toBe("S256");
      expect(location.href).not.toContain(testClientSecret);
      expect(location.searchParams.get("state")).toMatch(/^[A-Za-z0-9_-]{32,}$/);
      expect(location.searchParams.get("nonce")).toMatch(/^[A-Za-z0-9_-]{32,}$/);
      expect(location.searchParams.get("code_challenge")).toMatch(/^[A-Za-z0-9_-]{43}$/);
    } finally {
      for (const key of Object.keys(process.env)) {
        if (!(key in priorEnvironment)) delete process.env[key];
      }
      Object.assign(process.env, priorEnvironment);
    }
  });

  it("returns only a no-store verified result after code exchange and ID token validation", async () => {
    const callback = await import("@/app/auth/callback/route").catch(() => null);
    expect(callback).not.toBeNull();

    await withOidcEnvironment(async () => {
      tokenMode = "valid";
      const login = await beginLogin();
      const response = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?code=accepted-code&state=${login.state}`));
      expect(response.status).toBe(200);
      expect(response.headers.get("cache-control")).toContain("no-store");
      await expect(response.json()).resolves.toEqual({ status: "verified" });
      expect(observedTokenRequest).toContain("grant_type=authorization_code");
      expect(new URLSearchParams(observedTokenRequest).get("code_verifier")).toMatch(/^[A-Za-z0-9_-]{43,128}$/);
      expect(JSON.stringify({ location: login.location.href, result: { status: "verified" } })).not.toContain(testClientSecret);
      expect(JSON.stringify({ location: login.location.href, result: { status: "verified" } })).not.toContain(mockAccessToken);
    });
  });

  it("consumes state before mapping provider errors and rejects unknown or replayed state", async () => {
    const callback = await import("@/app/auth/callback/route").catch(() => null);
    expect(callback).not.toBeNull();

    await withOidcEnvironment(async () => {
      const login = await beginLogin();
      const denied = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?error=access_denied&error_description=do-not-reflect&state=${login.state}`));
      expect(denied.status).toBe(400);
      await expect(denied.json()).resolves.toEqual({ error: "provider_access_denied" });

      const replay = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?error=access_denied&state=${login.state}`));
      expect(replay.status).toBe(400);
      await expect(replay.json()).resolves.toEqual({ error: "invalid_state" });

      const unknownProviderError = await beginLogin();
      const unknownProviderResponse = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?error=unlisted_provider_error&error_description=do-not-reflect&state=${unknownProviderError.state}`));
      expect(unknownProviderResponse.status).toBe(400);
      await expect(unknownProviderResponse.json()).resolves.toEqual({ error: "provider_error" });

      const unknown = await callback!.GET(new Request("http://127.0.0.1:5173/auth/callback?code=not-used&state=unknown"));
      expect(unknown.status).toBe(400);
      await expect(unknown.json()).resolves.toEqual({ error: "invalid_state" });
    });
  });

  it("fails closed for missing code and invalid ID token or provider responses", async () => {
    const callback = await import("@/app/auth/callback/route").catch(() => null);
    expect(callback).not.toBeNull();

    await withOidcEnvironment(async () => {
      const missingCode = await beginLogin();
      const missingCodeResponse = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?state=${missingCode.state}`));
      expect(missingCodeResponse.status).toBe(400);
      await expect(missingCodeResponse.json()).resolves.toEqual({ error: "missing_code" });

      for (const mode of ["nonce", "issuer", "audience", "expiry", "signature", "failure"] as const) {
        tokenMode = mode;
        const login = await beginLogin();
        const response = await callback!.GET(new Request(`http://127.0.0.1:5173/auth/callback?code=accepted-code&state=${login.state}`));
        expect(response.status, mode).toBe(401);
        expect(response.headers.get("cache-control")).toContain("no-store");
        await expect(response.json()).resolves.toEqual({ error: "authentication_failed" });
      }
    });
  });

  it("expires, consumes once, bounds capacity, and uses distinct cryptographic transaction values", async () => {
    const oidc = await import("@/lib/server/oidc");
    let now = 10_000;
    const store = new oidc.InMemoryLoginTransactionStore({ clock: () => now, ttlMs: 10, capacity: 2 });
    const first = store.create({ issuer, clientId: "client", redirectUri: "http://127.0.0.1:5173/auth/callback" });
    const second = store.create({ issuer, clientId: "client", redirectUri: "http://127.0.0.1:5173/auth/callback" });
    expect(first.state).not.toBe(second.state);
    expect(first.nonce).not.toBe(second.nonce);
    expect(first.codeVerifier).not.toBe(second.codeVerifier);
    expect(first.state).toMatch(/^[A-Za-z0-9_-]{32,}$/);
    expect(first.nonce).toMatch(/^[A-Za-z0-9_-]{32,}$/);
    expect(first.codeVerifier).toMatch(/^[A-Za-z0-9_-]{43,128}$/);
    expect(store.consume(first.state)).toEqual(first);
    expect(store.consume(first.state)).toBeUndefined();
    expect(() => store.create({ issuer, clientId: "client", redirectUri: "http://127.0.0.1:5173/auth/callback" })).not.toThrow();
    expect(() => store.create({ issuer, clientId: "client", redirectUri: "http://127.0.0.1:5173/auth/callback" })).toThrow(oidc.LoginTransactionCapacityError);
    now += 11;
    expect(store.consume(second.state)).toBeUndefined();
  });
});

async function beginLogin() {
  const login = await import("@/app/auth/login/route");
  const response = await login.GET(new Request("http://127.0.0.1:5173/auth/login"));
  const location = new URL(response.headers.get("location")!);
  expectedNonce = location.searchParams.get("nonce")!;
  return { location, state: location.searchParams.get("state")! };
}

async function withOidcEnvironment(action: () => Promise<void>) {
  const priorEnvironment = { ...process.env };
  Object.assign(process.env, {
    OIDC_ISSUER: issuer,
    OIDC_CLIENT_ID: "test-client",
    OIDC_CLIENT_SECRET: testClientSecret,
    OIDC_REDIRECT_URI: "http://127.0.0.1:5173/auth/callback",
    OIDC_ALLOW_INSECURE_TEST_HTTP: "1",
  });
  try {
    await action();
  } finally {
    for (const key of Object.keys(process.env)) {
      if (!(key in priorEnvironment)) delete process.env[key];
    }
    Object.assign(process.env, priorEnvironment);
  }
}

function signIdToken(mode: typeof tokenMode): string {
  const now = Math.floor(Date.now() / 1000);
  const claims = {
    iss: mode === "issuer" ? `${issuer}/other` : issuer,
    sub: "mock-subject",
    aud: mode === "audience" ? "wrong-client" : "test-client",
    exp: mode === "expiry" ? now - 60 : now + 300,
    iat: now,
    nonce: mode === "nonce" ? "wrong-nonce" : expectedNonce,
    email: "user@example.test",
    name: "Mock User",
  };
  const encode = (value: unknown) => Buffer.from(JSON.stringify(value)).toString("base64url");
  const signed = `${encode({ alg: "RS256", kid: "test-key", typ: "JWT" })}.${encode(claims)}`;
  const signature = createSign("RSA-SHA256")
    .update(signed)
    .end()
    .sign(mode === "signature" ? incorrectSigningKey : signingKey)
    .toString("base64url");
  return `${signed}.${signature}`;
}

async function requestBody(request: import("node:http").IncomingMessage): Promise<string> {
  let body = "";
  for await (const chunk of request) body += chunk;
  return body;
}
