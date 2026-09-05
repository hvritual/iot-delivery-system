import { spawn, spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

const DEFAULT_TIMEOUT_MS = 15_000;

export async function launchBrowser(environment = process.env) {
  if (typeof WebSocket !== "function") {
    throw new Error("YU-29 requires a Node runtime with the global WebSocket API");
  }
  const executable = resolveBrowserExecutable(environment);
  const userDataDir = await mkdtemp(path.join(tmpdir(), "iotd-yu29-browser-"));
  const args = [
    "--headless=new",
    "--remote-debugging-port=0",
    `--user-data-dir=${userDataDir}`,
    "--no-first-run",
    "--no-default-browser-check",
    "--disable-background-networking",
    "--disable-component-update",
    "--disable-sync",
    "--metrics-recording-only",
    "about:blank",
  ];
  if (environment.IOT_DELIVERY_E2E_NO_SANDBOX === "1") args.unshift("--no-sandbox");

  const child = spawn(executable, args, { stdio: ["ignore", "ignore", "pipe"] });
  const endpoint = await devtoolsEndpoint(child);
  const client = await CDPClient.connect(endpoint);

  return {
    executable,
    client,
    process: child,
    userDataDir,
    async createContext() {
      const { browserContextId } = await client.send("Target.createBrowserContext");
      const { targetId } = await client.send("Target.createTarget", { url: "about:blank", browserContextId });
      const { sessionId } = await client.send("Target.attachToTarget", { targetId, flatten: true });
      await client.send("Page.enable", {}, sessionId);
      await client.send("Runtime.enable", {}, sessionId);
      await client.send("Network.enable", {}, sessionId);
      return new BrowserContext(client, browserContextId, targetId, sessionId);
    },
    async close() {
      try {
        await client.send("Browser.close");
      } catch {
        child.kill("SIGTERM");
      }
      client.close();
      if (!child.killed) child.kill("SIGTERM");
      await rm(userDataDir, { recursive: true, force: true });
    },
  };
}

export class BrowserContext {
  constructor(client, browserContextId, targetId, sessionId) {
    this.client = client;
    this.browserContextId = browserContextId;
    this.targetId = targetId;
    this.sessionId = sessionId;
  }

  async navigate(url) {
    const loaded = this.client.waitForEvent("Page.loadEventFired", this.sessionId);
    await this.client.send("Page.navigate", { url }, this.sessionId);
    await loaded;
  }

  async evaluate(expression) {
    const result = await this.client.send("Runtime.evaluate", {
      expression,
      awaitPromise: true,
      returnByValue: true,
      userGesture: true,
    }, this.sessionId);
    if (result.exceptionDetails) {
      const description = result.exceptionDetails.exception?.description
        ?? result.exceptionDetails.text
        ?? "browser evaluation failed";
      throw new Error(description);
    }
    return result.result?.value;
  }

  async waitFor(expression, timeoutMs = DEFAULT_TIMEOUT_MS) {
    const deadline = Date.now() + timeoutMs;
    let last;
    while (Date.now() < deadline) {
      last = await this.evaluate(expression);
      if (last) return last;
      await delay(75);
    }
    throw new Error(`browser condition timed out: ${expression}; last=${JSON.stringify(last)}`);
  }

  async cookies() {
    const { cookies } = await this.client.send("Storage.getCookies", { browserContextId: this.browserContextId });
    return cookies ?? [];
  }

  async close() {
    try {
      await this.client.send("Target.disposeBrowserContext", { browserContextId: this.browserContextId });
    } catch {
      // Browser shutdown can race context disposal during test cleanup.
    }
  }
}

class CDPClient {
  constructor(socket) {
    this.socket = socket;
    this.nextID = 1;
    this.pending = new Map();
    this.waiters = new Map();
    socket.addEventListener("message", (event) => this.handleMessage(event.data));
  }

  static async connect(url) {
    const socket = new WebSocket(url);
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timed out connecting to browser DevTools")), DEFAULT_TIMEOUT_MS);
      socket.addEventListener("open", () => {
        clearTimeout(timer);
        resolve();
      }, { once: true });
      socket.addEventListener("error", () => {
        clearTimeout(timer);
        reject(new Error("failed to connect to browser DevTools"));
      }, { once: true });
    });
    return new CDPClient(socket);
  }

  send(method, params = {}, sessionId = undefined) {
    const id = this.nextID++;
    const payload = { id, method, params, ...(sessionId ? { sessionId } : {}) };
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`CDP command timed out: ${method}`));
      }, DEFAULT_TIMEOUT_MS);
      this.pending.set(id, { resolve, reject, timer, method });
      this.socket.send(JSON.stringify(payload));
    });
  }

  waitForEvent(method, sessionId = undefined, timeoutMs = DEFAULT_TIMEOUT_MS) {
    const key = eventKey(method, sessionId);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        const queue = this.waiters.get(key) ?? [];
        this.waiters.set(key, queue.filter((entry) => entry.resolve !== resolve));
        reject(new Error(`CDP event timed out: ${method}`));
      }, timeoutMs);
      const queue = this.waiters.get(key) ?? [];
      queue.push({ resolve, timer });
      this.waiters.set(key, queue);
    });
  }

  handleMessage(raw) {
    const message = JSON.parse(String(raw));
    if (message.id) {
      const pending = this.pending.get(message.id);
      if (!pending) return;
      clearTimeout(pending.timer);
      this.pending.delete(message.id);
      if (message.error) pending.reject(new Error(`${pending.method}: ${message.error.message}`));
      else pending.resolve(message.result ?? {});
      return;
    }
    if (!message.method) return;
    const key = eventKey(message.method, message.sessionId);
    const queue = this.waiters.get(key);
    if (!queue?.length) return;
    const waiter = queue.shift();
    clearTimeout(waiter.timer);
    if (queue.length) this.waiters.set(key, queue);
    else this.waiters.delete(key);
    waiter.resolve(message.params ?? {});
  }

  close() {
    try {
      this.socket.close();
    } catch {
      // no-op
    }
  }
}

