import { expect, type Page, test } from "./fixtures";
import { chooseView, expandNoteList, workspaceView } from "./workspaceView";

// Synthetic Tv* types from tests/e2e/fixtures/type-views: one per SPEC-0112 shape.

const selectedView = (page: Page) => workspaceView(page).locator("[aria-pressed=true]");

const facets = (page: Page) => page.getByRole("group", { name: "Collection summary" });

const rows = (page: Page) => page.locator("tr.configured-view__row");

const footer = (page: Page) => page.locator(".configured-view__footer-status");

for (const reducedMotion of ["no-preference", "reduce"] as const) {
  test(`collection grouping and note hover respect motion preference: ${reducedMotion}`, async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion });
    await page.goto("/notes/tv-guide");
    const group = page.getByRole("button", { name: "Howto 2", exact: true });
    const caret = group.locator(".configured-view__group-caret");
    await expect(group).toHaveAttribute("aria-expanded", "true");
    await expandNoteList(page, "Field guide notes");

    const note = page
      .getByRole("region", { name: "Note list", exact: true })
      .locator(".ontology-note")
      .filter({ hasText: "Mooring knots" });

    await expect(note).toBeVisible();
    const transform = await caret.evaluate((element) => getComputedStyle(element).transform);
    const background = await note.evaluate((element) => getComputedStyle(element).backgroundColor);

    for (const target of [caret, note]) {
      const duration = await target.evaluate((element) => {
        element.addEventListener("transitionrun", (event) => {
          if (event instanceof TransitionEvent && event.target === element) {
            element.setAttribute("data-motion-transition", event.propertyName);
          }
        });

        return Math.max(
          ...getComputedStyle(element).transitionDuration.split(",").map(Number.parseFloat),
        );
      });

      if (reducedMotion === "reduce") {
        expect(duration).toBeLessThanOrEqual(0.001);
      } else {
        expect(duration).toBeGreaterThan(0.05);
      }
    }

    await expect(rows(page)).toHaveCount(5);
    await group.click();
    await expect(group).toHaveAttribute("aria-expanded", "false");
    await expect(rows(page)).toHaveCount(3);
    await expect
      .poll(() => caret.evaluate((element) => getComputedStyle(element).transform))
      .not.toBe(transform);
    await note.hover();
    await expect
      .poll(() => note.evaluate((element) => getComputedStyle(element).backgroundColor))
      .not.toBe(background);

    if (reducedMotion === "no-preference") {
      await expect(caret).toHaveAttribute("data-motion-transition", "transform");
      await expect(note).toHaveAttribute("data-motion-transition", "background-color");
    }
  });
}

