import { expect, test, type Page } from "./fixtures";
import { expectView, workspaceView } from "./workspaceView";

// The View demo efforts collection (fixtures/unified-views) has five custom
// choices and three standard ones; its standard Briefing renders as custom.
const customChoices = [
  "Demo dashboard",
  "Diagram demo",
  "Demo native · Table",
  "Demo native · Cards",
  "Demo native · Board",
];

const standardChoices = ["Briefing", "Table", "Cards"];

async function openEfforts(page: Page) {
  await page.goto("/notes");
  const collections = page.getByRole("region", { name: "Collections" });
  const expand = collections.getByRole("button", { name: "Expand E2E Views", exact: true });

  await expect(collections.getByRole("button", { name: "E2E Views", exact: true })).toBeVisible();

  if (await expand.count()) await expand.click();
  await collections.getByRole("button", { name: "View demo efforts (2)", exact: true }).click();
  await expectView(page, "Demo dashboard");
}

const labels = (page: Page) => workspaceView(page).getByRole("button").allInnerTexts();

const screenshot = (page: Page, name: string) =>
  process.env.RHIZOME_FEEDBACK_DIR
    ? page.screenshot({ path: `${process.env.RHIZOME_FEEDBACK_DIR}/view-switcher-${name}.png` })
    : undefined;

test("the switcher shows custom views first and moves what does not fit into More", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  await openEfforts(page);
  await expect.poll(() => labels(page)).toEqual([...customChoices, ...standardChoices]);
  await screenshot(page, "wide");

  await page.setViewportSize({ width: 900, height: 900 });

  const more = workspaceView(page).getByRole("button", { name: "More", exact: true });

  await expect(more).toBeVisible();

  const shown = await labels(page);

  expect(shown.at(-1)).toBe("More");
  expect(shown.slice(0, -1)).toEqual(customChoices.slice(0, shown.length - 1));

  // Inline choices and the trigger stay inside the tab row.
  const row = await page.locator(".view-header__tabs").boundingBox();
  const trigger = await more.boundingBox();

  expect(trigger!.x + trigger!.width).toBeLessThanOrEqual(row!.x + row!.width);
  await screenshot(page, "narrow");

  await more.click();
  await expect(page.getByRole("menuitemradio")).toHaveText(
    [...customChoices, ...standardChoices].slice(shown.length - 1),
  );
  await page.getByRole("menuitemradio", { name: "Table", exact: true }).click();
  await expect(page.locator(".configured-view__table tbody tr[data-row-key]")).toHaveCount(2);

  // The selected view stays visible: named on the trigger.
  await expectView(page, "Table");
  await expect(workspaceView(page).locator("[aria-haspopup=menu]")).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await screenshot(page, "narrow-selected");

  await page.setViewportSize({ width: 1600, height: 900 });
  await expect(workspaceView(page).getByRole("button", { name: "More" })).toHaveCount(0);
  await expect(
    workspaceView(page).getByRole("button", { name: "Table", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
});

// The Other view demo note (fixtures/unified-views) has three custom node views.
const noteChoices = [
  "Evidence map",
  "Review checklist",
  "Delivery timeline",
  "Structure",
  "Markdown",
];

test("the note bar keeps one line and moves the views that do not fit into More", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1600, height: 900 });
  await page.goto(`/notes?note=${encodeURIComponent("notes/view-demo/other.md")}`);

  const bar = page.locator(".ontology-pane__controls:visible");

  await expect.poll(() => labels(page)).toEqual(noteChoices);
  await screenshot(page, "note-wide");

  await page.setViewportSize({ width: 860, height: 900 });

  const more = workspaceView(page).locator("[aria-haspopup=menu]");

  await expect(more).toBeVisible();

  const shown = await labels(page);

  expect(shown.slice(0, -1)).toEqual(noteChoices.slice(0, shown.length - 1));

  // One line: the switcher and Copy link share a row inside the bar.
  const row = (await bar.boundingBox())!;
  const switcher = (await workspaceView(page).boundingBox())!;
  const copy = (await bar.getByRole("button", { name: "Copy link" }).boundingBox())!;

  expect(row.height).toBeLessThan(48);
  expect(Math.abs(switcher.y + switcher.height / 2 - (copy.y + copy.height / 2))).toBeLessThan(2);
  expect(copy.x + copy.width).toBeLessThanOrEqual(row.x + row.width);
  await screenshot(page, "note-narrow");

  await more.click();
  // Structure is the note's default, so its menu item also says "Default".
  await expect(page.getByRole("menuitemradio")).toHaveText(
    noteChoices.slice(shown.length - 1).map((name) => new RegExp(`^${name}`)),
  );
  await page.getByRole("menuitemradio", { name: "Markdown", exact: true }).click();
  await expectView(page, "Markdown");
  await screenshot(page, "note-narrow-selected");
});
