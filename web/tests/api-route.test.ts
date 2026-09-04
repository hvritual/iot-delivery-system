import { once } from "node:events";
import { createServer, type Server } from "node:http";
import { randomBytes } from "node:crypto";

import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { POST } from "@/app/api/[...path]/route";
import { serverSessions } from "@/lib/server/session";

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
    const priorAssertionKey = process.env.IOT_DELIVERY_BFF_ASSERTION_KEY;
    const priorIssuer = process.env.OIDC_ISSUER;
    const priorClientID = process.env.OIDC_CLIENT_ID;
    const priorClientSecret = process.env.OIDC_CLIENT_SECRET;
    const priorRedirectURI = process.env.OIDC_REDIRECT_URI;
    const priorAllowInsecure = process.env.OIDC_ALLOW_INSECURE_TEST_HTTP;
    process.env.IOT_DELIVERY_API_TARGET = origin;
    process.env.IOT_DELIVERY_LOCAL_API_KEY = "route-test-key";
    process.env.IOT_DELIVERY_BFF_ASSERTION_KEY = randomBytes(32).toString("base64url");
    process.env.OIDC_ISSUER = "https://issuer.example";
    process.env.OIDC_CLIENT_ID = "route-test-client";
    process.env.OIDC_CLIENT_SECRET = "route-test-secret";
    process.env.OIDC_REDIRECT_URI = "http://127.0.0.1:5173/auth/callback";
    process.env.OIDC_ALLOW_INSECURE_TEST_HTTP = "1";
    const session = serverSessions.create({ issuer: "https://issuer.example", subject: "route-test-user" });

    try {
      const response = await POST(
        new Request("http://127.0.0.1:5173/api/items?projectId=PRJ-1", {
          method: "POST",
          body: JSON.stringify({ title: "保留 API 路径" }),
          headers: { "content-type": "application/json", cookie: `__Host-iotd_session=${session.id}`, origin: "http://127.0.0.1:5173", "x-csrf-token": session.csrfToken },
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
      if (priorAssertionKey === undefined) delete process.env.IOT_DELIVERY_BFF_ASSERTION_KEY;
      else process.env.IOT_DELIVERY_BFF_ASSERTION_KEY = priorAssertionKey;
      if (priorIssuer === undefined) delete process.env.OIDC_ISSUER;
      else process.env.OIDC_ISSUER = priorIssuer;
      if (priorClientID === undefined) delete process.env.OIDC_CLIENT_ID;
      else process.env.OIDC_CLIENT_ID = priorClientID;
      if (priorClientSecret === undefined) delete process.env.OIDC_CLIENT_SECRET;
      else process.env.OIDC_CLIENT_SECRET = priorClientSecret;
      if (priorRedirectURI === undefined) delete process.env.OIDC_REDIRECT_URI;
      else process.env.OIDC_REDIRECT_URI = priorRedirectURI;
      if (priorAllowInsecure === undefined) delete process.env.OIDC_ALLOW_INSECURE_TEST_HTTP;
      else process.env.OIDC_ALLOW_INSECURE_TEST_HTTP = priorAllowInsecure;
    }
  });
});
