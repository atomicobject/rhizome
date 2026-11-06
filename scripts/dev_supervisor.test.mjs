import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdirSync, mkdtempSync, rmSync, utimesSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { fingerprintForStats, loadSupervisorEnv, resolveDevConfig, waitForServeOrigin } from "./dev_supervisor.mjs";

function temp(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), "rhizome-dev-supervisor-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  return root;
}

test("config defaults and explicit overrides", () => {
  const defaults = resolveDevConfig({ RZM_DEV_REPO_ROOT: "/repo", RZM_DEV_BINARY: "bin/current/rzm" });
  assert.equal(defaults.binaryPath, path.resolve("/repo/bin/current/rzm"));
  assert.equal(defaults.webDir, path.resolve("/repo/web"));
  assert.equal(defaults.vaultPath, path.resolve("/repo"));
  assert.equal(defaults.backendPort, null);
  assert.equal(defaults.skipVite, false);
  const explicit = resolveDevConfig({ RZM_DEV_REPO_ROOT: "/repo", RZM_DEV_BINARY: "bin/custom/rzm",
    RZM_DEV_WEB_DIR: "frontend", RZM_DEV_VAULT: "fixture", RZM_DEV_HOST: "0.0.0.0",
    RZM_DEV_PORT: "9911", RZM_DEV_SKIP_VITE: "1" });
  assert.equal(explicit.binaryPath, path.resolve("/repo/bin/custom/rzm"));
  assert.equal(explicit.webDir, path.resolve("/repo/frontend"));
  assert.equal(explicit.vaultPath, path.resolve("/repo/fixture"));
  assert.equal(explicit.backendHost, "0.0.0.0");
  assert.equal(explicit.backendPort, 9911);
  assert.equal(explicit.skipVite, true);
});

test("binary fingerprints include mtime, size and inode", () => {
  const stats = { mtimeMs: 10, size: 2048, ino: 7 };
  const first = fingerprintForStats(stats);
  assert.equal(fingerprintForStats({ ...stats }), first);
  for (const change of [{ mtimeMs: 11 }, { size: 4096 }, { ino: 8 }]) {
    assert.notEqual(fingerprintForStats({ ...stats, ...change }), first);
  }
});

test("discovery rejects stale and foreign manifests, then admits the matching origin", async (t) => {
  const root = temp(t);
  mkdirSync(path.join(root, ".rhizome"));
  const manifest = path.join(root, ".rhizome/runtime.json");
  const write = (pid, time) => {
    writeFileSync(manifest, JSON.stringify({ PID: pid, HTTPURL: "http://127.0.0.1:43123" }));
    utimesSync(manifest, time, time);
  };
  for (const [pid, time] of [[222, 1], [111, 3]]) {
    write(pid, time);
    await assert.rejects(waitForServeOrigin(root, { timeoutMs: 1, minMtimeMs: 2000, expectedPID: 222 }), /timed out/);
  }
  write(222, 3);
  assert.equal(await waitForServeOrigin(root, { minMtimeMs: 2000, expectedPID: 222 }), "http://127.0.0.1:43123");
  assert.equal(await waitForServeOrigin(root, 200), "http://127.0.0.1:43123");
});

test("child environment preserves quoted values", (t) => {
  const root = temp(t);
  writeFileSync(path.join(root, ".env"), 'EXAMPLE_QUOTED="quoted-value"\nEXAMPLE_PLAIN=plain-value\n');
  assert.deepEqual(loadSupervisorEnv(root), { EXAMPLE_QUOTED: "quoted-value", EXAMPLE_PLAIN: "plain-value" });
});

