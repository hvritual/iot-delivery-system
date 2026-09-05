import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const testsDir = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(testsDir, "..");
const repoRoot = path.resolve(webRoot, "..");
const scenario = readFileSync(path.join(webRoot, "e2e/yu29-scenario.mjs"), "utf8");
const fixture = readFileSync(path.join(repoRoot, "backend-yunka/cmd/yu29-fixture/main.go"), "utf8");
const packageJSON = JSON.parse(readFileSync(path.join(webRoot, "package.json"), "utf8"));

describe("YU-29 E2E harness contract", () => {
  it("uses two independent real browser contexts without browser identity persistence", () => {
    expect(scenario.match(/browser\.createContext\(\)/g)).toHaveLength(2);
    for (const forbidden of ["localStorage", "sessionStorage", "playwright", "puppeteer"]) {
      expect(scenario).not.toContain(forbidden);
    }
  });

  it("creates real users through bootstrap and YU-20 rather than direct identity SQL", () => {
    expect(fixture).toContain("AdministratorBootstrap().Initialize");
    expect(fixture).toContain("MemberAdministration().Create");
    expect(fixture).toContain("INSERT INTO organizations");
    expect(fixture).not.toMatch(/INSERT\s+INTO\s+users/i);
    expect(fixture).not.toMatch(/INSERT\s+INTO\s+role_bindings/i);
    expect(fixture).not.toMatch(/INSERT\s+INTO\s+iotd_local_credentials/i);
  });

  it("adds the E2E command without adding a browser test dependency", () => {
    expect(packageJSON.scripts["e2e:yu29"]).toBe("node scripts/yu29-e2e.mjs");
    expect(packageJSON.devDependencies?.["@playwright/test"]).toBeUndefined();
    expect(packageJSON.devDependencies?.playwright).toBeUndefined();
    expect(packageJSON.devDependencies?.puppeteer).toBeUndefined();
  });
});
