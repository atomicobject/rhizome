import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "@playwright/test";

const dirname = path.dirname(fileURLToPath(import.meta.url));

const repoRoot = path.resolve(dirname, "..");

// Local runs reuse a server already listening on the port, so a shared default
// would test whichever worktree started first. Each checkout gets its own port.
// ponytail: 1-in-10,000 collision per pair of checkouts; set RHIZOME_E2E_PORT if two collide.
const checkoutPort = () =>
  20000 + (createHash("sha256").update(repoRoot).digest().readUInt32BE(0) % 10000);

const port = Number(process.env.RHIZOME_E2E_PORT || (process.env.CI ? 4173 : checkoutPort()));

export default defineConfig({
  testDir: path.resolve(dirname, "tests/e2e"),
  // The suite shares one mutable vault. Serial workers keep edit, index, and
  // file-watch journeys from changing another test's disk-backed expectations.
  workers: 1,
  projects: [
    { name: "workspace", testIgnore: /html-notes\.spec\.ts/ },
    {
      name: "html-notes",
      testMatch: /html-notes\.spec\.ts/,
      dependencies: ["workspace"],
    },
  ],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: process.env.CI ? "retain-on-failure" : "off",
    // WHY: index.html loads a render-blocking Google Fonts stylesheet, and
    // page.goto waits for "load". A slow or stalled internet request then
    // times out navigation. Failing every non-local DNS lookup keeps the suite
    // hermetic; fonts fall back and no test depends on them. HTML note frames
    // load from <token>.localhost, so that loopback name stays resolvable.
    launchOptions: {
      args: [
        "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE localhost, EXCLUDE *.localhost",
      ],
    },
  },
  webServer: {
    command: "node ./scripts/run-e2e-server.mjs",
    cwd: dirname,
    port,
    reuseExistingServer: !process.env.CI,
    timeout: 180000,
    env: {
      RHIZOME_REPO_ROOT: repoRoot,
      RHIZOME_E2E_PORT: String(port),
      GOFLAGS: [process.env.GOFLAGS, "-tags=e2efake"].filter(Boolean).join(" "),
    },
  },
});
