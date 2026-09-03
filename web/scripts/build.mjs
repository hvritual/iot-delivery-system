import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const nextBinary = resolve(
  root,
  "node_modules",
  ".bin",
  process.platform === "win32" ? "next.cmd" : "next",
);

const child = spawn(nextBinary, ["build", "--webpack"], {
  cwd: root,
  env: {
    ...process.env,
    // Local builds should not require DNS or telemetry egress to be valid.
    NEXT_TELEMETRY_DISABLED: process.env.NEXT_TELEMETRY_DISABLED || "1",
  },
  shell: process.platform === "win32",
  stdio: "inherit",
});

child.once("error", (error) => {
  console.error(`Unable to start Next production build: ${error.message}`);
  process.exitCode = 1;
});

child.once("exit", (code, signal) => {
  if (signal) {
    console.error(`Next production build stopped by signal ${signal}.`);
    process.exitCode = 1;
    return;
  }
  process.exitCode = code ?? 1;
});
