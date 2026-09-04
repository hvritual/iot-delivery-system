import { readFile } from "node:fs/promises";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

const webRoot = resolve(import.meta.dirname, "..");

describe("production build command", () => {
  it("uses the offline launcher so build does not depend on telemetry network access", async () => {
    const packageJSON = JSON.parse(await readFile(resolve(webRoot, "package.json"), "utf8")) as {
      scripts: Record<string, string>;
    };

    expect(packageJSON.scripts.build).toBe("node scripts/build.mjs");

    const launcher = await readFile(resolve(webRoot, "scripts", "build.mjs"), "utf8");
    expect(launcher).toContain("NEXT_TELEMETRY_DISABLED");
    expect(launcher).toContain('"build", "--webpack"');
    expect(launcher).toContain('shell: process.platform === "win32"');
  });
});
