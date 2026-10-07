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

test("scrolls collections and the note list independently inside the navigation rail", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  await expect(page.getByRole("heading", { name: "Task Flow", exact: true }).first()).toBeVisible();
  const navigation = page.getByRole("complementary", { name: "Notes navigation" });
  const collections = navigation.getByRole("region", { name: "Collections", exact: true });
  const notes = navigation.getByRole("region", { name: "Note list", exact: true });
  const collapsedGroups = collections.getByRole("button", { name: /^Expand / });

  while ((await collapsedGroups.count()) > 0) {
    await collapsedGroups.first().click();
  }

  await expect
    .poll(() => collections.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await expect
    .poll(() => notes.evaluate((element) => element.scrollHeight - element.clientHeight))
    .toBeGreaterThan(0);
  await collections.evaluate((element) => {
    element.scrollTop = 0;
  });
  await notes.evaluate((element) => {
    element.scrollTop = 0;
  });
  await collections.hover({ position: { x: 20, y: 20 } });
  await page.mouse.wheel(0, 800);
  await expect.poll(() => collections.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await notes.evaluate((element) => element.scrollTop)).toBe(0);
  const collectionsScroll = await collections.evaluate((element) => element.scrollTop);

  await notes.hover({ position: { x: 20, y: 20 } });
  await page.mouse.wheel(0, 800);
  await expect.poll(() => notes.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await collections.evaluate((element) => element.scrollTop)).toBe(collectionsScroll);
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

test("persists context collapse and collapses context on Home", async ({ page }) => {
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(
    page.getByRole("heading", { name: "Search Rewrite", exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeVisible();
  await page.getByRole("button", { name: "Collapse note context" }).click();
  await page.reload();
  await expect(page.getByRole("button", { name: "Expand note context" })).toBeVisible();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeHidden();
  await page.getByRole("button", { name: "Expand note context" }).click();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await tabs.getByRole("tab", { name: "Home", exact: true }).click();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeHidden();
  await tabs.getByRole("tab", { name: /Search Rewrite/ }).click();
  await expect(page.getByRole("tablist", { name: "Note context" })).toBeVisible();
});
