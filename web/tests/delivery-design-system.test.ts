import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));

function readProjectFile(path: string) {
  return readFileSync(
    new URL(path, `file://${projectRoot.replace(/\\/g, "/")}/`),
    "utf8",
  );
}

describe("delivery design system", () => {
  it("anchors the application shell in Inter with a target-owned Chinese fallback stack", () => {
    const layout = readProjectFile("app/layout.tsx");
    const css = readProjectFile("app/globals.css");

    expect(layout).toContain('import { Inter } from "next/font/google";');
    expect(layout).toContain('variable: "--font-delivery-sans"');
    expect(layout).toContain("className={deliverySans.variable}");
    expect(css).toContain("--delivery-font-ui: var(--font-delivery-sans)");
    expect(css).toContain('"PingFang SC"');
    expect(css).toContain("--font-sans: var(--delivery-font-ui);");
  });

  it("imports the accepted canonical tokens without changing their bytes", () => {
    const css = readProjectFile("app/globals.css");
    expect(css).toContain('@import "../styles/iot-delivery-tokens.css"');
    const tokens = readProjectFile("styles/iot-delivery-tokens.css");
    expect(tokens).toContain("--layout-sidebar-primary-width: 236px;");
    expect(tokens).toContain("--layout-sidebar-secondary-width: 220px;");
    expect(tokens).toContain("--layout-header-height: 48px;");
    expect(tokens).toContain("--font-size-sm: 14px;");
    expect(tokens).toContain("--font-size-xs: 12px;");
    expect(tokens).toContain("--color-primary: #242428;");
    expect(css).not.toContain("linear-gradient");
    expect(
      createHash("sha256")
        .update(readProjectFile("styles/iot-delivery-tokens.css"))
        .digest("hex"),
    ).toBe("70986934379be7940aef7a99a781aeb5249e4295cc974128900227b7602e3dfa");
    expect(
      createHash("sha256")
        .update(readProjectFile("styles/iot-delivery-tokens.ts"))
        .digest("hex"),
    ).toBe("3956c36ca0077d4ca39150284c2d78f598efcfbc0111e1b7ce32aa3f55dab727");
    expect(
      createHash("sha256")
        .update(readProjectFile("styles/iot-delivery-tokens.json"))
        .digest("hex"),
    ).toBe("db6f7e6f158ce507c09e5bddaf99f6d67b158770e49956b148493a39d19c3551");
  });
});
