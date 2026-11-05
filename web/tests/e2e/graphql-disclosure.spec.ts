import { expect, test } from "./fixtures";

test("opens variables and headers from one labelled disclosure row", async ({ page }) => {
  await page.goto("/graphql");
  const toggle = page.getByRole("button", { name: "Show editor tools", exact: true });
  await expect(toggle).toBeVisible();
  await expect(page.getByRole("button", { name: /^Merge fragments into query/ })).toBeVisible();

  const variables = page
    .locator(".graphiql-editor-tools > button")
    .filter({ hasText: /^Variables$/ });

  await expect(variables).toBeHidden();
  const row = await page.locator(".graphiql-editor-tools").boundingBox();
  const target = await toggle.boundingBox();
  expect(target && row && target.width / row.width).toBeGreaterThan(0.9);
  await toggle.click();
  await expect(variables).toBeVisible();
  await page.getByRole("button", { name: "Hide editor tools", exact: true }).click();
  await expect(variables).toBeHidden();
});
