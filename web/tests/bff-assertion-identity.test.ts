import { describe, expect, it } from "vitest";

import { BffAssertionConfigurationError, createBffAssertionHeaders } from "@/lib/server/bff-assertion";

const environment = { IOT_DELIVERY_BFF_ASSERTION_KEY: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE" };

describe("BFF assertion identity keys", () => {
  it("rejects a subject with leading or trailing whitespace instead of normalizing it", () => {
    for (const subject of [" user", "user "]) {
      expect(() => createBffAssertionHeaders({ issuer: "https://issuer.example", subject }, "GET", "/api/items", undefined, environment)).toThrow(BffAssertionConfigurationError);
    }
  });

  it("rejects C1 control characters in an identity key", () => {
    expect(() => createBffAssertionHeaders({ issuer: "https://issuer.example", subject: "user\u0085one" }, "GET", "/api/items", undefined, environment)).toThrow(BffAssertionConfigurationError);
  });
});
