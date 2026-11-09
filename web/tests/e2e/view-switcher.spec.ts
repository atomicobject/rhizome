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
