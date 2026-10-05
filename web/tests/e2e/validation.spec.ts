import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, test } from "./fixtures";
import type { ValidateEnvelope, ValidationDiagnostic, ValidationScope } from "../../src/api/types";

function envelope(count: number): ValidateEnvelope {
  return {
    status: "ok",
    health: count ? "current_issues" : "current_clean",
    generation: 10,
    publishedGeneration: 10,
    snapshot: {
      vaultIdentity: "fixture",
      generation: 10,
      scope: "default",
      selectedChecks: ["broken_links"],
      startedAt: 1,
      finishedAt: Math.floor(Date.now() / 1000),
      durationMs: 5,
      completion: "complete",
      issueCount: count,
      errorCount: 0,
      affectedFileCount: count ? 1 : 0,
      affectedNoteCount: count ? 1 : 0,
      repairActionCount: 0,
      checks: [{ check: "broken_links", outcome: "completed", issueCount: count, durationMs: 5 }],
    },
  };
}

test("production validation publishes coherent scoped diagnostics", async ({ page, request }) => {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/v2/validate");

        return response.ok() ? ((await response.json()).publishedGeneration ?? 0) : 0;
      },
      { timeout: 30000 },
    )
    .toBeGreaterThan(0);
  const state: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();

  const response = await request.get(
    `/api/v1/validation/diagnostics?generation=${state.publishedGeneration}&limit=100`,
  );

  expect(response.ok()).toBe(true);
  const detail = await response.json();
  expect(detail.total).toBe(state.snapshot?.issueCount);
  expect(detail.diagnostics.length).toBeLessThanOrEqual(100);
  await page.addInitScript(() => {
    performance.setResourceTimingBufferSize(2000);
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        performance.measure("validation-long-task", {
          start: entry.startTime,
          duration: entry.duration,
        });
      }
    }).observe({ type: "longtask", buffered: true });
  });
  await page.goto("/notes/issues");
  await expect(page.getByRole("region", { name: "Problems", exact: true })).toBeVisible();
  await expect(page.locator(".validation-status-strip__summary")).not.toHaveText(/Not checked yet/);
  await page.screenshot({ path: test.info().outputPath("validation-production.png") });

  const metrics = await page.evaluate(() => {
    const resources = performance
      .getEntriesByType("resource")
      .filter(
        (entry): entry is PerformanceResourceTiming => entry instanceof PerformanceResourceTiming,
      );

    const validation = resources.filter((entry) =>
      /api\/v[12]\/(validate|validation\/)/.test(entry.name),
    );

    const tasks = performance.getEntriesByName("validation-long-task");

    return {
      requests: validation.length,
      transferredBytes: validation.reduce((sum, entry) => sum + entry.transferSize, 0),
      longTasks: tasks.length,
      longestTaskMs: Math.max(0, ...tasks.map((entry) => entry.duration)),
    };
  });

  await test.info().attach("validation-browser-metrics", {
    body: JSON.stringify(metrics),
    contentType: "application/json",
  });
  console.log("Validation browser metrics", JSON.stringify(metrics));
});