function fakeSupervisor(t, failStart = 0) {
  const root = temp(t);
  mkdirSync(path.join(root, ".rhizome"));
  mkdirSync(path.join(root, "bin"));
  const executable = (name, code) => {
    const target = path.join(root, "bin", name);
    writeFileSync(target, `#!${process.execPath}\n${code}`, { mode: 0o755 });
    return target;
  };
  const backend = executable("backend", `
const fs = require("node:fs");
const count = fs.existsSync("count") ? Number(fs.readFileSync("count")) + 1 : 1;
fs.writeFileSync("count", String(count));
if (count === ${failStart}) process.exit(1);
const origin = "http://127.0.0.1:" + (44000 + count);
fs.writeFileSync(".rhizome/runtime.json", JSON.stringify({ PID: process.pid, HTTPURL: origin }));
console.log("FIXTURE " + JSON.stringify({ kind: "backend", count, pid: process.pid, origin }));
setInterval(() => {}, 1000);
`);
  executable("npm", `
console.log("FIXTURE " + JSON.stringify({ kind: "vite", pid: process.pid, origin: process.env.VITE_RHIZOME_API_ORIGIN }));
setInterval(() => {}, 1000);
`);
  const child = spawn(process.execPath, [fileURLToPath(new URL("./dev_supervisor.mjs", import.meta.url))], {
    cwd: root,
    env: { ...process.env, RZM_DEV_REPO_ROOT: root, RZM_DEV_BINARY: backend, RZM_DEV_WEB_DIR: root,
      RZM_DEV_SKIP_VITE: "0", RZM_DEV_PORT: "", PATH: `${path.join(root, "bin")}${path.delimiter}${process.env.PATH}` },
    stdio: ["pipe", "pipe", "pipe"],
  });
  const events = [];
  const waiters = new Set();
  let output = "";
  let pending = "";
  child.stdout.on("data", (data) => {
    output += data;
    pending += data;
    const lines = pending.split("\n");
    pending = lines.pop();
    for (const line of lines) {
      if (line.startsWith("FIXTURE ")) events.push(JSON.parse(line.slice(8)));
      else events.push({ kind: "log", line });
    }
    for (const check of waiters) check();
  });
  child.stderr.on("data", (data) => { output += data; });
  const exited = once(child, "exit");
  t.after(async () => {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGTERM");
    const timer = setTimeout(() => child.kill("SIGKILL"), 5000);
    try { await exited; } finally { clearTimeout(timer); }
    for (const event of events) {
      if (!event.pid) continue;
      try { process.kill(event.pid, "SIGKILL"); } catch {}
    }
  });
  const waitFor = (predicate) => new Promise((resolve, reject) => {
    const check = () => {
      const found = events.find(predicate);
      if (!found) return;
      clearTimeout(timer);
      waiters.delete(check);
      resolve(found);
    };
    const timer = setTimeout(() => {
      waiters.delete(check);
      reject(new Error(`fixture event timed out:\n${output}`));
    }, 7000);
    waiters.add(check);
    check();
  });
  return { child, exited, events, waitFor };
}

const processFixture = { skip: process.platform === "win32" && "fake executables use shebangs" };

test("a crashed backend reconciles Vite with its new origin", processFixture, async (t) => {
  const fixture = fakeSupervisor(t);
  const first = await fixture.waitFor((e) => e.kind === "backend" && e.count === 1);
  await fixture.waitFor((e) => e.kind === "vite" && e.origin === first.origin);
  process.kill(first.pid, "SIGTERM");
  const second = await fixture.waitFor((e) => e.kind === "backend" && e.count === 2);
  assert.notEqual(second.origin, first.origin);
  await fixture.waitFor((e) => e.kind === "vite" && e.origin === second.origin);
});

test("a failed crash replacement does not terminate the supervisor", processFixture, async (t) => {
  const fixture = fakeSupervisor(t, 2);
  const first = await fixture.waitFor((e) => e.kind === "backend" && e.count === 1);
  await fixture.waitFor((e) => e.kind === "vite" && e.origin === first.origin);
  process.kill(first.pid, "SIGTERM");
  const replacement = await fixture.waitFor((e) => e.kind === "backend" && e.count === 3);
  await fixture.waitFor((e) => e.kind === "vite" && e.origin === replacement.origin);
  assert.equal(fixture.child.exitCode, null);
});

