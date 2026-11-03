import { expect, test, type Page } from "./fixtures";

const frame = (page: Page) => page.frameLocator("iframe.custom-view-frame:not([data-incoming])");

const started = async (page: Page) =>
  (await frame(page).getByLabel("Mount").textContent())?.replace(/ mount \d+$/, "");

test("a UI param keeps the frame, an identity param remounts it, and Back follows both", async ({
  page,
}) => {
  await page.goto("/notes?view=e2e.views.params&tab=first&ignored=1");
  const params = frame(page).getByLabel("Params");
  await expect(params).toHaveText("tab=first item=one");
  const firstFrame = await started(page);

  await frame(page).getByRole("button", { name: "Second tab" }).click();
  await expect(page).toHaveURL(/[?&]tab=second(&|$)/);
  await expect(params).toHaveText("tab=second item=one");
  expect(await started(page)).toBe(firstFrame);

  await frame(page).getByRole("button", { name: "Item two" }).click();
  await expect(page).toHaveURL(/[?&]item=two(&|$)/);
  await expect(params).toHaveText("tab=second item=two");
  const secondFrame = await started(page);
  expect(secondFrame).not.toBe(firstFrame);

  // Back over the identity change remounts; back over the UI change does not.
  await page.goBack();
  await expect(params).toHaveText("tab=second item=one");
  const thirdFrame = await started(page);
  expect(thirdFrame).not.toBe(secondFrame);

  await page.goBack();
  await expect(params).toHaveText("tab=first item=one");
  expect(await started(page)).toBe(thirdFrame);
});

test("a standalone page applies params to its own URL", async ({ page }) => {
  await page.goto("/views/e2e.views.params?tab=first");
  const params = page.getByLabel("Params");
  await expect(params).toHaveText("tab=first item=one");

  await page.getByRole("button", { name: "Second tab" }).click();
  await expect(page).toHaveURL(/\/views\/e2e\.views\.params\?tab=second$/);
  await expect(params).toHaveText("tab=second item=one");

  await page.goBack();
  await expect(params).toHaveText("tab=first item=one");
});
