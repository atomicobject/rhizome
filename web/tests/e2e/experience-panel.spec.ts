import { expect, test, type Page } from "./fixtures";

// The Notes sidebar's panel slot and the built-in Folder panel (SPEC-0136 R6).

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
