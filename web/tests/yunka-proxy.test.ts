import { describe, expect, it } from "vitest";

import { buildRuntimeUrl, createRuntimeRequest } from "@/lib/server/runtime-proxy";

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
});
