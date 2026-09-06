import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, chmod } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { runUIReplicaScenario } from "../e2e/ui-replica-scenario.mjs";

const webRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const backendRoot = path.resolve(webRoot, "../backend-yunka");
const workspace = await mkdtemp(path.join(tmpdir(), "iotd-ui-replica-"));
await chmod(workspace, 0o700);
const databasePath = path.join(workspace, "delivery.db");
const vaultPath = path.join(workspace, "vault");
const manifestPath = path.join(workspace, "fixture.json");
const runtimeBinary = path.join(
  workspace,
  process.platform === "win32" ? "runtime.exe" : "runtime",
);
const webBase = "http://localhost:5173";
const runtimeBase = "http://127.0.0.1:8281";
const children = [];
try {
  await run(
    "go",
    [
      "run",
      "./cmd/yu29-fixture",
      "-db",
      databasePath,
      "-vault",
      vaultPath,
      "-manifest",
      manifestPath,
    ],
    backendRoot,
  );
  const fixture = JSON.parse(await readFile(manifestPath, "utf8"));
  await run(
    "go",
    ["build", "-mod=readonly", "-o", runtimeBinary, "./cmd/yunka-bootstrap"],
    backendRoot,
  );
  const backend = start(runtimeBinary, [], backendRoot, {
    IOT_DELIVERY_RUNTIME_ENVIRONMENT: "production",
    IOT_DELIVERY_BOOTSTRAP_MODE: "disabled",
    IOT_DELIVERY_YUNKA_HTTP_ADDR: "127.0.0.1:8281",
    IOT_DELIVERY_YUNKA_GRPC_ADDR: "127.0.0.1:8282",
    IOT_DELIVERY_YUNKA_DB: databasePath,
    IOT_DELIVERY_YUNKA_OBSIDIAN_VAULT: vaultPath,
    IOT_DELIVERY_BFF_ORGANIZATION_ID: fixture.organizationId,
    IOT_DELIVERY_BFF_ASSERTION_KEY: fixture.bffAssertionKey,
    IOT_DELIVERY_LOCAL_AUTH_JWT_KEY: fixture.localAuthJwtKey,
  });
  children.push(backend);
  await ready(`${runtimeBase}/health`);
  // Tests exercise the actual production build, not the HTML design/demo runtime.
  children.push(
    start(
      process.execPath,
      [
        path.join(webRoot, "node_modules/next/dist/bin/next"),
        "start",
        "--hostname",
        "127.0.0.1",
        "--port",
        "5173",
      ],
      webRoot,
      {
        IOT_DELIVERY_API_TARGET: runtimeBase,
        IOT_DELIVERY_WEB_ORIGIN: webBase,
        NEXT_TELEMETRY_DISABLED: "1",
      },
    ),
  );
  await ready(webBase);
  await runUIReplicaScenario({
    fixture,
    webBase,
    stopBackend: () => stop(backend),
  });
  console.log("UI replica real-runtime E2E PASS");
} finally {
  for (const child of children.reverse()) await stop(child);
  await rm(workspace, { recursive: true, force: true });
}
function start(cmd, args, cwd, extra = {}) {
  const child = spawn(cmd, args, {
    cwd,
    env: { ...process.env, ...extra },
    stdio: ["ignore", "pipe", "pipe"],
  });
  // Runtime access logs are intentionally not artifacted. They are not UI evidence.
  child.stderr.on("data", (chunk) =>
    process.stderr.write(`[${path.basename(cwd)}] ${chunk}`),
  );
  return child;
}
async function run(cmd, args, cwd) {
  const code = await exit(start(cmd, args, cwd));
  if (code !== 0) throw new Error(`${cmd} command failed (${code})`);
}
function exit(child) {
  return new Promise((resolve, reject) => {
    if (child.exitCode !== null) return resolve(child.exitCode);
    child.once("error", reject);
    child.once("exit", (code) => resolve(code ?? 1));
  });
}
async function stop(child) {
  if (!child || child.exitCode !== null) return;
  child.kill("SIGTERM");
  const done = await Promise.race([
    exit(child).then(() => true),
    delay(5000).then(() => false),
  ]);
  if (!done && child.exitCode === null) {
    child.kill("SIGKILL");
    await exit(child);
  }
}
async function ready(url) {
  const end = Date.now() + 45000;
  while (Date.now() < end) {
    try {
      const r = await fetch(url);
      if (r.ok || r.status === 401 || r.status === 403) return;
    } catch {}
    await delay(150);
  }
  throw new Error(`Service did not become ready: ${url}`);
}
function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
