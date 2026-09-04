import { randomBytes } from "node:crypto";

import { afterEach, describe, expect, it, vi } from "vitest";

import { POST } from "@/app/api/[...path]/route";
import { serverSessions } from "@/lib/server/session";

const assertionKey = randomBytes(32).toString("base64url");
const login = {
  issuer: "https://issuer.example/tenant",
  subject: "external-user-1",
  email: "person@example.test",
  displayName: "Person One",
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("authenticated BFF API route", () => {
  it("rejects a missing session without contacting the upstream", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", { method: "POST" }), routeContext(["items"]));

    expect(response.status).toBe(401);
    expect(response.headers.get("cache-control")).toContain("no-store");
    const body = await response.json() as { error: string; traceId?: string };
    expect(body.error).toBe("unauthenticated");
    expect(body.traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(response.headers.get("x-trace-id")).toBe(body.traceId);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("rejects a forged unsafe request without contacting the upstream", async () => {
    configureBff();
    const session = serverSessions.create(login);
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: { cookie: `__Host-iotd_session=${session.id}`, origin: "https://attacker.example", "x-csrf-token": session.csrfToken },
      body: JSON.stringify({ title: "must not reach upstream" }),
    }), routeContext(["items"]));

    expect(response.status).toBe(403);
    expect(response.headers.get("cache-control")).toContain("no-store");
    const body = await response.json() as { error: string; traceId?: string };
    expect(body.error).toBe("forbidden");
    expect(body.traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(response.headers.get("x-trace-id")).toBe(body.traceId);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("strips browser-controlled internal headers and forwards only server generated credentials", async () => {
    configureBff();
    const session = serverSessions.create(login);
    const fetcher = vi.fn(async (request: Request) => new Response(JSON.stringify({ ok: true }), { headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items?projectId=P-1", {
      method: "POST",
      headers: {
        cookie: `__Host-iotd_session=${session.id}`,
        origin: "https://app.example",
        "x-csrf-token": session.csrfToken,
        "x-api-key": "browser-key",
        "x-iot-delivery-assertion": "forged",
        "x-iot-delivery-assertion-signature": "forged",
        "x-trace-id": "forged",
      },
      body: JSON.stringify({ title: "authenticated mutation" }),
    }), routeContext(["items"]));

    expect(response.status).toBe(200);
    expect(fetcher).toHaveBeenCalledOnce();
    const upstream = fetcher.mock.calls[0]?.[0] as Request;
    expect(upstream.headers.get("x-api-key")).toBe("server-bff-channel-key");
    expect(upstream.headers.get("x-iot-delivery-assertion")).not.toBe("forged");
    expect(upstream.headers.get("x-iot-delivery-assertion-signature")).not.toBe("forged");
    expect(upstream.headers.get("x-trace-id")).toMatch(/^[0-9a-f]{32}$/);
    expect(await upstream.text()).toBe(JSON.stringify({ title: "authenticated mutation" }));
  });

  it("returns a traced 503 without forwarding when BFF configuration is incomplete", async () => {
    configureBff();
    vi.stubEnv("IOT_DELIVERY_BFF_ASSERTION_KEY", "");
    const session = serverSessions.create(login);
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: { cookie: `__Host-iotd_session=${session.id}`, origin: "https://app.example", "x-csrf-token": session.csrfToken },
      body: JSON.stringify({ title: "must not forward" }),
    }), routeContext(["items"]));

    expectTracedError(response, 503, "service_unavailable");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("returns a traced 502 when the upstream cannot be reached", async () => {
    configureBff();
    const session = serverSessions.create(login);
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("network failure"); }));

    const response = await POST(new Request("https://app.example/api/items", {
      method: "POST",
      headers: { cookie: `__Host-iotd_session=${session.id}`, origin: "https://app.example", "x-csrf-token": session.csrfToken },
      body: JSON.stringify({ title: "upstream failure" }),
    }), routeContext(["items"]));

    expectTracedError(response, 502, "upstream_unavailable");
  });
});

function routeContext(path: string[]) {
  return { params: Promise.resolve({ path }) };
}

function configureBff() {
  vi.stubEnv("OIDC_ISSUER", "https://issuer.example");
  vi.stubEnv("OIDC_CLIENT_ID", "test-client");
  vi.stubEnv("OIDC_CLIENT_SECRET", "test-secret");
  vi.stubEnv("OIDC_REDIRECT_URI", "https://app.example/auth/callback");
  vi.stubEnv("IOT_DELIVERY_LOCAL_API_KEY", "server-bff-channel-key");
  vi.stubEnv("IOT_DELIVERY_BFF_ASSERTION_KEY", assertionKey);
}

async function expectTracedError(response: Response, status: number, error: string) {
  expect(response.status).toBe(status);
  expect(response.headers.get("cache-control")).toContain("no-store");
  const body = await response.json() as { error: string; traceId?: string };
  expect(body.error).toBe(error);
  expect(body.traceId).toMatch(/^[0-9a-f]{32}$/);
  expect(response.headers.get("x-trace-id")).toBe(body.traceId);
}