test("each collection shape opens with its default layout", async ({ page }) => {
  // Workflow: open work spans several stages and stays small, so it opens as a Board.
  await page.goto("/notes/tv-work-item");
  await expect(selectedView(page)).toContainText("Board");
  await expect(facets(page)).toContainText("10 work items");
  // The view lists the records, so the rail's copy starts collapsed.
  await expect(page.getByRole("button", { name: "Work item notes" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );

  // Dated: a month-grouped log, newest first.
  await page.goto("/notes/tv-meeting");
  await expect(selectedView(page)).toContainText("Table");
  const months = page.locator(".configured-view__group-row");
  await expect(months).toHaveText([/Oct 2026\s*2/, /Sep 2026\s*3/, /Aug 2026\s*3/]);
  await expect(facets(page)).toContainText("Aug 2026 –");

  // Catalog: grouped by its category enum in declaration order.
  await page.goto("/notes/tv-guide");
  await expect(selectedView(page)).toContainText("Table");
  await expect(page.locator(".configured-view__group-row")).toHaveText([
    /Howto\s*2/,
    /Reference\s*2/,
    /Explanation\s*1/,
  ]);
});

test("header facets filter the table and show as filter chips", async ({ page }) => {
  await page.goto("/notes/tv-work-item");
  await chooseView(page, "Table");
  await expect(rows(page)).not.toHaveCount(0);

  const gap = facets(page).getByRole("button", { name: "Next step empty 5/10" });
  await expect(gap).toHaveAttribute("title", "Every open item names its next step.");
  await gap.click();
  // The counts now describe the filtered rows, so find the facet by its field.
  const applied = facets(page).getByRole("button", { name: /^Next step empty/ });
  await expect(applied).toHaveAttribute("aria-pressed", "true");
  // One of the five is Cancelled, a dropped stage whose group starts collapsed.
  await expect(footer(page)).toContainText("5 rows");
  await expect(rows(page)).toHaveCount(4);
  await expect(
    page.getByRole("button", { name: /Remove filter Next step is empty/ }),
  ).toBeVisible();

  await applied.click();
  await facets(page).getByRole("button", { name: "Doing 3" }).click();
  await expect(footer(page)).toContainText("3 rows");
  await expect(rows(page)).toHaveCount(3);
  await expect(
    page.getByRole("button", { name: /Remove filter Stage is any of Doing/ }),
  ).toBeVisible();
});

test("a selected row shows as a record in the right rail and opens in a tab", async ({ page }) => {
  await page.goto("/notes/tv-work-item");
  await chooseView(page, "Table");

  // A click on the title selects the row; it does not open the note.
  const row = rows(page).filter({ hasText: "Anchor chain inspection" });
  await row.getByRole("button", { name: "Open Anchor chain inspection" }).click();

  const record = page.getByRole("article", { name: "Selected record Anchor chain inspection" });
  await expect(record).toBeVisible();
  await expect(record).toContainText(
    "Inspect the anchor chain links before the storm season starts.",
  );
  await expect(record.getByRole("region", { name: "Review meetings" })).toContainText(
    "Harbor review, August 4",
  );

  await record.getByRole("button", { name: "Open Anchor chain inspection" }).click();
  await expect(
    page.getByRole("tablist", { name: "Open notes" }).getByRole("tab", { selected: true }),
  ).toContainText("Anchor chain inspection");
});

test("a bulk edit adds a link to every selected row, stages, and saves", async ({ page }) => {
  await page.goto("/notes/tv-meeting");
  await expect(rows(page)).toHaveCount(8);

  await page.getByRole("checkbox", { name: "Select Harbor review, September 1" }).check();
  await page.getByRole("checkbox", { name: "Select Lantern review, October 2" }).check();

  const bar = page.getByRole("toolbar", { name: "Edit selected rows" });
  await expect(bar).toContainText("2 selected");

  const attendees = bar.getByLabel("Add Attendees");
  await expect(attendees).toBeEnabled();
  const bob = await attendees.locator("option", { hasText: /bob/i }).getAttribute("value");
  await attendees.selectOption(bob ?? "");

  await expect(page.getByText("2 staged changes", { exact: true })).toBeVisible();

  const committed = page.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/edit-sessions/") && response.url().endsWith("/commit"),
  );

  await page.getByRole("button", { name: "Save", exact: true }).click();
  expect((await committed).ok()).toBeTruthy();
  await expect(page.locator(".ontology-edit-bar")).toHaveCount(0);

  for (const title of ["Harbor review, September 1", "Lantern review, October 2"]) {
    await expect(rows(page).filter({ hasText: title })).toContainText(/bob/i);
  }
});

test("the type Briefing reads a collection and opens its Table from a signal", async ({ page }) => {
  await page.goto("/notes/tv-work-item");
  await chooseView(page, "Briefing");
  const frame = page.frameLocator("iframe.custom-view-frame");

  const attention = frame.getByRole("region", { name: "Needs attention" });
  const context = frame.getByRole("region", { name: "Context", exact: true });
  const nextStep = context.getByRole("listitem").filter({ hasText: "Next step empty" });
  await expect(attention).not.toContainText("Next step empty");
  await expect(nextStep).toContainText("Next step empty on 5 of 10 work items");
  await expect(nextStep).toContainText("Every open item names its next step.");
  // Eight of ten work items have review meetings.
  await expect(context).toContainText("No review meetings link to 2 of 10 work items");

  // Doing and In review are the active stages.
  await expect(frame.getByRole("region", { name: "In motion" }).locator(".gv-record")).toHaveCount(
    5,
  );
  await expect(
    frame.getByRole("region", { name: "Shape" }).getByRole("heading", { name: "Stage lifecycle" }),
  ).toBeVisible();

  const connections = frame.getByRole("region", { name: "Connections" });
  await expect(connections.getByRole("listitem").filter({ hasText: /^Project/ })).toContainText(
    "9/10",
  );
  await expect(connections.getByRole("listitem").filter({ hasText: /^Meetings/ })).toContainText(
    "8/10",
  );
  await expect(frame.getByRole("region", { name: "Linked from outside" })).toContainText(
    "review meeting",
  );

  await nextStep.getByRole("button", { name: "Open table" }).click();
  await expect(selectedView(page)).toHaveText("Table");

  // A dated type charts its primary date by month.
  await page.goto("/notes/tv-meeting");
  await chooseView(page, "Briefing");
  await expect(frame.getByRole("img", { name: /^Date per month, Aug 2026 to / })).toBeVisible();
  await expect(frame.getByRole("region", { name: "In motion" })).toHaveCount(0);
});
