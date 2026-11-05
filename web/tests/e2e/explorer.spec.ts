import { expect, test } from "./fixtures";

test("root folder containment and folder focus", async ({ page }, testInfo) => {
  await page.goto("/explorer");
  await expect(page.locator('[data-graph-ready="true"]')).toBeVisible();
  await expect(page.getByText(/Lines follow folder containment/)).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("explorer-root.png") });
  const src = page.locator(".explorer-rail").getByRole("button", { name: /\bsrc$/i });
  await src.click();
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
  await expect(page.locator(".explorer-pane__header h2")).toHaveCSS("font-size", "16px");
  await expect(page.locator(".explorer-pane__header h2")).toHaveCSS("font-weight", "600");
});
