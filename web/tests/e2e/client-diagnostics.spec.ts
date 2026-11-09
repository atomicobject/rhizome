import { execFile } from "node:child_process";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { expect, test } from "./fixtures";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

type WebSummary = {
  surface: string;
  frames: { over_200ms: number; longest_ms: number };
  refresh: { cycles: number; refetched: number; unchanged: number };
  regions?: { name: string; mount: number }[];
  experiences?: {
    hash: string;
    frames_created: number;
    locations_reused: number;
    pages_retained: number;
    phases?: { phase: string; samples: number }[];
  }[];
};

type PerfResult = { perf: { flags: { kind: string }[]; web?: WebSummary[] } };

// Reads the vault's persisted evidence offline, the way an agent would. `since`
// scopes it to this test, since a reused server keeps earlier tests' evidence.
async function readPerf(vaultPath: string, since: string): Promise<PerfResult> {
  const { stdout } = await promisify(execFile)(
    "go",
    ["run", ".", "diagnostics", "perf", "--json", "--vault", vaultPath, "--since", since],
    {
      cwd: repoRoot,
      env: {
        ...process.env,
        // Same flags and caches as run-e2e-server.mjs, so the build is reused.
        GOFLAGS: [process.env.GOFLAGS, "-tags=e2efake"].filter(Boolean).join(" "),
        GOCACHE: process.env.GOCACHE || path.join(repoRoot, ".gocache"),
        GOMODCACHE: process.env.GOMODCACHE || path.join(repoRoot, ".gomodcache"),
        GOTMPDIR: process.env.GOTMPDIR || path.join(repoRoot, ".gotmp"),
        RZM_SKIP_REPO_DELEGATE: "1",
      },
    },
  );

  // SAFETY: `rzm diagnostics perf --json` emits the Result shape; a missing
  // field fails the assertions that read it.
  return JSON.parse(stdout) as PerfResult;
}

test("a frame stall and an unchanged background refresh reach rzm diagnostics perf", async ({
  page,
  request,
}) => {
  test.setTimeout(120000);
  const since = new Date().toISOString();
  const status = await (await request.get("/api/v1/status")).json();
  const vaultPath: string = status.vaultPath;
  const notePath = path.join(vaultPath, "notes", "task-flow.md");
  const original = await readFile(notePath, "utf8");

  await page.goto("/notes/issues");
  await expect(page.getByRole("button", { name: "Refresh validation", exact: true })).toBeEnabled({
    timeout: 30000,
  });

  try {
    // An edit to a note this page does not show refetches index queries whose
    // data comes back unchanged.
    await writeFile(notePath, `${original}\n\nclient diagnostics e2e\n`);
    await page.evaluate(() => {
      const end = performance.now() + 300;

      while (performance.now() < end) {
        // Block the main thread so the frame loop sees a gap over 200 ms.
      }
    });

    await expect
      .poll(
        async () => {
          const web = (await readPerf(vaultPath, since)).perf.web?.find(
            (s) => s.surface === "browser",
          );

          return {
            stalled: (web?.frames.over_200ms ?? 0) > 0,
            unchanged: (web?.refresh.unchanged ?? 0) > 0,
            // Production bundles mount no render regions unless the desktop asks.
            regions: web?.regions?.length ?? 0,
          };
        },
        { timeout: 60000, intervals: [5000] },
      )
      .toEqual({ stalled: true, unchanged: true, regions: 0 });

    const result = await readPerf(vaultPath, since);
    expect(result.perf.flags.map((flag) => flag.kind)).toContain("web_frame_stalls");
  } finally {
    await writeFile(notePath, original);
  }
});

test("the desktop render-profiling flag mounts render regions in the production bundle", async ({
  page,
  request,
}) => {
  test.setTimeout(120000);
  const since = new Date().toISOString();
  const status = await (await request.get("/api/v1/status")).json();

  await page.addInitScript(() => {
    window.__RHIZOME_RENDER_PROFILING__ = true;
  });
  await page.goto("/notes/issues");

  await expect
    .poll(
      async () => {
        const web = (await readPerf(status.vaultPath, since)).perf.web?.find(
          (s) => s.surface === "browser",
        );

        return web?.regions?.find((region) => region.name === "app-shell")?.mount ?? 0;
      },
      { timeout: 60000, intervals: [5000] },
    )
    .toBeGreaterThan(0);
});

test("experience frames reach rzm diagnostics perf by hash, under the profiling flag", async ({
  page,
  request,
}) => {
  test.setTimeout(120000);
  const since = new Date().toISOString();
  const status = await (await request.get("/api/v1/status")).json();

  await page.addInitScript(() => {
    window.__RHIZOME_RENDER_PROFILING__ = true;
  });
  // The synthetic experience in fixtures/experience: its home, then a record in a new tab.
  await page.goto("/notes/group/E2E%20Experience");
  const shown = page.frameLocator("iframe.experience-frame[data-shown]");
  await expect(shown.getByRole("heading", { name: "Experience home" })).toBeVisible();
  await shown.getByRole("button", { name: "Open alpha" }).click();
  await expect(shown.getByRole("heading", { name: "Claim page alpha.md" })).toBeVisible();

  let web: WebSummary | undefined;

  await expect
    .poll(
      async () => {
        web = (await readPerf(status.vaultPath, since)).perf.web?.find(
          (s) => s.surface === "browser",
        );
        const experience = web?.experiences?.[0];

        return {
          frames: experience?.frames_created ?? 0,
          phases: experience?.phases?.map((each) => each.phase).sort() ?? [],
          region: web?.regions?.some((region) => region.name === "experience-frames") ?? false,
        };
      },
      { timeout: 60000, intervals: [5000] },
    )
    .toEqual({
      // The page frame, reused for the record, and the sidebar panel's frame.
      frames: 2,
      // The fixture's pages call useMarkReady, so every kit phase is sampled.
      phases: ["data-ready", "first-render", "location-painted", "module-loaded", "shell-start"],
      region: true,
    });

  // The experience is named by a hash, never its folder path or title.
  expect(web?.experiences?.[0]?.hash).toMatch(/^[0-9a-f]{14}$/);
  expect(JSON.stringify(web)).not.toMatch(/E2E Experience|alpha|experience\//i);
});
