import { expect, test, type Page } from "./fixtures";
import { feedbackScreenshot } from "./groupViewHelpers";

// The Notes sidebar's panel slot, the built-in Folder panel, and a custom
// panel view on the synthetic experience in fixtures/experience (SPEC-0136 R6).

const NOTE = "notes/decisions/backlink-decision.md";

async function openNote(page: Page, notePath = NOTE) {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/notes?${new URLSearchParams({ note: notePath })}`);

  return page
    .getByRole("complementary", { name: "Notes navigation" })
    .getByRole("region", { name: "Folder panel" });
}

test("Folder starts minimised, remembers being opened, opens notes, and links its folder to Explorer", async ({
  page,
}) => {
  const panel = await openNote(page);
  const title = panel.getByRole("button", { name: "Folder", exact: true });
  const contents = panel.getByRole("navigation", { name: "Folder contents" });

  await expect(title).toHaveAttribute("aria-expanded", "false");
  await expect(contents).toBeHidden();

  await title.click();
  await expect(title).toHaveAttribute("aria-expanded", "true");
  await expect(contents.getByRole("button", { name: "notes", exact: true })).toBeVisible();
  await expect(contents.getByRole("button", { name: "decisions", exact: true })).toBeVisible();
  const current = contents.locator('[aria-current="page"]');
  await expect(current).toHaveCount(1);
  const siblings = contents.locator(".folder-panel__row--note");
  await expect(siblings).toHaveCount(3);

  // Opened once, it stays open in this vault.
  await page.reload();
  await expect(title).toHaveAttribute("aria-expanded", "true");

  // A sibling opens in Notes, and the panel follows it without remounting.
  const marker = await contents.evaluate((element) => {
    element.setAttribute("data-e2e-mounted", "yes");

    return element.getAttribute("data-e2e-mounted");
  });

  expect(marker).toBe("yes");
  const sibling = siblings.filter({ hasNot: page.locator('[aria-current="page"]') }).first();
  const siblingName = (await sibling.textContent()) ?? "";
  await siblings.filter({ hasText: siblingName }).click();
  await expect(page).toHaveURL(/note=notes%2Fdecisions%2F(?!backlink)/);
  await expect(contents.locator('[aria-current="page"]')).toHaveText(siblingName);
  await expect(contents).toHaveAttribute("data-e2e-mounted", "yes");

  // The folder's name opens Explorer there.
  await contents.getByRole("button", { name: "decisions", exact: true }).click();
  await expect(page).toHaveURL(/\/explorer\?folder=notes%2Fdecisions$/);
});

test("the panel splits, covers, and minimises with keyboard-reachable controls", async ({
  page,
}) => {
  const panel = await openNote(page);
  const rail = page.getByRole("complementary", { name: "Notes navigation" });
  const collections = rail.getByRole("region", { name: "Collections" });
  const title = panel.getByRole("button", { name: "Folder", exact: true });
  await title.click();
  await expect(title).toHaveAttribute("aria-expanded", "true");

  // The divider resizes the split in 16px steps from the keyboard.
  const divider = panel.getByRole("separator", { name: "Resize Folder panel" });
  const before = Number(await divider.getAttribute("aria-valuenow"));
  await divider.focus();
  await page.keyboard.press("ArrowUp");
  await expect(divider).toHaveAttribute("aria-valuenow", String(before + 16));
  await expect
    .poll(async () => Math.round((await panel.boundingBox())?.height ?? 0))
    .toBe(before + 16);

  // Cover hides the navigation; Escape gives it back.
  const cover = panel.getByRole("button", { name: "Fill the sidebar with Folder" });
  await cover.click();
  await expect(cover).toHaveAttribute("aria-pressed", "true");
  await expect(collections).toBeHidden();
  await panel.locator('[aria-current="page"]').focus();
  await page.keyboard.press("Escape");
  await expect(cover).toHaveAttribute("aria-pressed", "false");
  await expect(collections).toBeVisible();

  // Minimising from inside the panel keeps focus on its title.
  await panel.locator('[aria-current="page"]').focus();
  await title.press("Enter");
  await expect(title).toHaveAttribute("aria-expanded", "false");
  await expect(title).toBeFocused();

  // The sidebar shortcut still works with focus in the panel.
  await page.keyboard.press("ControlOrMeta+Backslash");
  await expect(rail).toBeHidden();
  await page.keyboard.press("ControlOrMeta+Backslash");
  await expect(rail).toBeVisible();
});

test("a page without a note holds no panel", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes");
  const rail = page.getByRole("complementary", { name: "Notes navigation" });

  await expect(rail.getByRole("region", { name: "Collections" })).toBeVisible();
  await expect(rail.getByRole("region", { name: "Folder panel" })).toHaveCount(0);
});

test("a custom panel keeps one frame and its state across subjects, covers, minimises, and gives way to Folder", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/notes/group/E2E%20Experience");
  const rail = page.getByRole("complementary", { name: "Notes navigation" });
  const collections = rail.getByRole("region", { name: "Collections" });
  const slot = rail.getByRole("region", { name: "Claims panel" });
  const panelFrames = page.locator("iframe.panel-frame");
  const panel = page.frameLocator("iframe.panel-frame[data-shown]");
  const shownPage = page.frameLocator("iframe.experience-frame[data-shown]");
  const subject = panel.getByLabel("Panel subject");
  const marks = panel.getByRole("button", { name: /^Marked/ });

  const tab = (name: RegExp) =>
    page.getByRole("tablist", { name: "Open notes" }).getByRole("tab", { name });

  // The group's subject shows the experience's panel in its own frame.
  await expect(subject).toHaveText("E2E Experience");
  await expect(panelFrames).toHaveCount(1);
  expect(await panelFrames.getAttribute("src")).toMatch(/[?&]frame=experience(&|$)/);
  const start = await panel.getByLabel("Panel frame").textContent();
  await marks.click();
  await expect(marks).toHaveText("Marked 1");

  // A record of the experience moves the same page to its subject.
  await shownPage.getByRole("button", { name: "Open alpha" }).click();
  await expect(tab(/Claim alpha/i)).toHaveAttribute("aria-selected", "true");
  await expect(subject).toHaveText("alpha.md");
  await expect(marks).toHaveText("Marked 1");
  await expect(panel.getByLabel("Panel frame")).toHaveText(start ?? "");
  await expect(panelFrames).toHaveCount(1);
  await feedbackScreenshot(page, "experience-panel");

  // Cover fills the sidebar with the same frame.
  const cover = slot.getByRole("button", { name: "Fill the sidebar with Claims" });
  await cover.click();
  await expect(collections).toBeHidden();
  await expect(marks).toHaveText("Marked 1");
  await cover.click();
  await expect(collections).toBeVisible();

  // Minimised, the frame suspends out of sight and comes back as it was.
  const title = slot.getByRole("button", { name: "Claims", exact: true });
  await title.click();
  await expect(title).toHaveAttribute("aria-expanded", "false");
  await expect(page.locator("iframe.panel-frame[data-shown]")).toHaveCount(0);
  await title.click();
  await expect(marks).toHaveText("Marked 1");
  await expect(panel.getByLabel("Panel frame")).toHaveText(start ?? "");

  // The sidebar shortcut works with focus inside the panel's frame.
  await marks.focus();
  await page.keyboard.press("ControlOrMeta+Backslash");
  await expect(rail).toBeHidden();
  await page.keyboard.press("ControlOrMeta+Backslash");
  await expect(rail).toBeVisible();
  await expect(marks).toHaveText("Marked 1");

  // Another tab of the experience keeps the panel.
  await tab(/^Home$/).click();
  await expect(subject).toHaveText("E2E Experience");
  await expect(marks).toHaveText("Marked 1");

  // An untyped note gets Folder; the panel's frame waits, suspended.
  await page.evaluate(() => {
    window.history.pushState({}, "", "/notes?note=notes%2Ftask-flow.md");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(rail.getByRole("region", { name: "Folder panel" })).toBeVisible();
  await expect(slot).toHaveCount(0);
  await expect(panelFrames).toHaveCount(1);
  await expect(page.locator("iframe.panel-frame[data-shown]")).toHaveCount(0);

  await tab(/Claim alpha/i).click();
  await expect(subject).toHaveText("alpha.md");
  await expect(marks).toHaveText("Marked 1");
  await expect(panel.getByLabel("Panel frame")).toHaveText(start ?? "");
});
