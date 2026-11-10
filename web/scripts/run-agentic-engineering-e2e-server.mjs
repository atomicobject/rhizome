import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const repoRoot = process.env.RHIZOME_REPO_ROOT;

const port = process.env.RHIZOME_E2E_PORT;

const state = process.argv[2];

if (!repoRoot || !port || !["empty", "mixed"].includes(state)) {
  throw new Error("Set RHIZOME_REPO_ROOT, RHIZOME_E2E_PORT, and an empty or mixed fixture state");
}

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "rhizome-ae-e2e-"));

const vault = path.join(scratch, "vault");

const rzm = path.join(scratch, "rzm");

const env = { ...process.env, RZM_SKIP_REPO_DELEGATE: "1" };

for (const directory of [env.GOCACHE, env.GOMODCACHE, env.GOTMPDIR]) {
  if (directory) fs.mkdirSync(directory, { recursive: true });
}

let server;

fs.mkdirSync(vault);

process.on("exit", () => {
  if (fs.existsSync(rzm)) {
    spawnSync(rzm, ["stop", "--vault", vault], {
      cwd: vault,
      env,
      stdio: "ignore",
      timeout: 10000,
    });
  }

  fs.rmSync(scratch, { recursive: true, force: true });
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    if (server) server.kill(signal);
    else process.exit(1);
  });
}

function run(command, args, cwd = repoRoot) {
  const result = spawnSync(command, args, { cwd, env, stdio: "inherit" });

  if (result.error) throw result.error;

  if (result.status !== 0) process.exit(result.status ?? 1);
}

if (!process.env.RHIZOME_E2E_SKIP_WEB_BUILD)
  run("npm", ["run", "build"], path.join(repoRoot, "web"));

const untrustedFixtureBuildTags = "fts5 e2efake";

run("go", ["build", "-mod=vendor", "-tags", untrustedFixtureBuildTags, "-o", rzm, "."]);

run("git", ["init", "-q"], vault);

run(
  rzm,
  [
    "init",
    "--workflow",
    "agentic-engineering",
    "--agents",
    "claude,codex",
    "--search",
    "off",
    "--binary-manager",
    "external",
  ],
  vault,
);

fs.cpSync(path.join(repoRoot, "testdata/agentic-engineering", state), vault, { recursive: true });

run(rzm, ["index", "--vault", vault], vault);

server = spawn(rzm, ["serve", "--host", "127.0.0.1", "--port", port, "--vault", vault], {
  cwd: vault,
  env,
  stdio: "inherit",
});

await new Promise((resolve, reject) => {
  server.once("error", reject);
  server.once("exit", (code) => {
    process.exitCode = code ?? 1;
    resolve();
  });
});