function eventKey(method, sessionId) {
  return `${sessionId ?? "browser"}:${method}`;
}

function resolveBrowserExecutable(environment) {
  const configured = environment.IOT_DELIVERY_E2E_BROWSER?.trim();
  if (configured) {
    if (!existsSync(configured)) throw new Error(`IOT_DELIVERY_E2E_BROWSER does not exist: ${configured}`);
    return configured;
  }

  const candidates = process.platform === "win32"
    ? [
      `${environment.PROGRAMFILES ?? "C:\\Program Files"}\\Google\\Chrome\\Application\\chrome.exe`,
      `${environment["PROGRAMFILES(X86)"] ?? "C:\\Program Files (x86)"}\\Google\\Chrome\\Application\\chrome.exe`,
      `${environment.LOCALAPPDATA ?? ""}\\Google\\Chrome\\Application\\chrome.exe`,
      `${environment.PROGRAMFILES ?? "C:\\Program Files"}\\Microsoft\\Edge\\Application\\msedge.exe`,
    ]
    : process.platform === "darwin"
      ? [
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
        "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
        "/Applications/Chromium.app/Contents/MacOS/Chromium",
      ]
      : ["chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "microsoft-edge"];

  for (const candidate of candidates) {
    if (candidate.includes(path.sep) && existsSync(candidate)) return candidate;
    if (!candidate.includes(path.sep)) {
      const found = spawnSync("which", [candidate], { encoding: "utf8" });
      if (found.status === 0 && found.stdout.trim()) return found.stdout.trim();
    }
  }
  throw new Error("no Chromium-family browser found; set IOT_DELIVERY_E2E_BROWSER to an installed Chrome/Chromium/Edge executable");
}

function devtoolsEndpoint(child) {
  return new Promise((resolve, reject) => {
    let stderr = "";
    const timer = setTimeout(() => {
      reject(new Error(`timed out waiting for browser DevTools endpoint; stderr=${stderr.slice(-2000)}`));
    }, DEFAULT_TIMEOUT_MS);
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
      const match = stderr.match(/DevTools listening on (ws:\/\/[^\s]+)/);
      if (!match) return;
      clearTimeout(timer);
      resolve(match[1]);
    });
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      reject(new Error(`browser exited before DevTools was ready: code=${code} signal=${signal}; stderr=${stderr.slice(-2000)}`));
    });
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
  });
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
