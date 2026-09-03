import { createServer, type IncomingMessage, type Server } from "node:http";
import { once } from "node:events";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { forwardRuntimeRequest } from "@/lib/server/runtime-forwarder";

let server: Server;
let origin = "";
let observed: { body: string; method: string; path: string; apiKey: string | undefined } | undefined;

function readBody(request: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    request.on("error", reject);
  });
}

beforeAll(async () => {
  server = createServer(async (request, response) => {
    observed = {
      body: await readBody(request),
      method: request.method ?? "",
      path: request.url ?? "",
      apiKey: request.headers["x-api-key"] as string | undefined,
    };
    response.writeHead(201, { "content-type": "application/json", "x-runtime": "yunka" });
    response.end(JSON.stringify({ accepted: true }));
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

describe("runtime forwarding", () => {
  it("relays an authenticated mutation and preserves the upstream response", async () => {
    const response = await forwardRuntimeRequest(
      new Request("http://127.0.0.1:5173/api/items?projectId=PRJ-1", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ title: "Yunka 集成验收" }),
      }),
      ["api", "items"],
      { IOT_DELIVERY_API_TARGET: origin, IOT_DELIVERY_LOCAL_API_KEY: "server-only-key" },
    );

    expect(observed).toEqual({
      body: JSON.stringify({ title: "Yunka 集成验收" }),
      method: "POST",
      path: "/api/items?projectId=PRJ-1",
      apiKey: "server-only-key",
    });
    expect(response.status).toBe(201);
    expect(response.headers.get("x-runtime")).toBe("yunka");
    await expect(response.json()).resolves.toEqual({ accepted: true });
  });
});
