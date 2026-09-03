import { once } from "node:events";
import { createServer, type Server } from "node:http";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { POST } from "@/app/api/[...path]/route";

let server: Server;
let origin = "";
let observedPath = "";

beforeAll(async () => {
  server = createServer((request, response) => {
    observedPath = request.url ?? "";
    response.writeHead(202, { "content-type": "application/json" });
    response.end(JSON.stringify({ proxied: true }));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("test server did not expose a TCP address");
  origin = `http://127.0.0.1:${address.port}`;
});

afterAll(async () => {
  server.close();
  await once(server, "close");
});

describe("Next API catch-all route", () => {
  it("keeps the browser API path stable while forwarding to Yunka", async () => {
    const priorTarget = process.env.IOT_DELIVERY_API_TARGET;
    const priorKey = process.env.IOT_DELIVERY_LOCAL_API_KEY;
    process.env.IOT_DELIVERY_API_TARGET = origin;
    process.env.IOT_DELIVERY_LOCAL_API_KEY = "route-test-key";

    try {
      const response = await POST(
        new Request("http://127.0.0.1:5173/api/items?projectId=PRJ-1", {
          method: "POST",
          body: JSON.stringify({ title: "保留 API 路径" }),
          headers: { "content-type": "application/json" },
        }),
        { params: Promise.resolve({ path: ["items"] }) },
      );

      expect(observedPath).toBe("/api/items?projectId=PRJ-1");
      expect(response.status).toBe(202);
      await expect(response.json()).resolves.toEqual({ proxied: true });
    } finally {
      if (priorTarget === undefined) delete process.env.IOT_DELIVERY_API_TARGET;
      else process.env.IOT_DELIVERY_API_TARGET = priorTarget;
      if (priorKey === undefined) delete process.env.IOT_DELIVERY_LOCAL_API_KEY;
      else process.env.IOT_DELIVERY_LOCAL_API_KEY = priorKey;
    }
  });
});
