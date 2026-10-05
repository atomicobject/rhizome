import { expect, test } from "./fixtures";
import type { ViewExecuteResponse } from "../../src/api/types";
import { chooseView } from "./workspaceView";

test("previews configured row titles in every presentation", async ({ page }) => {
  await page.goto("/notes?view=e2e.action-items");
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  for (const variant of ["Table", "Board", "Cards"]) {
    await chooseView(page, variant);
    await expect(page.getByRole("button", { name: "Refresh view", exact: true })).toBeEnabled();

    const title = page
      .locator(variant === "Table" ? ".configured-view__scalar-cell" : ".configured-card__title")
      .first();

    const text = (await title.textContent())?.trim();
    expect(text).toBeTruthy();
    await title.focus();
    const preview = page.locator(".note-preview[role=region]");
    await expect(preview).toBeVisible();
    await expect(preview.locator(".note-preview__title")).toHaveText(text ?? "");
    await page.keyboard.press("Escape");
    await expect(preview).toBeHidden();
  }
});

test("renders relation links in card eyebrows and previews", async ({ page }) => {
  await page.route("**/api/v1/views/e2e.action-items/execute", async (route) => {
    const response = await route.fetch();
    const execution: ViewExecuteResponse = await response.json();
    execution.card = {
      ...execution.card,
      fields: execution.card?.fields ?? [],
      title: { field: "title" },
      eyebrow: { field: "assignedTo" },
      preview: { field: "assignedTo" },
    };
    await route.fulfill({ response, json: execution });
  });
  await page.goto("/notes?view=e2e.action-items");
  await chooseView(page, "Cards");
  const card = page.locator(".configured-card").first();

  for (const selector of [".configured-card__eyebrow", ".configured-card__preview"]) {
    const slot = card.locator(selector);
    await expect(slot).not.toContainText("[[");
    const link = slot.getByRole("link");
    await expect(link).toHaveText("Bob");
    await link.focus();
    await expect(page.getByRole("region", { name: "Preview of Bob", exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
  }

  await card.locator(".configured-card__preview").getByRole("link").click();
  await expect(page).toHaveURL(/note=notes%2Fpeople%2Fbob\.md/);
});

test("opens HTML notes from the Explorer tree in the note workspace", async ({ page }) => {
  await page.goto("/explorer");
  const rail = page.locator(".explorer-rail");
  await rail.getByRole("button", { name: /reports/i }).click();
  await rail.getByRole("button", { name: /prototype\.html/i }).click();
  await expect(page).toHaveURL(/\/notes\?note=reports%2Fprototype\.html/);
  await expect(
    page.locator('iframe[title="Interactive HTML note: reports/prototype.html"]'),
  ).toBeVisible();
});

test("previews child sections and collection items before opening them", async ({ page }) => {
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await chooseView(page, "Structure");
  const stories = page.locator('.body-child-link[data-field="stories"]');
  await stories.focus();
  const preview = page.locator(".note-preview[role=region]");
  await expect(preview).toBeVisible();
  await expect(preview.locator(".note-preview__title")).toHaveText("Stories");
  await page.keyboard.press("Escape");
  await stories.click();
  const story = page.locator(".body-collection__row:visible").first();
  await story.focus();
  await expect(preview).toBeVisible();
  await expect(preview.locator(".note-preview__title")).toHaveText("Stable typed retrieval");
  await page.keyboard.press("Escape");
  await story.click();
  await expect(page).toHaveURL(/#%5Estory-001/);
});

test("keeps focused note previews open and returns focus after Escape", async ({ page }) => {
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  const context = page.getByRole("complementary", { name: "Note context" });
  await context.getByRole("tab", { name: "Info", exact: true }).click();
  const sections = context.getByRole("heading", { name: "Sections", exact: true }).locator("..");
  const section = sections.getByRole("button").first();
  await section.focus();
  await section.hover();
  const preview = page.locator(".note-preview[role=region]");
  await expect(preview).toBeVisible();
  await expect(preview.locator(".note-preview__title")).toHaveText("Requirements");
  await page.mouse.move(0, 0);
  await expect(preview).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(preview).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(preview).toBeHidden();
  await expect(section).toBeFocused();
});

test("keeps Explorer suggestions available for keyboard activation", async ({ page }) => {
  await page.goto("/explorer");
  await page.getByPlaceholder("Search notes + code").fill("task-flow");
  const suggestion = page.locator(".explorer-suggestions button").first();
  await expect(suggestion).toBeVisible();
  await suggestion.focus();
  await page.waitForTimeout(200);
  await expect(suggestion).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/notes\?note=notes%2Ftask-flow\.md/);
});
