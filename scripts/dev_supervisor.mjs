import { spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";

const RESTART_POLL_MS = 1000;
const STOP_GRACE_MS = 1500;
const DISCOVERY_WAIT_MS = 5000;
const DISCOVERY_FILE = path.join(".rhizome", "runtime.json");

export function resolveDevConfig(env = process.env) {
  const repoRoot = path.resolve(env.RZM_DEV_REPO_ROOT ?? process.cwd());
  const binaryPath = path.resolve(
    repoRoot,
    env.RZM_DEV_BINARY ?? path.join("bin", defaultGoos(), "rzm")
  );
  const webDir = path.resolve(repoRoot, env.RZM_DEV_WEB_DIR ?? "web");
  const vaultPath = path.resolve(repoRoot, env.RZM_DEV_VAULT ?? ".");
  const backendHost = env.RZM_DEV_HOST ?? "127.0.0.1";
  const backendPort = env.RZM_DEV_PORT ? parsePort(env.RZM_DEV_PORT) : null;

  return {
    repoRoot,
    binaryPath,
    webDir,
    vaultPath,
    backendHost,
    backendPort,
    skipVite: env.RZM_DEV_SKIP_VITE === "1",
  };
}

export function loadSupervisorEnv(repoRoot) {
  const loaded = {};
  for (const file of [path.join(repoRoot, ".env"), path.join(repoRoot, ".rhizome", ".env")]) {
    if (!fs.existsSync(file)) continue;
    Object.assign(loaded, parseDotEnv(fs.readFileSync(file, "utf8")));
  }
  return loaded;
}

export function fingerprintForStats(stats) {
  return `${Math.trunc(stats.mtimeMs)}:${stats.size}:${stats.ino ?? 0}`;
}

export function readBinaryFingerprint(binaryPath) {
  return fingerprintForStats(fs.statSync(binaryPath));
}

export async function waitForServeOrigin(vaultPath, options = DISCOVERY_WAIT_MS) {
  const timeoutMs =
    typeof options === "number"
      ? options
      : (options.timeoutMs ?? DISCOVERY_WAIT_MS);
  const minMtimeMs =
    typeof options === "number" ? 0 : (options.minMtimeMs ?? 0);
  const expectedPID =
    typeof options === "number" ? null : (options.expectedPID ?? null);
  const target = path.join(vaultPath, DISCOVERY_FILE);
  const startedAt = Date.now();
  for (;;) {
    try {
      const stats = fs.statSync(target);
      if (minMtimeMs > 0 && stats.mtimeMs < minMtimeMs) {
        throw new Error("stale discovery file");
      }
      const data = JSON.parse(fs.readFileSync(target, "utf8"));
      if (
        expectedPID !== null &&
        data.PID !== expectedPID &&
        data.pid !== expectedPID
      ) {
        throw new Error("discovery file belongs to another process");
      }
      const origin = normalizeOrigin(data.HTTPURL ?? data.httpURL);
      if (origin) return origin;
    } catch {}
    if (Date.now() >= startedAt + timeoutMs) {
      throw new Error(`timed out waiting for ${target}`);
    }
    await sleep(100);
  }
}

async function main() {
  const config = resolveDevConfig();
  if (!fs.existsSync(config.binaryPath)) {
    throw new Error(`missing backend binary: ${config.binaryPath}`);
  }

  const supervisor = new DevSupervisor(config, {
    ...process.env,
    ...loadSupervisorEnv(config.repoRoot),
  });
  await supervisor.run();
}

class DevSupervisor {
  constructor(config, childEnv) {
    this.config = config;
    this.childEnv = childEnv;
    this.backend = null;
    this.vite = null;
    this.viteOrigin = null;
    this.shuttingDown = false;
    this.restartPromise = null;
    this.restartQueued = false;
    this.binaryFingerprint = readBinaryFingerprint(config.binaryPath);
    this.watchTimer = null;
  }

  async run() {
    this.installSignalHandlers();
    const startedAt = Date.now();
    const backend = await this.startBackend("initial start");
    this.viteOrigin = await waitForServeOrigin(this.config.vaultPath, {
      minMtimeMs: startedAt,
      expectedPID: backend.pid ?? null,
    });
    if (!this.config.skipVite) {
      this.startVite();
    }
    this.watchTimer = setInterval(() => {
      void this.checkBinary();
    }, RESTART_POLL_MS);
    process.stdin.resume();
  }

  installSignalHandlers() {
    const shutdown = async (signal) => {
      if (this.shuttingDown) return;
      this.shuttingDown = true;
      this.log(`stopping (${signal})`);
      if (this.watchTimer) clearInterval(this.watchTimer);
      await Promise.allSettled([this.stopVite(), this.stopBackend()]);
      process.exit(0);
    };

    process.on("SIGINT", () => void shutdown("SIGINT"));
    process.on("SIGTERM", () => void shutdown("SIGTERM"));
  }

  async checkBinary() {
    if (this.shuttingDown) return;
    let nextFingerprint;
    try {
      nextFingerprint = readBinaryFingerprint(this.config.binaryPath);
    } catch (error) {
      this.log(`waiting for backend binary: ${error.message}`);
      return;
    }
    if (nextFingerprint === this.binaryFingerprint) return;
    this.binaryFingerprint = nextFingerprint;
    await this.restartBackend("binary rebuilt");
  }

  async restartBackend(reason) {
    if (this.shuttingDown) return;
    if (this.restartPromise) {
      this.restartQueued = true;
      return this.restartPromise;
    }
    this.restartPromise = (async () => {
      this.log(`restarting backend (${reason})`);
      await this.stopBackend();
      const startedAt = Date.now();
      const backend = await this.startBackend(reason);
      const nextOrigin = await waitForServeOrigin(this.config.vaultPath, {
        minMtimeMs: startedAt,
        expectedPID: backend.pid ?? null,
      });
      if (this.viteOrigin !== nextOrigin) {
        this.log(`backend origin changed to ${nextOrigin}`);
        this.viteOrigin = nextOrigin;
        if (this.vite) {
          await this.restartVite();
        }
      }
    })().finally(() => {
      this.restartPromise = null;
      if (this.restartQueued) {
        this.restartQueued = false;
        void this.restartBackend("queued change");
      }
    });
    return this.restartPromise;
  }

  async startBackend(reason) {
    this.log(`starting backend (${reason})`);
    const args = [
      "serve",
      "--vault",
      this.config.vaultPath,
      "--host",
      this.config.backendHost,
    ];
    if (this.config.backendPort === null) {
      args.push("--port", "0", "--reuse-discovery-port");
    } else {
      args.push("--port", String(this.config.backendPort));
    }
    const child = spawn(
      this.config.binaryPath,
      args,
      {
        cwd: this.config.repoRoot,
        env: this.childEnv,
        stdio: "inherit",
      }
    );
    child.on("exit", (code, signal) => {
      const expected = this.backend !== child || this.shuttingDown;
      if (this.backend === child) this.backend = null;
      if (expected) return;
      this.log(`backend exited (${signal ?? code ?? "unknown"}); retrying`);
      setTimeout(() => {
        if (!this.shuttingDown && !this.backend) {
          void this.startBackend("backend exit");
        }
      }, RESTART_POLL_MS);
    });
    this.backend = child;
    return child;
  }

  async stopBackend() {
    if (!this.backend) return;
    const child = this.backend;
    this.backend = null;
    await stopChild(child, "rzm serve");
  }

  startVite() {
    this.log(`starting Vite dev server with API ${this.viteOrigin}`);
    const child = spawn("npm", ["run", "dev"], {
      cwd: this.config.webDir,
      env: {
        ...this.childEnv,
        VITE_RHIZOME_API_ORIGIN: this.viteOrigin,
      },
      stdio: "inherit",
    });
    child.on("exit", (code, signal) => {
      const expected = this.vite !== child || this.shuttingDown;
      if (this.vite === child) this.vite = null;
      if (expected) return;
      this.log(`vite exited (${signal ?? code ?? "unknown"}); shutting down`);
      this.shuttingDown = true;
      if (this.watchTimer) clearInterval(this.watchTimer);
      void this.stopBackend().finally(() => {
        process.exit(code ?? 1);
      });
    });
    this.vite = child;
  }

  async stopVite() {
    if (!this.vite) return;
    const child = this.vite;
    this.vite = null;
    await stopChild(child, "vite");
  }

  async restartVite() {
    await this.stopVite();
    if (!this.shuttingDown) {
      this.startVite();
    }
  }

  log(message) {
    console.log(`[dev] ${message}`);
  }
}

async function stopChild(child, label) {
  child.kill("SIGTERM");
  const cleanExit = await waitForExit(child, STOP_GRACE_MS);
  if (cleanExit) return;
  console.log(`[dev] forcing ${label} to stop`);
  child.kill("SIGKILL");
  await waitForExit(child, STOP_GRACE_MS);
}

function waitForExit(child, timeoutMs) {
  return new Promise((resolve) => {
    let settled = false;
    const timer = setTimeout(() => finish(false), timeoutMs);
    const finish = (value) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(value);
    };
    child.once("exit", () => finish(true));
    child.once("error", () => finish(true));
  });
}

