import { randomBytes } from "node:crypto";

import { afterEach, describe, expect, it, vi } from "vitest";

import { forwardLocalAuthBrowserRequest } from "@/lib/server/local-auth-forwarder";
import { LOCAL_CSRF_COOKIE_NAME, LOCAL_SESSION_COOKIE_NAME } from "@/lib/server/local-auth-session";

const localSession = randomBytes(32).toString("base64url");
const localCsrf = randomBytes(32).toString("base64url");
const accessToken = `${randomBytes(24).toString("base64url")}.${randomBytes(24).toString("base64url")}.${randomBytes(24).toString("base64url")}`;
const environment = { IOT_DELIVERY_API_TARGET: "http://127.0.0.1:8281" };

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("YU-28 local auth browser forwarder", () => {
  it("validates the browser Origin, rewrites only the server hop Origin, and strips browser credentials", async () => {
    const observed: Request[] = [];
    const responseHeaders = new Headers({ "content-type": "application/json", "cache-control": "no-store" });
    responseHeaders.append("set-cookie", `${LOCAL_SESSION_COOKIE_NAME}=${localSession}; Path=/; HttpOnly; Secure; SameSite=Strict`);
    responseHeaders.append("set-cookie", `${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}; Path=/; Secure; SameSite=Strict`);
    const fetcher = vi.fn(async (request: Request) => {
      observed.push(request);
      return new Response(JSON.stringify({
        authenticated: true,
        organizationId: "org-a",
        userId: "user-a",
        accessToken,
        accessExpiresAt: "2026-09-05T12:00:00Z",
        csrfToken: localCsrf,
      }), { status: 200, headers: responseHeaders });
    });

    const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/login", {
      method: "POST",
      headers: {
        origin: "https://app.example",
        "content-type": "application/json",
        authorization: "Bearer browser-token",
        "x-api-key": "browser-key",
        cookie: "analytics=1; __Host-iotd_session=old-oidc-session-value-that-is-long-enough",
      },
      body: JSON.stringify({ organizationId: "org-a", userId: "user-a", password: "secret" }),
    }), ["login"], environment, fetcher as typeof fetch);

    expect(response.status).toBe(200);
    expect(observed).toHaveLength(1);
    const upstream = observed[0];
    expect(upstream.url).toBe("http://127.0.0.1:8281/auth/local/login");
    expect(upstream.headers.get("origin")).toBe("http://127.0.0.1:8281");
    expect(upstream.headers.get("authorization")).toBeNull();
    expect(upstream.headers.get("x-api-key")).toBeNull();
    expect(upstream.headers.get("cookie")).toBeNull();
    expect(await upstream.text()).toBe(JSON.stringify({ organizationId: "org-a", userId: "user-a", password: "secret" }));
    await expect(response.json()).resolves.toEqual({ authenticated: true, organizationId: "org-a", userId: "user-a" });
    expect(response.headers.get("set-cookie")).toContain(LOCAL_SESSION_COOKIE_NAME);
    expect(response.headers.get("set-cookie")).toContain(LOCAL_CSRF_COOKIE_NAME);
  });

  it("forwards only exact local cookies and current CSRF to protected YU-26 admin routes", async () => {
    let upstream: Request | undefined;
    const fetcher = vi.fn(async (request: Request) => {
      upstream = request;
      return Response.json({ UserID: "user-b", UserRevision: 1, CredentialRevision: 1 }, { status: 201 });
    });

    const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/admin/members", {
      method: "POST",
      headers: {
        origin: "https://app.example",
        "content-type": "application/json",
        "x-csrf-token": localCsrf,
        authorization: "Bearer forged-browser-token",
        "x-api-key": "forged-browser-key",
        cookie: `analytics=1; ${LOCAL_SESSION_COOKIE_NAME}=${localSession}; ${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}; __Host-iotd_session=oidc-session-value-that-is-long-enough`,
      },
      body: JSON.stringify({ displayName: "Member B", email: "", password: "temporary" }),
    }), ["admin", "members"], environment, fetcher as typeof fetch);

    expect(response.status).toBe(201);
    expect(upstream).toBeDefined();
    expect(upstream?.headers.get("cookie")).toBe(`${LOCAL_SESSION_COOKIE_NAME}=${localSession}; ${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}`);
    expect(upstream?.headers.get("x-csrf-token")).toBe(localCsrf);
    expect(upstream?.headers.get("origin")).toBe("http://127.0.0.1:8281");
    expect(upstream?.headers.get("authorization")).toBeNull();
    expect(upstream?.headers.get("x-api-key")).toBeNull();
  });

  it("rejects a cross-origin unsafe browser request before the runtime is contacted", async () => {
    const fetcher = vi.fn();
    const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/logout", {
      method: "POST",
      headers: {
        origin: "https://attacker.example",
        cookie: `${LOCAL_SESSION_COOKIE_NAME}=${localSession}; ${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}`,
        "x-csrf-token": localCsrf,
      },
    }), ["logout"], environment, fetcher as typeof fetch);

    expect(response.status).toBe(403);
    await expect(response.json()).resolves.toMatchObject({ error: "forbidden" });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("keeps current-member profile data but never exposes the renewed access JWT or CSRF token to UI code", async () => {
    const fetcher = vi.fn(async () => Response.json({
      authenticated: true,
      organizationId: "org-a",
      userId: "user-a",
      displayName: "User A",
      email: "user-a@example.test",
      userRevision: 3,
      sessionRevision: 4,
      accessToken,
      accessExpiresAt: "2026-09-05T12:00:00Z",
      csrfToken: localCsrf,
    }));

    const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/current", {
      headers: { cookie: `${LOCAL_SESSION_COOKIE_NAME}=${localSession}; ${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}` },
    }), ["current"], environment, fetcher as typeof fetch);

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toEqual({
      authenticated: true,
      organizationId: "org-a",
      userId: "user-a",
      displayName: "User A",
      email: "user-a@example.test",
      userRevision: 3,
      sessionRevision: 4,
    });
  });

  it("maps a runtime transport failure to a stable non-cacheable 503", async () => {
    const fetcher = vi.fn(async () => { throw new Error("runtime down"); });
    const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/current", {
      headers: { cookie: `${LOCAL_SESSION_COOKIE_NAME}=${localSession}` },
    }), ["current"], environment, fetcher as typeof fetch);

    expect(response.status).toBe(503);
    expect(response.headers.get("cache-control")).toContain("no-store");
    await expect(response.json()).resolves.toMatchObject({ error: "service_unavailable" });
  });
});
