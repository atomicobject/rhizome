import { expect, test } from "./fixtures";
import type { GraphResponse } from "../../src/api/types";
import { expectView, workspaceView } from "./workspaceView";

test("the vault graph fills the All home and returns after a type collection", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes");
  const graph = page.locator(".home-graph-layout__graph");
  await expect(graph.locator(".ontology-home__hint")).toHaveText(/^[1-9]\d* nodes/);
  await expect(graph.locator("canvas").first()).toBeVisible();
  const counts = await graph.locator(".ontology-home__hint").textContent();

  // A type opens its own views; Overview is no longer a type choice (SPEC-0112).
  await page.goto("/notes/spec");
  await expectView(page, "Table");
  await expect(
    workspaceView(page).getByRole("button", { name: "Overview", exact: true }),
  ).toHaveCount(0);
  await expect(graph).toHaveCount(0);

  await page.getByRole("button", { name: /^All notes \(/i }).click();
  await expect(page).toHaveURL(/\/notes$/);
  await expect(graph.locator(".ontology-home__hint")).toHaveText(counts ?? "");
  await expect.poll(() => graph.evaluate((element) => element.clientHeight)).toBeGreaterThan(400);
  // The renderer surface itself must fill the graph region, not just its frame.
  await expect
    .poll(() => graph.locator(".graph-container").evaluate((element) => element.clientHeight))
    .toBeGreaterThan(400);
  await expect
    .poll(() =>
      graph.evaluate((element) => element.getBoundingClientRect().bottom - window.innerHeight),
    )
    .toBeLessThanOrEqual(1);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

test("keeps the vault graph mounted when an index event refreshes only summary counts", async ({
  page,
}) => {
  // After an edit or a validation run, the next index event refetches a summary
  // whose type names are unchanged and whose counts differ.
  let sendIndexEvent = () => {};

  const indexEvent = new Promise<void>((resolve) => {
    sendIndexEvent = resolve;
  });

  let summaryReads = 0;
  await page.route("**/api/v1/ontology/summary", async (route) => {
    summaryReads += 1;
    const response = await route.fetch();
    const summary = await response.json();

    if (summaryReads > 1) {
      summary.types = summary.types.map(
        (type: { name: string; label?: string; issueCount?: number }) => ({
          ...type,
          issueCount: (type.issueCount ?? 0) + 1,
          label: type.name === "Team" ? "Refreshed teams" : type.label,
        }),
      );
    }

    await route.fulfill({ response, json: summary });
  });
  await page.route("**/api/v1/events", async (route) => {
    await indexEvent;
    await route.fulfill({
      headers: { "content-type": "text/event-stream" },
      body: "retry: 600000\nevent: index.changed\ndata: {}\n\n",
    });
  });
  await page.goto("/notes");
  const graph = page.locator(".home-graph-layout__graph");
  const canvas = graph.locator("canvas").first();
  await expect(canvas).toBeVisible();
  const originalCanvas = await canvas.elementHandle();

  if (!originalCanvas) throw new Error("Expected the rendered vault graph");
  sendIndexEvent();
  await expect(graph.locator('.graph-type-legend__item[data-type="Team"]')).toContainText(
    "Refreshed teams",
  );
  expect(await originalCanvas.evaluate((element) => element.isConnected)).toBe(true);
});

test("graph clicks retain existing file tabs and reuse the target file tab", async ({ page }) => {
  const graph: GraphResponse = {
    nodes: [
      {
        id: "note:communities/engineering.md",
        label: "Engineering",
        kind: "note",
        notePath: "communities/engineering.md",
        path: "communities/engineering.md",
        nodeRef: { notePath: "communities/engineering.md", kind: "NOTE" },
        resolvedType: "Community",
      },
    ],
    edges: [],
    truncated: false,
  };

  await page.route(
    (url) => url.pathname === "/api/v1/graphs/global",
    (route) => route.fulfill({ json: graph }),
  );
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  const taskTab = tabs.getByRole("tab", { name: /^Task Flow(?:,|$)/ });
  const engineeringTab = tabs.getByRole("tab", { name: /^Engineering(?:,|$)/ });
  const homeTab = tabs.getByRole("tab", { name: "Home", exact: true });
  await expect(taskTab).toHaveAttribute("aria-selected", "true");
  await expect(tabs.getByRole("tab")).toHaveCount(2);

  // Sigma normalizes a single node to the canvas center, independent of layout jitter.
  // Hover verifies real renderer picking before each real mouse click.
  const mouseCanvas = page.locator(".home-graph-layout__graph canvas.sigma-mouse");
  const hoverLabel = page.locator(".home-graph-layout__graph .graph-hover-label");

  for (let visit = 0; visit < 2; visit += 1) {
    await test.step(`Graph visit ${visit + 1}`, async () => {
      await homeTab.click();
      await expect(mouseCanvas).toBeVisible();
      await page.mouse.move(0, 0);
      await expect(hoverLabel).toHaveCount(0);
      await expect(async () => {
        await mouseCanvas.hover();
        await expect(hoverLabel).toHaveText("Engineering");
      }).toPass();
      // Separate clicks must outlast Sigma's 300ms double-click gesture window.
      await mouseCanvas.click({ delay: 350 });
      await expect(engineeringTab).toHaveAttribute("aria-selected", "true");
      await expect(page).toHaveURL(/note=communities%2Fengineering\.md/);
      await expect(tabs.getByRole("tab")).toHaveCount(3);
      await expect(taskTab).toHaveCount(1);
      await expect(engineeringTab).toHaveCount(1);
    });
  }

  await taskTab.click();
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText("Task Flow");
});