function parsePort(value) {
  const port = Number.parseInt(value, 10);
  if (!Number.isInteger(port) || port <= 0 || port > 65535) {
    throw new Error(`invalid RZM_DEV_PORT: ${value}`);
  }
  return port;
}

function normalizeOrigin(value) {
  if (!value) return null;
  try {
    return new URL(value).origin;
  } catch {
    return null;
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseDotEnv(text) {
  const out = {};
  for (const line of text.split(/\r?\n/)) {
    let raw = line.trim();
    if (!raw || raw.startsWith("#")) continue;
    if (raw.startsWith("export ")) {
      raw = raw.slice("export ".length).trim();
    }
    const eq = raw.indexOf("=");
    if (eq === -1) continue;
    const key = raw.slice(0, eq).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) continue;
    let value = stripInlineComment(raw.slice(eq + 1).trim()).trim();
    value = unquoteValue(value);
    out[key] = value;
  }
  return out;
}

function stripInlineComment(value) {
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < value.length; i += 1) {
    const char = value[i];
    if (char === "'" && !inDouble) inSingle = !inSingle;
    if (char === '"' && !inSingle) inDouble = !inDouble;
    if (char === "#" && !inSingle && !inDouble) {
      if (i === 0 || value[i - 1] === " " || value[i - 1] === "\t") {
        return value.slice(0, i);
      }
    }
  }
  return value;
}

function unquoteValue(value) {
  if (value.startsWith("'") && value.endsWith("'") && value.length >= 2) {
    return value.slice(1, -1);
  }
  if (value.startsWith('"') && value.endsWith('"') && value.length >= 2) {
    try {
      return JSON.parse(value);
    } catch {
      return value.slice(1, -1);
    }
  }
  return value;
}

function defaultGoos() {
  switch (process.platform) {
    case "darwin":
      return "darwin";
    case "win32":
      return "windows";
    default:
      return "linux";
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(`[dev] ${error.message}`);
    process.exit(1);
  });
}
