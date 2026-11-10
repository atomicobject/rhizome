import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const dirname = path.dirname(fileURLToPath(import.meta.url));

const repoRoot = path.resolve(dirname, "..");

const port = Number(
  process.env.RHIZOME_E2E_PORT ||
    24000 + (createHash("sha256").update(repoRoot).digest().readUInt16BE(0) % 5000),
);

const states = ["mixed", "empty"] as const;

export default defineConfig({
  testDir: path.resolve(dirname, "tests/e2e-agentic-engineering"),
  outputDir: "test-results/agentic-engineering",
  retries: 0,
  workers: 1,
  use: {
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    launchOptions: {
      args: [
        "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE localhost, EXCLUDE *.localhost",
      ],
    },
  },
  projects: states.map((state, index) => ({
    name: state,
    testMatch: state === "empty" ? /empty\.spec\.ts/ : /(?:delivery|constraints)\.spec\.ts/,
    use: { baseURL: `http://127.0.0.1:${port + index}` },
  })),
  webServer: states.map((state, index) => ({
    command: `node ./scripts/run-agentic-engineering-e2e-server.mjs ${state}`,
    cwd: dirname,
    port: port + index,
    reuseExistingServer: false,
    gracefulShutdown: { signal: "SIGTERM" as const, timeout: 15000 },
    timeout: 240000,
    env: {
      RHIZOME_REPO_ROOT: repoRoot,
      RHIZOME_E2E_PORT: String(port + index),
      RHIZOME_E2E_SKIP_WEB_BUILD: index > 0 ? "1" : (process.env.RHIZOME_E2E_SKIP_WEB_BUILD ?? ""),
      GOCACHE: path.join(repoRoot, ".gocache"),
      GOMODCACHE: path.join(repoRoot, ".gomodcache"),
      GOTMPDIR: path.join(repoRoot, ".gotmp"),
    },
  })),
});
