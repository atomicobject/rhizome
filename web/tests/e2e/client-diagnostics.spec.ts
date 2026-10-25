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
};

type PerfResult = { perf: { flags: { kind: string }[]; web?: WebSummary[] } };

// Reads the vault's persisted evidence offline, the way an agent would.
async function readPerf(vaultPath: string): Promise<PerfResult> {
  const { stdout } = await promisify(execFile)(
    "go",
    ["run", ".", "diagnostics", "perf", "--json", "--vault", vaultPath],
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
          const web = (await readPerf(vaultPath)).perf.web?.find((s) => s.surface === "browser");

          return {
            stalled: (web?.frames.over_200ms ?? 0) > 0,
            unchanged: (web?.refresh.unchanged ?? 0) > 0,
          };
        },
        { timeout: 60000, intervals: [5000] },
      )
      .toEqual({ stalled: true, unchanged: true });

    const result = await readPerf(vaultPath);
    expect(result.perf.flags.map((flag) => flag.kind)).toContain("web_frame_stalls");
  } finally {
    await writeFile(notePath, original);
  }
});
