import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

test("Briefing stays readable on wide and narrow screens while Table and Cards fill the workspace", async ({
  page,
}) => {
  await page.setViewportSize({ width: 2400, height: 1000 });
  await page.goto("/notes/tv-guide");
  await chooseView(page, "Briefing");
  const frame = page.frameLocator("iframe.custom-view-frame");
  await expect(frame.getByRole("region", { name: "Needs attention", exact: true })).toBeVisible();
  const main = frame.getByRole("main");

  for (const width of [2400, 800]) {
    await page.setViewportSize({ width, height: 1000 });

    const geometry = await main.evaluate((element) => {
      const bounds = element.getBoundingClientRect();
      const document = element.ownerDocument.documentElement;

      return {
        width: bounds.width,
        left: bounds.left,
        available: document.clientWidth,
        scroll: document.scrollWidth,
      };
    });

    if (width === 2400) expect(geometry.available).toBeGreaterThan(1400);
    else expect(geometry.available).toBeLessThan(1400);

    expect(geometry.width).toBeCloseTo(Math.min(1400, geometry.available), 0);
    expect(geometry.left).toBeCloseTo((geometry.available - geometry.width) / 2, 0);
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.available);
  }

  await page.setViewportSize({ width: 2400, height: 1000 });

  for (const [view, selector] of [
    ["Table", ".configured-view__table-wrap"],
    ["Cards", ".configured-view__cards"],
  ]) {
    await chooseView(page, view);
    const content = page.locator(selector);
    await expect(content).toBeVisible();

    const geometry = await content.evaluate((element) => ({
      width: element.getBoundingClientRect().width,
      available: element.closest(".configured-view")!.getBoundingClientRect().width,
    }));

    expect(geometry.width).toBeGreaterThan(1400);
    expect(geometry.width).toBeCloseTo(geometry.available, 0);
  }
});
