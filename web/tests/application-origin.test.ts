import { describe, expect, it } from "vitest";

import { ApplicationOriginConfigurationError, resolveTrustedApplicationOrigin } from "@/lib/server/application-origin";

describe("YU-27 trusted application origin", () => {
  it("derives canonical HTTPS and loopback HTTP origins without OIDC configuration", () => {
    expect(resolveTrustedApplicationOrigin(new Request("https://app.example/api/items"), {})).toBe("https://app.example");
    expect(resolveTrustedApplicationOrigin(new Request("http://127.0.0.1:5173/api/items"), {})).toBe("http://127.0.0.1:5173");
    expect(resolveTrustedApplicationOrigin(new Request("http://localhost:5173/api/items"), {})).toBe("http://localhost:5173");
  });

  it("rejects non-loopback plaintext origins", () => {
    expect(() => resolveTrustedApplicationOrigin(new Request("http://app.example/api/items"), {})).toThrow(ApplicationOriginConfigurationError);
  });

  it("requires an explicitly configured web origin to match the request origin exactly", () => {
    expect(resolveTrustedApplicationOrigin(
      new Request("https://app.example/api/items"),
      { IOT_DELIVERY_WEB_ORIGIN: "https://app.example" },
    )).toBe("https://app.example");
    expect(() => resolveTrustedApplicationOrigin(
      new Request("https://app.example/api/items"),
      { IOT_DELIVERY_WEB_ORIGIN: "https://other.example" },
    )).toThrow(ApplicationOriginConfigurationError);
  });
});
