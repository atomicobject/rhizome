import { expect, test } from "./fixtures";

test("scrolls type details inside the workspace", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto("/ontology/type/Spec");
  await expect(page.getByRole("heading", { name: "Spec", exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "Schema", exact: true }).click();
  const workspace = page.locator(".ontology-atlas-workspace");

  await expect
    .poll(() => workspace.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await workspace.hover({ position: { x: 20, y: 20 } });
  await page.mouse.wheel(0, 800);
  await expect.poll(() => workspace.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test("scrolls the sidebar inside itself without a note list", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  await expect(page.getByRole("heading", { name: "Task Flow", exact: true }).first()).toBeVisible();
  const navigation = page.getByRole("complementary", { name: "Notes navigation" });
  const collections = navigation.getByRole("region", { name: "Collections", exact: true });
  const collapsedGroups = collections.getByRole("button", { name: /^Expand / });

  await expect(navigation.getByRole("region", { name: "Note list" })).toHaveCount(0);

  while ((await collapsedGroups.count()) > 0) {
    await collapsedGroups.first().click();
  }

  await expect
    .poll(() => collections.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await collections.hover({ position: { x: 20, y: 20 } });
  await page.mouse.wheel(0, 800);
  await expect.poll(() => collections.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);
});

test("keeps context scoped to the active note while relations open beside it", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  await expect(page.getByRole("heading", { name: "Task Flow", exact: true }).first()).toBeVisible();
  const contextTabs = page.getByRole("tablist", { name: "Note context" });
  await expect(contextTabs).toBeVisible();
  await contextTabs.getByRole("tab", { name: "Info", exact: true }).click();
  const context = page.getByRole("complementary", { name: "Note context" });
  const related = context.getByRole("region", { name: "Focused note context" });
  await related
    .getByRole("button", { name: "Engineering", exact: true })
    .first()
    .click({ modifiers: ["ControlOrMeta"] });
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  // Context follows the active tab: Task Flow lists Engineering, and
  // Engineering lists Task Flow as a note that links to it.
  await expect(related.getByRole("button", { name: "Engineering", exact: true })).toBeVisible();
  await tabs.getByRole("tab", { name: /engineering/i }).click();
  await expect(related.getByRole("button", { name: "Task Flow", exact: true })).toBeVisible();
  await expect(related.getByRole("button", { name: "Engineering", exact: true })).toHaveCount(0);
  const body = page.locator(".ontology-pane__main:visible");
  await expect
    .poll(() => body.evaluate((element) => element.clientWidth))
    .toBeGreaterThanOrEqual(600);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

test("persists the right pane and panel widths, and closes the pane on Home", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(
    page.getByRole("heading", { name: "Search Rewrite", exact: true }).first(),
  ).toBeVisible();
  const contextTabs = page.getByRole("tablist", { name: "Note context" });
  await expect(contextTabs).toBeVisible();

  // ] hides the pane; the choice and a keyboard-resized sidebar survive a reload.
  await page.locator("body").press("]");
  await expect(contextTabs).toBeHidden();
  const sidebarHandle = page.getByRole("separator", { name: "Resize sidebar" });
  await sidebarHandle.focus();
  await sidebarHandle.press("ArrowRight");
  await expect(sidebarHandle).toHaveAttribute("aria-valuenow", "280");
  await page.reload();
  await expect(page.getByRole("button", { name: "Show right pane" })).toBeVisible();
  await expect(contextTabs).toBeHidden();
  await expect(page.getByRole("separator", { name: "Resize sidebar" })).toHaveAttribute(
    "aria-valuenow",
    "280",
  );
  await expect
    .poll(() => page.getByRole("complementary", { name: "Notes navigation" }).boundingBox())
    .toMatchObject({ width: 280 });

  await page.getByRole("button", { name: "Show right pane" }).click();
  await expect(contextTabs).toBeVisible();
  const paneHandle = page.getByRole("separator", { name: "Resize right pane" });
  await paneHandle.focus();
  await paneHandle.press("End");
  await expect
    .poll(() => page.getByRole("complementary", { name: "Note context" }).boundingBox())
    .toMatchObject({ width: 560 });

  // Home has nothing for the pane to show.
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await tabs.getByRole("tab", { name: "Home", exact: true }).click();
  await expect(contextTabs).toBeHidden();
  await expect(page.getByRole("button", { name: "Show right pane" })).toBeDisabled();
  await tabs.getByRole("tab", { name: /Search Rewrite/ }).click();
  await expect(contextTabs).toBeVisible();
});

test("below 1200px the right pane starts closed and opens over the content", async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 800 });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  const article = page.locator(".notes-shell__panel.is-active");
  await expect(article.getByRole("heading", { name: "Search Rewrite", exact: true })).toBeVisible();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeHidden();
  const width = await article.evaluate((element) => element.clientWidth);

  await page.getByRole("button", { name: "Show right pane" }).click();
  const pane = page.getByRole("complementary", { name: "Note context" });
  await expect(pane).toBeVisible();
  // The note keeps its width; the pane covers it, and Escape gives it back.
  expect(await article.evaluate((element) => element.clientWidth)).toBe(width);
  await pane.getByRole("tab", { name: "Info" }).focus();
  await page.keyboard.press("Escape");
  await expect(pane).toBeHidden();
});

test("a hidden sidebar leaves the workspace panel filling the window", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes");
  await page.getByRole("button", { name: "Hide sidebar" }).click();

  const frame = await page.locator(".notes-shell__frame").boundingBox();

  // Only the 6px insets on each side remain.
  expect(frame?.width ?? 0).toBeGreaterThan(1440 - 24);
});