test("shutdown cancels a pending crash retry", processFixture, async (t) => {
  const fixture = fakeSupervisor(t);
  const first = await fixture.waitFor((e) => e.kind === "backend" && e.count === 1);
  await fixture.waitFor((e) => e.kind === "vite");
  process.kill(first.pid, "SIGTERM");
  await fixture.waitFor((e) => e.kind === "log" && e.line.includes("retrying"));
  fixture.child.kill("SIGTERM");
  await fixture.exited;
  assert.equal(fixture.events.filter((e) => e.kind === "backend").length, 1);
});

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

async function restartingSupervisor(t) {
  const { DevSupervisor } = await import("./dev_supervisor.mjs");
  const root = temp(t);
  mkdirSync(path.join(root, ".rhizome"));
  const binaryPath = path.join(root, "binary");
  writeFileSync(binaryPath, "fixture");
  const supervisor = new DevSupervisor({ binaryPath, vaultPath: root, skipVite: true }, {});
  supervisor.log = () => {};
  const ready = (pid) => {
    writeFileSync(path.join(root, ".rhizome/runtime.json"),
      JSON.stringify({ PID: pid, HTTPURL: `http://127.0.0.1:${45000 + pid}` }));
    return { pid };
  };
  return { supervisor, ready };
}

test("rapid restart requests serialize and coalesce", async (t) => {
  const { supervisor, ready } = await restartingSupervisor(t);
  const firstStarted = deferred();
  const releaseFirst = deferred();
  const secondStarted = deferred();
  const operations = [];
  let starts = 0;
  supervisor.stopBackend = async () => { operations.push("stop"); };
  supervisor.startBackend = async () => {
    const n = ++starts;
    operations.push(`start ${n}`);
    if (n === 1) {
      firstStarted.resolve();
      await releaseFirst.promise;
    } else secondStarted.resolve();
    return ready(n);
  };
  const first = supervisor.restartBackend("first rebuild");
  await firstStarted.promise;
  const second = supervisor.restartBackend("second rebuild");
  const third = supervisor.restartBackend("third rebuild");
  releaseFirst.resolve();
  await Promise.all([first, second, third]);
  await secondStarted.promise;
  await supervisor.restartPromise;
  assert.deepEqual(operations, ["stop", "start 1", "stop", "start 2"]);
  assert.equal(supervisor.viteOrigin, "http://127.0.0.1:45002");
});

test("shutdown during a stop cannot start another backend", async (t) => {
  const { supervisor, ready } = await restartingSupervisor(t);
  const stopping = deferred();
  const releaseStop = deferred();
  let starts = 0;
  supervisor.stopBackend = async () => { stopping.resolve(); await releaseStop.promise; };
  supervisor.startBackend = async () => { starts++; return ready(starts); };
  const pending = supervisor.restartBackend("rebuild");
  await stopping.promise;
  supervisor.shuttingDown = true;
  releaseStop.resolve();
  await pending;
  assert.equal(starts, 0);
});

test("shutdown while replacing Vite cannot start a new Vite", async (t) => {
  const { supervisor, ready } = await restartingSupervisor(t);
  const stopping = deferred();
  const releaseStop = deferred();
  let starts = 0;
  supervisor.config.skipVite = false;
  supervisor.vite = {};
  supervisor.viteOrigin = "http://127.0.0.1:45000";
  supervisor.stopBackend = async () => {};
  supervisor.startBackend = async () => ready(1);
  supervisor.stopVite = async () => {
    supervisor.vite = null;
    stopping.resolve();
    await releaseStop.promise;
  };
  supervisor.startVite = () => { starts++; };
  const pending = supervisor.restartBackend("rebuild");
  await stopping.promise;
  supervisor.shuttingDown = true;
  releaseStop.resolve();
  await pending;
  assert.equal(starts, 0);
});
