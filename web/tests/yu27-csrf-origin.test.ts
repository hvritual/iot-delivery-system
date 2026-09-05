import { randomBytes } from "node:crypto";

import { afterEach, describe, expect, it, vi } from "vitest";

import { GET, POST } from "@/app/api/[...path]/route";
import { GET as getAuthSession } from "@/app/auth/session/route";
import { LOCAL_CSRF_COOKIE_NAME, LOCAL_SESSION_COOKIE_NAME } from "@/lib/server/local-auth-session";
import { serverSessions, SESSION_COOKIE_NAME } from "@/lib/server/session";

const localSession = randomBytes(32).toString("base64url");
const localCsrf = randomBytes(32).toString("base64url");
const localAccess = `${randomBytes(24).toString("base64url")}.${randomBytes(24).toString("base64url")}.${randomBytes(24).toString("base64url")}`;
const localCookieHeader = `${LOCAL_SESSION_COOKIE_NAME}=${localSession}; ${LOCAL_CSRF_COOKIE_NAME}=${localCsrf}`;

const localCurrent = {
  authenticated: true,
  organizationId: "org-a",
  userId: "user-a",
  accessToken: localAccess,
  csrfToken: localCsrf,
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("YU-27 API session source, Origin and CSRF", () => {
  it("permits an unsafe OIDC-session API request without reading OIDC configuration", async () => {
    vi.stubEnv("OIDC_ISSUER", "");
    vi.stubEnv("OIDC_CLIENT_ID", "");
    vi.stubEnv("OIDC_CLIENT_SECRET", "");
    vi.stubEnv("OIDC_REDIRECT_URI", "");
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    vi.stubEnv("IOT_DELIVERY_LOCAL_API_KEY", "bff-channel-key");
    vi.stubEnv("IOT_DELIVERY_BFF_ASSERTION_KEY", randomBytes(32).toString("base64url"));
    const oidc = serverSessions.create({ issuer: "https://issuer.example", subject: "subject-a" });
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: {
        cookie: `${SESSION_COOKIE_NAME}=${oidc.id}`,
        origin: "https://app.example",
        "x-csrf-token": oidc.csrfToken,
        "content-type": "application/json",
      },
      body: JSON.stringify({ title: "OIDC origin no longer config-coupled" }),
    }), routeContext(["items"]));

    expect(response.status).toBe(200);
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("uses YU-26 current server-side and forwards only its local access JWT", async () => {
    vi.stubEnv("OIDC_REDIRECT_URI", "");
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    vi.stubEnv("IOT_DELIVERY_LOCAL_API_KEY", "development-key-must-not-mix");
    const observed: Request[] = [];
    const fetcher = vi.fn(async (request: Request) => {
      observed.push(request);
      if (new URL(request.url).pathname === "/auth/local/current") {
        return Response.json(localCurrent, { headers: { "cache-control": "no-store" } });
      }
      return Response.json({ accepted: true });
    });
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: {
        cookie: localCookieHeader,
        origin: "https://app.example",
        "x-csrf-token": localCsrf,
        "content-type": "application/json",
        authorization: "Bearer browser-must-not-win",
        "x-api-key": "browser-must-not-win",
      },
      body: JSON.stringify({ title: "local mutation" }),
    }), routeContext(["items"]));

    expect(response.status).toBe(200);
    expect(observed).toHaveLength(2);
    const current = observed[0];
    const upstream = observed[1];
    expect(new URL(current.url).pathname).toBe("/auth/local/current");
    expect(current.headers.get("cookie")).toBe(localCookieHeader);
    expect(current.headers.get("authorization")).toBeNull();
    expect(new URL(upstream.url).pathname).toBe("/api/items");
    expect(upstream.headers.get("authorization")).toBe(`Bearer ${localAccess}`);
    expect(upstream.headers.get("x-api-key")).toBeNull();
    expect(upstream.headers.get("x-iot-delivery-assertion")).toBeNull();
    expect(upstream.headers.get("cookie")).toBeNull();
    expect(upstream.headers.get("x-csrf-token")).toBeNull();
  });

  it("returns stable 403 for a stale local CSRF token before the mutation reaches upstream", async () => {
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    const fetcher = vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/auth/local/current") return Response.json(localCurrent);
      throw new Error("mutation must not be contacted");
    });
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: {
        cookie: localCookieHeader,
        origin: "https://app.example",
        "x-csrf-token": randomBytes(32).toString("base64url"),
      },
      body: "{}",
    }), routeContext(["items"]));

    expect(response.status).toBe(403);
    await expect(response.json()).resolves.toMatchObject({ error: "forbidden" });
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("does not require Origin or CSRF headers on a safe local API request", async () => {
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    const fetcher = vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === "/auth/local/current") return Response.json(localCurrent);
      return Response.json({ safe: true });
    });
    vi.stubGlobal("fetch", fetcher);

    const response = await GET(new Request("https://app.example/api/projects", {
      headers: { cookie: localCookieHeader },
    }), routeContext(["projects"]));

    expect(response.status).toBe(200);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("rejects mixed local and OIDC session families before contacting upstream", async () => {
    const oidc = serverSessions.create({ issuer: "https://issuer.example", subject: "subject-mixed" });
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);

    const response = await GET(new Request("https://app.example/api/projects", {
      headers: { cookie: `${SESSION_COOKIE_NAME}=${oidc.id}; ${localCookieHeader}` },
    }), routeContext(["projects"]));

    expect(response.status).toBe(401);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("returns local CSRF through /auth/session without exposing the server-side access JWT", async () => {
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    const fetcher = vi.fn(async (request: Request) => {
      expect(request.headers.get("cookie")).toBe(localCookieHeader);
      return Response.json(localCurrent);
    });
    vi.stubGlobal("fetch", fetcher);

    const response = await getAuthSession(new Request("https://app.example/auth/session", {
      headers: { cookie: localCookieHeader },
    }));

    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toContain("no-store");
    await expect(response.json()).resolves.toEqual({ authenticated: true, csrfToken: localCsrf });
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("propagates a regenerated YU-26 CSRF cookie when the browser lost it", async () => {
    vi.stubEnv("IOT_DELIVERY_API_TARGET", "https://runtime.example");
    const replacement = randomBytes(32).toString("base64url");
    const setCookie = `${LOCAL_CSRF_COOKIE_NAME}=${replacement}; Path=/; Secure; SameSite=Strict`;
    vi.stubGlobal("fetch", vi.fn(async () => Response.json(
      { ...localCurrent, csrfToken: replacement },
      { headers: { "set-cookie": setCookie } },
    )));

    const response = await getAuthSession(new Request("https://app.example/auth/session", {
      headers: { cookie: `${LOCAL_SESSION_COOKIE_NAME}=${localSession}` },
    }));

    expect(response.status).toBe(200);
    expect(response.headers.get("set-cookie")).toContain(`${LOCAL_CSRF_COOKIE_NAME}=${replacement}`);
    await expect(response.json()).resolves.toEqual({ authenticated: true, csrfToken: replacement });
  });
});

function routeContext(path: string[]) {
  return { params: Promise.resolve({ path }) };
}
