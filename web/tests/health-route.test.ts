import { once } from "node:events";
import { createServer, type Server } from "node:http";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { GET } from "@/app/health/route";

let server: Server;
let origin = "";
let observedPath = "";

beforeAll(async () => {
  server = createServer((request, response) => {
    observedPath = request.url ?? "";
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ status: "ok" }));
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

describe("Next health route", () => {
  it("continues to expose the Yunka health endpoint at the frontend origin", async () => {
    const priorTarget = process.env.IOT_DELIVERY_API_TARGET;
    process.env.IOT_DELIVERY_API_TARGET = origin;
    try {
      const response = await GET(new Request("http://127.0.0.1:5173/health"));
      expect(observedPath).toBe("/health");
      expect(response.status).toBe(200);
      await expect(response.json()).resolves.toEqual({ status: "ok" });
    } finally {
      if (priorTarget === undefined) delete process.env.IOT_DELIVERY_API_TARGET;
      else process.env.IOT_DELIVERY_API_TARGET = priorTarget;
    }
  });
});
