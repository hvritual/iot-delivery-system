import { expect, it } from "vitest";
import { forwardLocalAuthBrowserRequest } from "@/lib/server/local-auth-forwarder";

it("YU32H preserves 429 Retry-After and never forwards browser-supplied source identity", async () => {
  const fetcher: typeof fetch = async (input) => {
    const upstream = input as Request;
    for (const header of ["forwarded", "x-forwarded-for", "x-real-ip"]) expect(upstream.headers.get(header)).toBeNull();
    return Response.json({ error: "too_many_attempts", traceId: "test" }, { status: 429, headers: { "retry-after": "900", "cache-control": "no-store" } });
  };
  const response = await forwardLocalAuthBrowserRequest(new Request("https://app.example/auth/local/login", {
    method: "POST", headers: { origin: "https://app.example", "x-forwarded-for": "192.0.2.1", "x-real-ip": "192.0.2.2", forwarded: "for=192.0.2.3" },
    body: JSON.stringify({ organizationId: "org-a", userId: "user-a", password: "a secret" }),
  }), ["login"], { IOT_DELIVERY_API_TARGET: "http://127.0.0.1:8281" }, fetcher);
  expect(response.status).toBe(429);
  expect(response.headers.get("retry-after")).toBe("900");
  expect(response.headers.get("cache-control")).toBe("no-store");
  expect(response.headers.get("set-cookie")).toBeNull();
  expect(await response.json()).toEqual({ error: "too_many_attempts", traceId: "test" });
});
