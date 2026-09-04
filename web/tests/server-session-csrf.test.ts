import { randomBytes } from "node:crypto";

import { describe, expect, it, vi } from "vitest";

import { GET as getSession } from "@/app/auth/session/route";
import { POST as logout } from "@/app/auth/logout/route";
import { guardSessionRequest } from "@/lib/server/session-guard";
import {
  InMemoryServerSessionStore,
  serverSessions,
  SessionCapacityError,
} from "@/lib/server/session";

const login = { issuer: "https://issuer.example", subject: "subject-1", email: "not-returned@example.test" };
const testClientSecret = randomBytes(32).toString("base64url");

describe("server session store", () => {
  it("uses unique opaque identifiers, bounds capacity, and supports revocation", () => {
    const store = new InMemoryServerSessionStore({ capacity: 2 });
    const first = store.create(login);
    const second = store.create(login);
    expect(first.id).toMatch(/^[A-Za-z0-9_-]{32,}$/);
    expect(first.csrfToken).toMatch(/^[A-Za-z0-9_-]{32,}$/);
    expect(first.id).not.toBe(first.csrfToken);
    expect(first.id).not.toBe(second.id);
    expect(() => store.create(login)).toThrow(SessionCapacityError);
    expect(store.revoke(first.id)).toBe(true);
    expect(store.read(first.id)).toBeUndefined();
    expect(store.revoke(first.id)).toBe(false);
  });

  it("expires idle sessions and never renews beyond the absolute TTL", () => {
    let now = 0;
    const store = new InMemoryServerSessionStore({ clock: () => now, idleTtlMs: 10, absoluteTtlMs: 30 });
    const idle = store.create(login);
    now = 10;
    expect(store.read(idle.id)).toBeUndefined();

    now = 0;
    const absolute = store.create(login);
    for (const next of [9, 18, 27]) {
      now = next;
      expect(store.read(absolute.id)).toBeDefined();
    }
    now = 30;
    expect(store.read(absolute.id)).toBeUndefined();
  });

  it("does not renew rejected unsafe requests, but renews allowed requests only up to the absolute deadline", () => {
    let now = 0;
    const origin = "https://app.example";
    const createStore = () => new InMemoryServerSessionStore({ clock: () => now, idleTtlMs: 10, absoluteTtlMs: 30 });

    const badOriginStore = createStore();
    const badOriginSession = badOriginStore.create(login);
    now = 9;
    expect(guardSessionRequest(new Request("https://app.example/api", {
      method: "POST",
      headers: { cookie: `__Host-iotd_session=${badOriginSession.id}`, origin: "https://other.example", "x-csrf-token": badOriginSession.csrfToken },
    }), badOriginStore, origin)).toEqual({ ok: false, reason: "invalid_origin" });
    now = 10;
    expect(badOriginStore.read(badOriginSession.id)).toBeUndefined();

    now = 0;
    const badCsrfStore = createStore();
    const badCsrfSession = badCsrfStore.create(login);
    now = 9;
    expect(guardSessionRequest(new Request("https://app.example/api", {
      method: "POST",
      headers: { cookie: `__Host-iotd_session=${badCsrfSession.id}`, origin, "x-csrf-token": "wrong-token" },
    }), badCsrfStore, origin)).toEqual({ ok: false, reason: "invalid_csrf" });
    now = 10;
    expect(badCsrfStore.read(badCsrfSession.id)).toBeUndefined();

    now = 0;
    const allowedStore = createStore();
    const allowedSession = allowedStore.create(login);
    now = 9;
    expect(guardSessionRequest(new Request("https://app.example/auth/session", {
      headers: { cookie: `__Host-iotd_session=${allowedSession.id}` },
    }), allowedStore)).toMatchObject({ ok: true });
    now = 18;
    expect(allowedStore.read(allowedSession.id)).toBeDefined();
    now = 30;
    expect(allowedStore.read(allowedSession.id)).toBeUndefined();
  });
});

describe("session and CSRF guards", () => {
  it("permits a valid session on safe methods and requires exact origin plus CSRF for unsafe methods", () => {
    const store = new InMemoryServerSessionStore();
    const session = store.create(login);
    const cookie = `__Host-iotd_session=${session.id}`;
    expect(guardSessionRequest(new Request("https://app.example/auth/session", { headers: { cookie } }), store)).toMatchObject({ ok: true });
    expect(guardSessionRequest(new Request("https://app.example/api", { method: "POST", headers: { cookie } }), store, "https://app.example")).toEqual({ ok: false, reason: "invalid_origin" });
    expect(guardSessionRequest(new Request("https://app.example/api", { method: "POST", headers: { cookie, origin: "https://other.example", "x-csrf-token": session.csrfToken } }), store, "https://app.example")).toEqual({ ok: false, reason: "invalid_origin" });
    expect(guardSessionRequest(new Request("https://app.example/api", { method: "POST", headers: { cookie, origin: "https://app.example", "x-csrf-token": "wrong-token" } }), store, "https://app.example")).toEqual({ ok: false, reason: "invalid_csrf" });
    expect(guardSessionRequest(new Request("https://app.example/api", { method: "POST", headers: { cookie, origin: "https://app.example", "x-csrf-token": session.csrfToken } }), store, "https://app.example")).toMatchObject({ ok: true });
  });

  it("ignores unrelated browser cookies while still rejecting repeated or malformed target cookies", () => {
    const store = new InMemoryServerSessionStore();
    const session = store.create(login);
    const accepted = guardSessionRequest(new Request("https://app.example/auth/session", {
      headers: { cookie: `analytics=abc==; empty=; __Host-iotd_session=${session.id}` },
    }), store);
    expect(accepted).toMatchObject({ ok: true });

    const malformed = guardSessionRequest(new Request("https://app.example/auth/session", {
      headers: { cookie: "__Host-iotd_session=not/opaque" },
    }), store);
    expect(malformed).toEqual({ ok: false, reason: "unauthenticated" });
    const repeated = guardSessionRequest(new Request("https://app.example/auth/session", {
      headers: { cookie: `__Host-iotd_session=${session.id}; __Host-iotd_session=${session.id}` },
    }), store);
    expect(repeated).toEqual({ ok: false, reason: "unauthenticated" });
    const bareThenValid = guardSessionRequest(new Request("https://app.example/auth/session", {
      headers: { cookie: `__Host-iotd_session; __Host-iotd_session=${session.id}` },
    }), store);
    expect(bareThenValid).toEqual({ ok: false, reason: "unauthenticated" });
  });
});

