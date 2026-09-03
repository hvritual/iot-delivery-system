import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));

function readProjectFile(path: string) {
  return readFileSync(new URL(path, `file://${projectRoot.replace(/\\/g, "/")}/`), "utf8");
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

  it("defines an independently owned type, spacing, and surface token scale", () => {
    const css = readProjectFile("app/globals.css");

    expect(css).toContain("--delivery-type-label: 0.6875rem;");
    expect(css).toContain("--delivery-type-ui: 0.75rem;");
    expect(css).toContain("--delivery-type-body: 0.8125rem;");
    expect(css).toContain("--delivery-type-title: 0.9375rem;");
    expect(css).toContain("--delivery-space-1: 0.25rem;");
    expect(css).toContain("--delivery-space-4: 1rem;");
    expect(css).toContain("--delivery-surface: oklch(");
    expect(css).toContain("--delivery-canvas: oklch(");
    expect(css).not.toContain('packages/ui/styles/tokens.css');
  });
});
