import { describe, expect, it } from "vitest";

import { BFF_ASSERTION_HEADER, BFF_ASSERTION_SIGNATURE_HEADER, createBffAssertionHeaders } from "@/lib/server/bff-assertion";

describe("BFF assertion cross-language vector", () => {
  it("emits the fixed v1 payload and signature accepted by the Go verifier", () => {
    const headers = createBffAssertionHeaders(
      { issuer: "https://issuer.example/tenant", subject: "user-1", email: "user@example.test", displayName: "User One" },
      "POST",
      "/api/items?projectId=P-1",
      new TextEncoder().encode('{"title":"vector"}'),
      { IOT_DELIVERY_BFF_ASSERTION_KEY: "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE" },
      1_788_422_400,
      "11111111111111111111111111111111",
      "nonce-vector-0001",
    );
    expect(headers.get(BFF_ASSERTION_HEADER)).toBe("eyJ2IjoxLCJpc3N1ZXIiOiJodHRwczovL2lzc3Vlci5leGFtcGxlL3RlbmFudCIsInN1YmplY3QiOiJ1c2VyLTEiLCJlbWFpbCI6InVzZXJAZXhhbXBsZS50ZXN0IiwiZGlzcGxheU5hbWUiOiJVc2VyIE9uZSIsIm5vbmNlIjoibm9uY2UtdmVjdG9yLTAwMDEiLCJ0cmFjZUlkIjoiMTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTEiLCJtZXRob2QiOiJQT1NUIiwicGF0aCI6Ii9hcGkvaXRlbXM_cHJvamVjdElkPVAtMSIsImJvZHlTaGEyNTYiOiJmMjcxODNiY2Y2Nzg2YjgxOTMxMTJiNWRkMDQ2ZDUxZGI5ZmQ1ZDE1OTA3NjkxN2RjODUwYTc2MmE1Y2UyZTkxIiwiaWF0IjoxNzg4NDIyNDAwLCJleHAiOjE3ODg0MjI0NjB9");
    expect(headers.get(BFF_ASSERTION_SIGNATURE_HEADER)).toBe("L8v1Jb1YMnmCUoCy-0UN6Hy7jx1SPFoRHGA457yuO7k");
  });
});
