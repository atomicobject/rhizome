import { expect, test } from "./fixtures";

test("root folder containment and folder focus", async ({ page }, testInfo) => {
  await page.goto("/explorer");
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  await expect(page.locator(".graph-type-legend input")).toHaveCount(0);
  await page.getByRole("button", { name: "Display", exact: true }).click();

  const sizeToggle = page.getByRole("checkbox", {
    name: "Size counts the notes and code files a folder holds",
  });

  await expect(sizeToggle).toBeVisible();
  const checked = await sizeToggle.isChecked();
  await sizeToggle.click();
  await expect(sizeToggle).toBeChecked({ checked: !checked });
  await sizeToggle.click();
  await page.getByRole("button", { name: "Display", exact: true }).click();
  const complete = await page.request.get("/api/v1/graphs/global?limit=200000");
  const data = await complete.json();
  expect(data.truncated ?? false).toBe(false);
  const notes = data.nodes.filter((n: { kind: string }) => n.kind === "note").length;
  const code = data.nodes.filter((n: { kind: string }) => n.kind === "code").length;
  await expect(page.locator(".view-header__count")).toHaveText(
    `${notes.toLocaleString("en-US")} notes · ${code.toLocaleString("en-US")} code files`,
  );
  await expect(page.getByText("Part of the graph is hidden")).toHaveCount(0);

  const initialSrc = page
    .locator(".explorer-rail__body > .tree-list > .tree-item > .tree-row")
    .filter({ hasText: /^src$/ });

  if ((await initialSrc.getAttribute("aria-expanded")) === "true") {
    await initialSrc.click();
    await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  }

  const before = await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label");

  const nodeCount = (label: string | null) =>
    Number(label?.match(/Graph of ([\d,]+)/)?.[1]?.replaceAll(",", ""));

  const initialNodes = nodeCount(before);
  await page.screenshot({ path: testInfo.outputPath("explorer-root.png") });

  const src = page
    .locator(".explorer-rail__body > .tree-list > .tree-item > .tree-row")
    .filter({ hasText: /^src$/ });

  const started = performance.now();
  await src.click();
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  console.log(`Explorer folder click to ready: ${Math.round(performance.now() - started)} ms`);
  await expect
    .poll(async () =>
      nodeCount(await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label")),
    )
    .toBeGreaterThan(0);
  await expect(src).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("heading", { name: "Repository totals", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Lines", exact: true })).toBeVisible();
  await expect(page.locator(".explorer-header__breadcrumb")).toHaveText("src ·");
  await expect(page.locator(".explorer-header__totals")).toContainText(
    `of ${notes.toLocaleString("en-US")} notes`,
  );

  const expandedNodes = nodeCount(
    await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label"),
  );

  await src.click();
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  await expect
    .poll(async () =>
      nodeCount(await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label")),
    )
    .toBeLessThan(expandedNodes);
  await page.locator(".explorer-rail__root").click();
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  await expect
    .poll(async () =>
      nodeCount(await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label")),
    )
    .toBe(initialNodes);
  await src.click();
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  await expect
    .poll(async () =>
      nodeCount(await page.getByRole("img", { name: /^Graph of / }).getAttribute("aria-label")),
    )
    .toBe(expandedNodes);
  await expect(src).toHaveAttribute("aria-current", "page");
  await expect(page.locator(".explorer-pane")).toHaveCount(0);
  await page
    .locator(".explorer-rail")
    .getByRole("button", { name: /todoapp/i })
    .click();
  await page
    .locator(".explorer-rail")
    .getByRole("button", { name: /main\.py/i })
    .click();
  await expect(page.locator(".explorer-pane__code")).toBeVisible();
  await expect(page.locator('.explorer-rail .tree-row[aria-current="page"]')).toHaveText(
    /main\.py/i,
  );
  await expect(page.locator(".explorer-pane__header h2")).toHaveCSS("font-size", "16px");
  await expect(page.locator(".explorer-pane__header h2")).toHaveCSS("font-weight", "600");
});
