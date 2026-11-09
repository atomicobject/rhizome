import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

// The AO KB specs run against their own disposable vaults with the ao-kb starter
// installed, so they never change the shared suite's vault: the booking fixture
// for the journey specs, and the legacy fixture (AO schema 5) for the Knowledge
// site, on the next port.
const dirname = path.dirname(fileURLToPath(import.meta.url));

const repoRoot = path.resolve(dirname, "..");

const checkoutPort = () =>
  20000 + ((createHash("sha256").update(repoRoot).digest().readUInt32BE(0) + 1) % 10000);

const port = Number(process.env.RHIZOME_E2E_PORT || (process.env.CI ? 4174 : checkoutPort()));

const sitePort = port + 1;

const use = {
  trace: process.env.CI ? ("retain-on-failure" as const) : ("off" as const),
  launchOptions: {
    args: [
      "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE localhost, EXCLUDE *.localhost",
    ],
  },
};

// Playwright starts web servers in order, so the second reuses the first's web build.
function server(listen: number, fixture: string, skipWebBuild = false) {
  const env = {
    RHIZOME_REPO_ROOT: repoRoot,
    RHIZOME_E2E_PORT: String(listen),
    RHIZOME_E2E_SKIP_WEB_BUILD: skipWebBuild ? "1" : (process.env.RHIZOME_E2E_SKIP_WEB_BUILD ?? ""),
  };

  return {
    command: `node ./scripts/run-ao-kb-e2e-server.mjs ${fixture}`,
    cwd: dirname,
    port: listen,
    reuseExistingServer: !process.env.CI,
    timeout: 240000,
    env,
  };
}

export default defineConfig({
  testDir: path.resolve(dirname, "tests/e2e-ao-kb"),
  workers: 1,
  projects: [
    {
      name: "booking",
      testIgnore: /site\.spec\.ts/,
      use: { ...use, baseURL: `http://127.0.0.1:${port}` },
    },
    {
      name: "legacy",
      testMatch: /site\.spec\.ts/,
      use: { ...use, baseURL: `http://127.0.0.1:${sitePort}` },
    },
  ],
  webServer: [
    server(port, "testdata/ao-kb/booking"),
    server(sitePort, "testdata/ao-kb-legacy", true),
  ],
});
