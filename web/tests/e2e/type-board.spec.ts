import { expect, type Page, test } from "./fixtures";
import { chooseView } from "./workspaceView";

/** Opens a SPEC-0112 fixture type from the Collections rail. */
async function openCollection(page: Page, name: RegExp) {
  await page.goto("/notes");
  const collections = page.getByRole("region", { name: "Collections" });
  const expand = collections.getByRole("button", { name: "Expand E2E Type views", exact: true });
  await expect(
    collections.getByRole("button", { name: "E2E Type views", exact: true }),
  ).toBeVisible();

  if (await expand.count()) await expand.click();
  await collections.getByRole("button", { name }).click();
}

test("switches board lanes on the workflow type and moves a card between columns", async ({
  page,
}) => {
  await openCollection(page, /^Work items \(\d+\)$/);
  await chooseView(page, "Board");
  const board = page.getByLabel("Board", { exact: true });
  await expect(board.locator(".configured-card").first()).toBeVisible();

  // Priority lanes come in enum order, with records lacking a value last.
  await page.getByLabel("Board lanes").selectOption({ label: "Priority" });
  const lanes = board.locator(".configured-view__board-lane");
  await expect(lanes.locator(".configured-view__board-lane-header")).toHaveText([
    /High/,
    /Medium/,
    /Low/,
    /No priority/,
  ]);

  await page.getByLabel("Board lanes").selectOption({ label: "None" });
  await expect(board.locator(".configured-view__board-lane-header")).toHaveCount(0);

  const card = board
    .getByLabel("Backlog column", { exact: true })
    .locator(".configured-card")
    .first();

  const title = ((await card.locator(".configured-card__title").textContent()) ?? "").trim();
  expect(title).not.toBe("");
  await card.focus();
  await page.keyboard.press("m");
  await page.getByRole("menuitem", { name: "Ready", exact: true }).click();

  await expect(
    board
      .getByLabel("Ready column", { exact: true })
      .locator(".configured-card", { hasText: title }),
  ).toBeVisible();
  await expect(
    board.getByLabel("Backlog column", { exact: true }).locator(".configured-card", {
      hasText: title,
    }),
  ).toHaveCount(0);

  // Later specs count staged changes from zero.
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Discard", exact: true }).click();
  await expect(
    board.getByLabel("Backlog column", { exact: true }).locator(".configured-card", {
      hasText: title,
    }),
  ).toBeVisible();
});

test("saves a generated view and opens it as the type's default after reload", async ({ page }) => {
  await openCollection(page, /^Lane projects \(\d+\)$/);
  const rows = page.locator(".configured-view__table tbody tr[data-row-key]");
  await expect(rows).toHaveCount(3);

  const titles = async () =>
    (await rows.locator(".configured-view__title-button").allTextContents()).map((text) =>
      text.trim(),
    );

  const ascending = await titles();
  expect(ascending).toEqual([...ascending].sort((a, b) => a.localeCompare(b)));

  await page.getByRole("button", { name: /^Sort Title/ }).click();
  await expect.poll(titles).toEqual([...ascending].reverse());

  const saved = page.waitForResponse(
    (response) => response.url().endsWith("/save") && response.request().method() === "POST",
  );

  await page.getByRole("button", { name: "Save view, unsaved changes" }).click();
  const review = page.getByRole("dialog", { name: "Save shared view configuration" });
  await expect(review).toContainText(".rhizome/views");
  await expect(review).toContainText("Sort");
  await review.getByRole("button", { name: "Write view YAML", exact: true }).click();
  const response = await saved;
  expect(response.ok(), await response.text()).toBe(true);
  await expect(page.getByRole("status").filter({ hasText: /^Saved / })).toContainText(
    ".rhizome/views/",
  );

  await page.reload();
  await openCollection(page, /^Lane projects \(\d+\)$/);
  await expect(rows).toHaveCount(3);
  await expect.poll(titles).toEqual([...ascending].reverse());
  await expect(page.getByRole("button", { name: "Save view", exact: true })).toBeVisible();
});
