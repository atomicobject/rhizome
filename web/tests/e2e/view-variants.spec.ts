import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

test("moves a board card, reports the staged change, and opens a card", async ({ page }) => {
  await page.goto("/notes?view=e2e.action-items");
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  await chooseView(page, "Board");
  // An empty value renders as a narrow strip that still takes drops (SPEC-0112), so count both forms.
  await expect(
    page.locator(".configured-view__board-column, .configured-view__board-strip-cell"),
  ).toHaveCount(2);

  const card = page.locator(".configured-card").first();
  const movedTitle = (await card.locator(".configured-card__title").textContent())?.trim() ?? "";
  expect(movedTitle).not.toBe("");

  const sourceColumn = card.locator(
    "xpath=ancestor::section[contains(@class, 'configured-view__board-column')][1]",
  );

  const sourceLabel = (await sourceColumn.getAttribute("aria-label")) ?? "";
  expect(sourceLabel).toMatch(/ column$/);
  await card.focus();
  await page.keyboard.press("m");
  const targetLabel = (await page.getByRole("menuitem").first().textContent())?.trim() ?? "";
  expect(targetLabel).not.toBe("");
  expect(`${targetLabel} column`).not.toBe(sourceLabel);
  await page.getByRole("menuitem").first().click();

  const targetColumn = page.getByLabel(`${targetLabel} column`, { exact: true });
  await expect(targetColumn.locator(".configured-card", { hasText: movedTitle })).toBeVisible();
  await expect(
    page
      .getByLabel(sourceLabel, { exact: true })
      .locator(".configured-card", { hasText: movedTitle }),
  ).toHaveCount(0);

  await expect(
    page.locator(".configured-card", { hasText: movedTitle }).getByText("staged", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Changes 1" })).toBeVisible();
  await expect(page.getByText("1 staged change", { exact: true })).toBeVisible();

  await chooseView(page, "Cards");
  // The previous layout's rows stay on screen until the Cards execution settles.
  await expect(page.getByRole("button", { name: "Refresh view", exact: true })).toBeEnabled();
  const title = page.locator(".configured-card__title").first();
  const openedTitle = (await title.innerText()).trim();
  await title.click();

  await expect(page).toHaveURL((url) => url.pathname === "/notes" && url.searchParams.has("note"));
  await expect(
    page.getByRole("tablist", { name: "Open notes" }).getByRole("tab", { selected: true }),
  ).toContainText(openedTitle);
});

test("reorders a table row by keyboard in a view sorted by an editable field", async ({ page }) => {
  await page.goto("/notes?view=e2e.action-items");
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  // Sorting by the status enum makes the view orderable. Earlier specs may have
  // saved other statuses, so read the values instead of assuming them.
  await page.getByRole("button", { name: "Sort Status ascending" }).click();
  await expect(page.getByRole("button", { name: "Refresh view", exact: true })).toBeEnabled();
  const rows = page.locator("tr.configured-view__row");
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toHaveAttribute("aria-keyshortcuts", /Alt\+ArrowDown/);
  const status = (row: typeof rows) => row.getByRole("button", { name: "Edit Status" });
  const statusOf = async (row: typeof rows) => (await status(row).textContent())?.trim() ?? "";

  // A move needs two different values; stage one when earlier specs left them equal.
  if ((await statusOf(rows.first())) === (await statusOf(rows.last()))) {
    const last = rows.last();
    const other = (await statusOf(last)).toLowerCase() === "open" ? "blocked" : "open";
    await status(last).click();
    await last.getByRole("combobox", { name: "Edit Status" }).selectOption(other);
    await expect
      .poll(async () => (await statusOf(rows.first())) !== (await statusOf(rows.last())))
      .toBe(true);
  }

  await expect(page.getByRole("button", { name: "Refresh view", exact: true })).toBeEnabled();
  const movedTitle = (await rows.first().locator("td").last().textContent())?.trim() ?? "";
  const firstStatus = await statusOf(rows.first());
  const nextStatus = await statusOf(rows.last());
  expect(movedTitle).not.toBe("");
  expect(firstStatus).not.toBe(nextStatus);
  const moved = rows.filter({ hasText: movedTitle });

  let releaseReads!: () => void;

  const readsHeld = new Promise<void>((resolve) => {
    releaseReads = resolve;
  });

  await page.route("**/api/v1/views/e2e.action-items/execute", async (route) => {
    await readsHeld;
    await route.continue();
  });

  try {
    await rows.first().focus();
    await page.keyboard.press("Alt+ArrowDown");

    // Moving past the next row takes its status; the row keeps focus wherever it lands.
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
    await expect(status(moved)).toHaveText(nextStatus);
    await expect(moved).toBeFocused();
    const savedOrder = await rows.locator("td:last-child").allTextContents();

    const committed = page.waitForResponse(
      (response) =>
        response.url().includes("/api/v1/edit-sessions/") && response.url().endsWith("/commit"),
    );

    await page.getByRole("button", { name: "Save", exact: true }).click();
    expect((await committed).ok()).toBeTruthy();
    await expect(page.locator(".ontology-edit-bar")).toHaveCount(0);
    await expect(status(moved)).toHaveText(nextStatus);
    await expect(rows.locator("td:last-child")).toHaveText(savedOrder);

    await status(moved).click();
    await moved.getByRole("combobox", { name: "Edit Status" }).selectOption({ label: firstStatus });
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
    await expect(status(moved)).toHaveText(firstStatus);
    page.once("dialog", (dialog) => dialog.accept());
    await page.getByRole("button", { name: "Discard", exact: true }).click();
    await expect(status(moved)).toHaveText(nextStatus);
  } finally {
    releaseReads();
    await page.unrouteAll({ behavior: "wait" });
  }

  await expect(status(moved)).toHaveText(nextStatus);
  await page.reload();
  await expect(status(moved)).toHaveText(nextStatus);
});

test("groups a table by a link field under each target's title", async ({ page }) => {
  await page.goto("/notes?view=e2e.action-items");
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  const groupBy = page.getByRole("combobox", { name: "Group by" });
  await groupBy.selectOption({ label: "Assigned to" });
  await expect(groupBy).toHaveValue("assignedTo");

  const headers = page.locator(".configured-view__group-toggle");
  await expect(headers.first()).toBeVisible();

  for (const header of await headers.all()) {
    await expect(header).not.toContainText("[[");
    // A person is not a status, so the group has no status marker.
    await expect(header.locator(".status-mark")).toHaveCount(0);
  }
});