describe("auth session routes", () => {
  it("returns only an authentication flag and CSRF token for a valid session", async () => {
    const session = serverSessions.create(login);
    const response = await getSession(new Request("https://app.example/auth/session", { headers: { cookie: `__Host-iotd_session=${session.id}` } }));
    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toContain("no-store");
    expect(response.headers.get("vary")).toBe("Cookie");
    await expect(response.json()).resolves.toEqual({ authenticated: true, csrfToken: session.csrfToken });
  });

  it("rejects unknown and repeated session cookies without exposing identity data", async () => {
    const valid = serverSessions.create(login);
    const unknown = await getSession(new Request("https://app.example/auth/session", { headers: { cookie: "__Host-iotd_session=unknown-session-value-that-is-long-enough" } }));
    expect(unknown.status).toBe(401);
    await expect(unknown.json()).resolves.toEqual({ error: "unauthenticated" });
    expect(unknown.headers.get("set-cookie")).toContain("__Host-iotd_session=; Max-Age=0;");

    const repeated = await getSession(new Request("https://app.example/auth/session", { headers: { cookie: `__Host-iotd_session=${valid.id}; __Host-iotd_session=${valid.id}` } }));
    expect(repeated.status).toBe(401);
    await expect(repeated.json()).resolves.toEqual({ error: "unauthenticated" });
  });

  it("clears the Cookie for expired and revoked sessions", async () => {
    const clock = vi.spyOn(Date, "now");
    try {
      clock.mockReturnValue(0);
      const expired = serverSessions.create(login);
      clock.mockReturnValue(30 * 60 * 1_000);
      const expiredResponse = await getSession(new Request("https://app.example/auth/session", { headers: { cookie: `__Host-iotd_session=${expired.id}` } }));
      expect(expiredResponse.status).toBe(401);
      expect(expiredResponse.headers.get("set-cookie")).toContain("__Host-iotd_session=; Max-Age=0;");
    } finally {
      clock.mockRestore();
    }

    const revoked = serverSessions.create(login);
    serverSessions.revoke(revoked.id);
    const revokedResponse = await getSession(new Request("https://app.example/auth/session", { headers: { cookie: `__Host-iotd_session=${revoked.id}` } }));
    expect(revokedResponse.status).toBe(401);
    expect(revokedResponse.headers.get("set-cookie")).toContain("__Host-iotd_session=; Max-Age=0;");
  });

  it("logs out only with the exact trusted Origin and CSRF token, then revokes the old session", async () => {
    await withOidcEnvironment(async () => {
      const session = serverSessions.create(login);
      const cookie = `__Host-iotd_session=${session.id}`;
      const missingOrigin = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, "x-csrf-token": session.csrfToken },
      }));
      expect(missingOrigin.status).toBe(403);
      const malformedOrigin = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, origin: "not-an-origin", "x-csrf-token": session.csrfToken },
      }));
      expect(malformedOrigin.status).toBe(403);
      const crossOrigin = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, origin: "https://cross-origin.example", "x-csrf-token": session.csrfToken },
      }));
      expect(crossOrigin.status).toBe(403);
      const rejected = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, origin: "http://127.0.0.1:5173", "x-csrf-token": "incorrect" },
      }));
      expect(rejected.status).toBe(403);
      expect((await getSession(new Request("http://127.0.0.1:5173/auth/session", { headers: { cookie } }))).status).toBe(200);

      const missingCsrf = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, origin: "http://127.0.0.1:5173" },
      }));
      expect(missingCsrf.status).toBe(403);

      const loggedOut = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie, origin: "http://127.0.0.1:5173", "x-csrf-token": session.csrfToken },
      }));
      expect(loggedOut.status).toBe(204);
      expect(loggedOut.headers.get("cache-control")).toContain("no-store");
      expect(loggedOut.headers.get("set-cookie")).toContain("__Host-iotd_session=; Max-Age=0;");
      expect((await getSession(new Request("http://127.0.0.1:5173/auth/session", { headers: { cookie } }))).status).toBe(401);

      const unknown = await logout(new Request("http://127.0.0.1:5173/auth/logout", {
        method: "POST",
        headers: { cookie: "__Host-iotd_session=unknown-session-value-that-is-long-enough", origin: "http://127.0.0.1:5173", "x-csrf-token": session.csrfToken },
      }));
      expect(unknown.status).toBe(401);

      const logoutModule = await import("@/app/auth/logout/route");
      expect("GET" in logoutModule).toBe(false);
    });
  });
});

async function withOidcEnvironment(action: () => Promise<void>) {
  const priorEnvironment = { ...process.env };
  Object.assign(process.env, {
    OIDC_ISSUER: "http://127.0.0.1:9000",
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
