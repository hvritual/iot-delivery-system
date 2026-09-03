import { describe, expect, it } from "vitest";

import { buildRuntimeUrl, createRuntimeRequest } from "@/lib/server/runtime-proxy";
import { InMemoryServerSessionStore } from "@/lib/server/session";

describe("Next runtime proxy", () => {
  it("uses the isolated Yunka MVP as its default upstream", () => {
    expect(buildRuntimeUrl("/api/dashboard", {}, {})).toBe("http://127.0.0.1:8281/api/dashboard");
  });

  it("preserves the request path and keeps the local API key on the server", async () => {
    const request = new Request("http://127.0.0.1:5173/api/items?projectId=PRJ-1", {
      headers: { Accept: "application/json" },
    });

    const proxied = await createRuntimeRequest(request, ["api", "items"], {
      IOT_DELIVERY_API_TARGET: "http://127.0.0.1:8281/",
      IOT_DELIVERY_LOCAL_API_KEY: "server-only-key",
    });

    expect(proxied.url).toBe("http://127.0.0.1:8281/api/items?projectId=PRJ-1");
    expect(proxied.headers.get("x-api-key")).toBe("server-only-key");
    expect(proxied.headers.get("accept")).toBe("application/json");
  });

  it("forwards mutation payloads without accepting a browser-supplied local API key", async () => {
    const request = new Request("http://127.0.0.1:5173/api/items", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-API-Key": "untrusted-browser-key",
      },
      body: JSON.stringify({ title: "创建发布事项" }),
    });

    const proxied = await createRuntimeRequest(request, ["api", "items"], {
      IOT_DELIVERY_LOCAL_API_KEY: "server-only-key",
    });

    expect(proxied.method).toBe("POST");
    expect(proxied.headers.get("x-api-key")).toBe("server-only-key");
    expect(await proxied.text()).toBe(JSON.stringify({ title: "创建发布事项" }));
  });

  it("forwards only allowlisted business headers and never browser session or internal credentials", async () => {
    const session = new InMemoryServerSessionStore().create({ issuer: "https://issuer.example", subject: "subject-1" });
    const proxied = await createRuntimeRequest(new Request("https://app.example/api/items", {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        Cookie: `__Host-iotd_session=${session.id}`,
        Origin: "https://app.example",
        "X-CSRF-Token": session.csrfToken,
        Authorization: "Bearer browser-token",
        "X-API-Key": "browser-key",
        "X-IoT-Delivery-Assertion": "forged",
        "X-IoT-Delivery-Assertion-Signature": "forged",
        "X-Trace-ID": "00000000000000000000000000000000",
        Forwarded: "for=attacker.example",
        "X-Forwarded-For": "203.0.113.1",
        "X-Forwarded-Host": "attacker.example",
        "X-Forwarded-Proto": "https",
      },
      body: JSON.stringify({ title: "no browser credential leak" }),
    }), ["api", "items"], {
      IOT_DELIVERY_LOCAL_API_KEY: "server-key",
      IOT_DELIVERY_BFF_ASSERTION_KEY: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE",
    }, { issuer: "https://issuer.example", subject: "subject-1" });

    expect(proxied.headers.get("accept")).toBe("application/json");
    expect(proxied.headers.get("content-type")).toBe("application/json");
    expect(proxied.headers.get("x-api-key")).toBe("server-key");
    for (const header of ["cookie", "origin", "x-csrf-token", "authorization", "forwarded", "x-forwarded-for", "x-forwarded-host", "x-forwarded-proto"]) {
      expect(proxied.headers.get(header)).toBeNull();
    }
    expect(proxied.headers.get("x-iot-delivery-assertion")).not.toBe("forged");
    expect(proxied.headers.get("x-iot-delivery-assertion-signature")).not.toBe("forged");
    expect(proxied.headers.get("x-trace-id")).toMatch(/^[0-9a-f]{32}$/);
  });
});
