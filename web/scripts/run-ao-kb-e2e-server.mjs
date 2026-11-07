// Serves a disposable copy of the synthetic AO KB fixture with the ao-kb starter
// installed, for the AO KB journey specs (playwright.ao-kb.config.ts). It installs
// the starter through ordinary `rzm init`, exactly as the setup skill does, adds the
// conflict registry include, indexes, and serves.
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { refresh } from "../../pkg/app/cli/init/templates/starters/ao-kb/module/bindings/publish.mjs";

const repoRoot = process.env.RHIZOME_REPO_ROOT;

const port = process.env.RHIZOME_E2E_PORT;

if (!repoRoot || !port) {
  throw new Error(
    "RHIZOME_REPO_ROOT and RHIZOME_E2E_PORT are required; playwright.ao-kb.config.ts sets both",
  );
}

const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "rhizome-ao-kb-e2e-"));

const vault = path.join(scratch, "booking");

const rzm = path.join(scratch, "rzm");

process.on("exit", () => {
  if (fs.existsSync(rzm))
    spawnSync(rzm, ["stop", "--vault", vault], {
      cwd: vault,
      env: { ...process.env, RZM_SKIP_REPO_DELEGATE: "1" },
      stdio: "ignore",
      timeout: 10000,
    });
  fs.rmSync(scratch, { recursive: true, force: true });
});

fs.cpSync(path.join(repoRoot, "testdata", "ao-kb", "booking"), vault, { recursive: true });

const goEnv = {
  GOCACHE: process.env.GOCACHE || path.join(repoRoot, ".gocache"),
  GOMODCACHE: process.env.GOMODCACHE || path.join(repoRoot, ".gomodcache"),
  GOTMPDIR: process.env.GOTMPDIR || path.join(repoRoot, ".gotmp"),
  RZM_SKIP_REPO_DELEGATE: "1",
};

for (const dir of [goEnv.GOCACHE, goEnv.GOMODCACHE, goEnv.GOTMPDIR])
  fs.mkdirSync(dir, { recursive: true });

const run = (cmd, args, cwd = repoRoot) => {
  const result = spawnSync(cmd, args, { cwd, env: { ...process.env, ...goEnv }, stdio: "inherit" });

  if (result.status !== 0) process.exit(result.status ?? 1);
};

if (!process.env.RHIZOME_E2E_SKIP_WEB_BUILD)
  run("npm", ["run", "build"], path.join(repoRoot, "web"));

run("go", ["build", "-mod=vendor", "-tags", "fts5", "-o", rzm, "."]);

run("git", ["init", "-q"], vault);

run(
  rzm,
  [
    "init",
    "--workflow",
    "none",
    "--addons",
    "ao-kb",
    "--agents",
    "claude",
    "--search",
    "off",
    "--binary-manager",
    "external",
  ],
  vault,
);

// Discovery skips hidden directories unless an include names the path (the setup skill's step).
const config = path.join(vault, ".rhizome", "config.yml");

const intermediate = JSON.parse(
  fs.readFileSync(path.join(repoRoot, "testdata/ao-kb/model-intermediate.json"), "utf8"),
);

const includes = [
  "**/*.md",
  ".kb/curation/_conflicts.md",
  ...new Set(intermediate.sources.map((source) => source.path)),
];

fs.appendFileSync(config, `\nnotes:\n  includes: ${JSON.stringify(includes)}\n`);

process.env.RZM = rzm;

// CI uses the checked AO intermediate, with the production renderer and publisher.
await refresh({
  root: fs.realpathSync(vault),
  directory: path.join(vault, ".rhizome/modules/ao-kb/bindings"),
  selection: { formatVersion: 1, accountRoot: ".", product: null, toolkit: null },
  exportModel: async () => structuredClone(intermediate),
});

run(rzm, ["index", "--vault", vault]);

const server = spawn(rzm, ["serve", "--host", "127.0.0.1", "--port", port, "--vault", vault], {
  cwd: repoRoot,
  env: { ...process.env, ...goEnv },
  stdio: "inherit",
});

for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => server.kill(signal));

await new Promise((resolve, reject) => {
  server.once("error", reject);
  server.once("exit", (code) => {
    process.exitCode = code || 0;
    resolve();
  });
});