test("clean, stale, and failed validation stay calm and distinct at both desktop sizes", async ({
  page,
}) => {
  let state = envelope(0);
  await page.route("**/api/v2/validate", (route) => route.fulfill({ json: state }));
  await page.route("**/api/v1/validation/diagnostics?*", (route) =>
    route.fulfill({
      json: { generation: 10, diagnostics: [], total: 0 },
    }),
  );

  for (const width of [1440, 1024]) {
    await page.setViewportSize({ width, height: width === 1440 ? 900 : 768 });
    await page.emulateMedia({ reducedMotion: "reduce" });
    await page.goto("/notes/issues");
    await expect(page.getByText("No issues found", { exact: true })).toBeVisible();
    await expect(page.getByText("Needs attention", { exact: true })).toHaveCount(0);
    await expect(page.getByPlaceholder("Search issues…")).toHaveCount(0);
    await expect(page.getByRole("region", { name: "Note list", exact: true })).toHaveCount(0);
    await expect(page.locator(".validation-status-strip--danger")).toHaveCount(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({ path: test.info().outputPath(`validation-clean-${width}.png`) });
  }

  state = { ...state, health: "stale" };
  await page.reload();
  await expect(page.getByText("Results are out of date", { exact: true })).toBeVisible();
  await expect(page.getByText("No issues found", { exact: true })).toHaveCount(0);
  state = { ...state, status: "error", health: "failed", error: "Fixture refresh unavailable" };
  await page.reload();
  await expect(page.getByText("Refresh failed", { exact: true })).toBeVisible();
  await expect(page.getByText("Fixture refresh unavailable", { exact: true })).toBeVisible();
});

test("501 diagnostics remain reachable and filtered-empty is not clean", async ({ page }) => {
  const issues: ValidationDiagnostic[] = Array.from({ length: 501 }, (_, index) => ({
    issueKey: `fixture-${index}`,
    check: "broken_links",
    code: "broken_note_link",
    message: `Missing target ${index}`,
    target: `missing-${index}`,
    primaryPath: "notes/task-flow.md",
    affectedPaths: ["notes/task-flow.md"],
  }));

  await page.route("**/api/v2/validate", (route) => route.fulfill({ json: envelope(501) }));
  await page.route("**/api/v1/validation/diagnostics?*", (route) => {
    const params = new URL(route.request().url()).searchParams;
    const start = Number(params.get("cursor") || 0);
    const filtered = params.get("text") ? [] : issues;

    return route.fulfill({
      json: {
        generation: 10,
        diagnostics: filtered.slice(start, start + 100),
        total: filtered.length,
        nextCursor: start + 100 < filtered.length ? String(start + 100) : undefined,
      },
    });
  });
  await page.route("**/api/v1/validation/summaries", (route) => {
    const body = route.request().postDataJSON();
    const count = body.filter?.text ? 0 : 501;

    return route.fulfill({
      json: {
        generation: 10,
        summaries: body.scopes.map((scope: ValidationScope) => ({
          scope,
          issueCount: count,
          affectedFileCount: count ? 1 : 0,
          affectedNoteCount: count ? 1 : 0,
          repairActionCount: 0,
        })),
      },
    });
  });
  await page.goto("/notes/issues");
  await expect(page.locator("[data-issue-key]")).toHaveCount(100);

  for (const count of [200, 300, 400, 500, 501]) {
    await page.getByRole("button", { name: /^Load more/ }).click();
    await expect(page.locator("[data-issue-key]")).toHaveCount(count);
  }

  const last = page.locator('[data-issue-key="fixture-500"]');
  await last.click();
  await expect(page.getByRole("article", { name: "Issue detail" })).toContainText(
    "Missing target 500",
  );
  await page.getByRole("button", { name: "Previous issue", exact: true }).click();
  await expect(page.getByRole("article", { name: "Issue detail" })).toContainText(
    "Missing target 499",
  );
  await page.getByRole("button", { name: "Previous issue", exact: true }).press("Escape");
  await expect(page.locator('[data-issue-key="fixture-499"]')).toBeFocused();
  await page.getByPlaceholder("Search issues…").fill("no match");
  await expect(page.getByText("No issues match these filters.", { exact: true })).toBeVisible();
  await expect(page.getByText("No issues found", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Clear filters", exact: true }).click();
  await expect(page.locator("[data-issue-key]")).toHaveCount(501);
});

test("filesystem changes refresh Problems and manual refresh publishes another generation", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  await page.goto("/notes/issues");
  const refreshButton = page.getByRole("button", { name: "Refresh validation", exact: true });
  await expect(refreshButton).toBeEnabled({ timeout: 30000 });
  const before: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();
  const status = await (await request.get("/api/v1/status")).json();
  const notePath = path.join(status.vaultPath, "notes", "task-flow.md");
  const original = await readFile(notePath, "utf8");
  const missing = `validation-refresh-missing-${Date.now()}`;

  try {
    await writeFile(notePath, `${original}\n\n[[${missing}]]\n`);
    await expect
      .poll(
        async () => {
          const current: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();

          if (current.publishedGeneration <= before.publishedGeneration) return false;

          const response = await request.get(
            `/api/v1/validation/diagnostics?generation=${current.publishedGeneration}&text=${missing}`,
          );

          return response.ok() && (await response.json()).total > 0;
        },
        { timeout: 30000 },
      )
      .toBe(true);
    await page.getByPlaceholder("Search issues…").fill(missing);
    await expect(page.locator("[data-issue-key]")).toHaveCount(1);
    await page.getByPlaceholder("Search issues…").fill("");
    const current: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();

    const accepted = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v2/validate/refresh") &&
        response.request().method() === "POST",
    );

    await refreshButton.click();
    expect((await accepted).status()).toBe(202);
    await expect
      .poll(
        async () => {
          const state: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();

          return state.publishedGeneration;
        },
        { timeout: 30000 },
      )
      .toBeGreaterThan(current.publishedGeneration);
    await expect(refreshButton).toBeEnabled();
    await page.screenshot({ path: test.info().outputPath("validation-refresh.png") });
  } finally {
    await writeFile(notePath, original);
    await expect
      .poll(
        async () => {
          const current: ValidateEnvelope = await (await request.get("/api/v2/validate")).json();

          const response = await request.get(
            `/api/v1/validation/diagnostics?generation=${current.publishedGeneration}&text=${missing}`,
          );

          return response.ok() ? (await response.json()).total : -1;
        },
        { timeout: 30000 },
      )
      .toBe(0);
  }
});
