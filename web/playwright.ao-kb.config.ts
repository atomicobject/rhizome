import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

// The AO KB journey runs against its own disposable vault: the synthetic AO KB
// fixture with the ao-kb starter installed, so it never changes the shared suite's vault.
const dirname = path.dirname(fileURLToPath(import.meta.url));

const repoRoot = path.resolve(dirname, "..");

const checkoutPort = () =>
  20000 + ((createHash("sha256").update(repoRoot).digest().readUInt32BE(0) + 1) % 10000);

const port = Number(process.env.RHIZOME_E2E_PORT || (process.env.CI ? 4174 : checkoutPort()));

export default defineConfig({
  testDir: path.resolve(dirname, "tests/e2e-ao-kb"),
  workers: 1,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: process.env.CI ? "retain-on-failure" : "off",
    launchOptions: {
      args: [
        "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE localhost, EXCLUDE *.localhost",
      ],
    },
  },
  webServer: {
    command: "node ./scripts/run-ao-kb-e2e-server.mjs",
    cwd: dirname,
    port,
    reuseExistingServer: !process.env.CI,
    timeout: 240000,
    env: { RHIZOME_REPO_ROOT: repoRoot, RHIZOME_E2E_PORT: String(port) },
  },
});
