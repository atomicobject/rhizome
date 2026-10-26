import { expect, test } from "./fixtures";

// One workbench sidebar, toggled only by the user (SPEC-0127).

test("keeps the sidebar where the user put it across searches, reloads, and sections", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes");
  const sidebar = page.locator(".workbench-sidebar");
  const rail = page.getByRole("complementary", { name: "Notes navigation" });
  await expect(rail).toBeVisible();

  // A project search keeps it.
  const search = page.getByRole("searchbox", { name: "Search this project" });
  await search.fill("typed retrieval");
  await search.press("Enter");
  await expect(page.getByRole("heading", { name: "Search results" })).toBeVisible();
  await expect(rail).toBeVisible();

  // The top-bar toggle hides it, and it stays hidden after a reload.
  await page.getByRole("button", { name: "Hide sidebar" }).click();
  await expect(sidebar).toBeHidden();
  await page.reload();
  await expect(page.getByRole("button", { name: "Show sidebar" })).toBeVisible();
  await expect(sidebar).toBeHidden();

  // GraphQL has no sidebar: the toggle is disabled there and the choice holds.
  await page
    .getByRole("navigation", { name: "Primary" })
    .getByRole("button", { name: "GraphQL" })
    .click();
  await expect(page.getByRole("button", { name: "Show sidebar" })).toBeDisabled();
  await page
    .getByRole("navigation", { name: "Primary" })
    .getByRole("button", { name: "Notes" })
    .click();
  await expect(sidebar).toBeHidden();

  // The keyboard shows it again; focus is on the Notes section button.
  await page.keyboard.press("ControlOrMeta+Backslash");
  await expect(rail).toBeVisible();
});
