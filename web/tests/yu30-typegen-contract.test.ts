import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const root = fileURLToPath(new URL("../../", import.meta.url));
const web = fileURLToPath(new URL("../", import.meta.url));

describe("YU-30 Next generated type ownership", () => {
  it("regenerates mode-specific Next declarations before strict type checking", () => {
    const pkg = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
    const config = JSON.parse(readFileSync(new URL("../tsconfig.json", import.meta.url), "utf8"));
    expect(pkg.scripts.typecheck).toBe("next typegen && tsc --noEmit");
    expect(config.include).toContain("next-env.d.ts");
    expect(config.compilerOptions.strict).toBe(true);
  });

  it("leaves next-env declarations owned by Next, not a committed dev/build snapshot", () => {
    const tracked = execFileSync("git", ["ls-files", "--", "web/next-env.d.ts"], { cwd: root, encoding: "utf8" });
    expect(tracked).toBe("");
    const ignored = execFileSync("git", ["check-ignore", "--", "next-env.d.ts"], { cwd: web, encoding: "utf8" });
    expect(ignored.trim()).toBe("next-env.d.ts");
  });
});
