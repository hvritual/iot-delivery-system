import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { runYU29Scenario } from "../e2e/yu29-scenario.mjs";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(webRoot, "..");
const backendRoot = path.join(repoRoot, "backend-yunka");
const workspace = await mkdtemp(path.join(tmpdir(), "iotd-yu29-e2e-"));
const databasePath = path.join(workspace, "yu29.db");
const vaultPath = path.join(workspace, "vault");
const manifestPath = path.join(workspace, "fixture.json");
const runtimeBase = "http://127.0.0.1:8281";
const webBase = "http://localhost:5173";
const children = [];

try {
  await run("go", ["run", "./cmd/yu29-fixture", "-db", databasePath, "-vault", vaultPath, "-manifest", manifestPath], backendRoot);
  const fixture = JSON.parse(await readFile(manifestPath, "utf8"));

  children.push(start("go", ["run", "./cmd/yunka-bootstrap"], backendRoot, {
    IOT_DELIVERY_RUNTIME_ENVIRONMENT: "production",
    IOT_DELIVERY_BOOTSTRAP_MODE: "disabled",
    IOT_DELIVERY_YUNKA_HTTP_ADDR: "127.0.0.1:8281",
    IOT_DELIVERY_YUNKA_GRPC_ADDR: "127.0.0.1:8282",
    IOT_DELIVERY_YUNKA_DB: databasePath,
    IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT: vaultPath,
    IOT_DELIVERY_BFF_ORGANIZATION_ID: fixture.organizationId,
    IOT_DELIVERY_BFF_ASSERTION_KEY: fixture.bffAssertionKey,
    IOT_DELIVERY_LOCAL_AUTH_JWT_KEY: fixture.localAuthJwtKey,
  }));
  await waitForHTTP(`${runtimeBase}/health`);

  children.push(start(process.platform === "win32" ? "npm.cmd" : "npm", ["run", "dev"], webRoot, {
    IOT_DELIVERY_API_TARGET: runtimeBase,
    IOT_DELIVERY_WEB_ORIGIN: webBase,
  }));
  await waitForHTTP(webBase);

  await runYU29Scenario({ fixture, webBase });
  console.log("YU-29 E2E PASS");
} finally {
  for (const child of children.reverse()) await stop(child);
  if (process.env.IOT_DELIVERY_E2E_KEEP_WORKSPACE === "1") console.log(`YU-29 workspace: ${workspace}`);
  else await rm(workspace, { recursive: true, force: true });
}

function start(command, args, cwd, extraEnvironment = {}) {
  const child = spawn(command, args, { cwd, env: { ...process.env, ...extraEnvironment }, stdio: ["ignore", "pipe", "pipe"] });
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => process.stdout.write(`[${path.basename(cwd)}] ${chunk}`));
  child.stderr.on("data", (chunk) => process.stderr.write(`[${path.basename(cwd)}] ${chunk}`));
  return child;
}

async function run(command, args, cwd) {
  const child = start(command, args, cwd);
  const code = await exitCode(child);
  if (code !== 0) throw new Error(`${command} ${args.join(" ")} failed with code ${code}`);
}

async function stop(child) {
  if (!child || child.exitCode !== null) return;
  child.kill("SIGTERM");
  const exited = await Promise.race([exitCode(child).then(() => true), delay(5_000).then(() => false)]);
  if (!exited && child.exitCode === null) child.kill("SIGKILL");
}

function exitCode(child) {
  return new Promise((resolve, reject) => {
    if (child.exitCode !== null) return resolve(child.exitCode);
    child.once("error", reject);
    child.once("exit", (code) => resolve(code ?? 1));
  });
}

async function waitForHTTP(url, timeoutMs = 45_000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { cache: "no-store" });
      if (response.ok || response.status === 401 || response.status === 403) return;
      lastError = new Error(`HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await delay(150);
  }
  throw new Error(`timed out waiting for ${url}: ${lastError?.message ?? "unknown error"}`);
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
