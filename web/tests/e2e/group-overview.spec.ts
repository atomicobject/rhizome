import { expect, test } from "./fixtures";
import { customFrame, feedbackScreenshot, openGroup } from "./groupViewHelpers";
import { chooseView, expectView } from "./workspaceView";

test.use({ timezoneId: "America/Detroit", locale: "en-US" });

// Synthetic groups from fixtures/group-views:
// - E2E Planning: the Work items interface over Story and Bug, the
//   hierarchical Areas type that work items link to, and Releases that link to
//   work items, plus links out to E2E Contacts. A contact links back to a work
//   item, and a story names a contact in its body. One bug misses a required
//   field, so the group has an issue; Billing holds a risk-toned health value;
//   two areas leave the KEY lead field empty. The work stage enum declares
//   stages, and its warning-toned Blocked value is in the active stage.
// - E2E Contacts: one root.
// - E2E Library: two roots with no links.

test("group Overview maps member types, lists members and outside notes, and switches to the matrix", async ({
  page,
}) => {
  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await chooseView(page, "Overview");
  const frame = customFrame(page);

  const map = frame.getByRole("region", { name: "Map" });
  await expect(map.getByRole("group", { name: "E2E Planning map" })).toBeVisible();

  for (const name of ["Areas", "Releases", "Work items"])
    await expect(map.getByRole("button", { name: new RegExp(`^${name}\\b`) })).toHaveCount(1);

  const arcs = map.locator(".gv-map-arc");
  expect(await arcs.count()).toBeGreaterThan(0);

  for (const arc of await arcs.all()) {
    expect(
      Number.parseFloat(await arc.evaluate((el) => getComputedStyle(el).strokeWidth)),
    ).toBeGreaterThanOrEqual(4);
  }

  await expect(map.locator(".gv-legend-ring")).not.toContainText(["ring:"]);
  await feedbackScreenshot(page, "map-light");
  await page.emulateMedia({ colorScheme: "dark" });
  await feedbackScreenshot(page, "map-dark");
  await page.emulateMedia({ colorScheme: "light" });

  const members = frame.getByRole("region", { name: "Members" });
  await expect(
    members.getByRole("columnheader", { name: "Records linked", exact: true }),
  ).toBeVisible();
  await expect(members.getByRole("columnheader", { name: "Links", exact: true })).toBeVisible();

  for (const edge of await frame.locator(".gv-map-edge").all()) {
    await expect(edge.locator("line")).toHaveCount(1);
    await expect(edge.locator(".gv-map-hit")).toHaveCSS("stroke", "rgba(0, 0, 0, 0)");
  }

  for (const name of ["Areas", "Releases", "Work items"])
    await expect(members.getByRole("button", { name, exact: true })).toBeVisible();

  const layout = await members.evaluate((section) => {
    const container = section.closest(".gv-ov--group")!;
    const map = container.querySelector(".gv-ov-main")!;

    return {
      width: section.getBoundingClientRect().width,
      containerWidth: container.getBoundingClientRect().width,
      gap: section.getBoundingClientRect().top - map.getBoundingClientRect().bottom,
      rows: [...section.querySelectorAll("tbody tr")].map(
        (row) => row.getBoundingClientRect().height,
      ),
    };
  });

  expect(layout.width).toBe(layout.containerWidth);
  expect(layout.gap).toBeLessThanOrEqual(40);

  for (const height of layout.rows) expect(height).toBeLessThanOrEqual(36);

  // Ada is the typed owner of one work item and watches another from outside;
  // Lin is named only in a body link.
  const outside = frame.getByRole("region", { name: "Outside the group" });
  const more = outside.getByRole("button", { name: /Show \d+ (?:more|notes?)/ });

  if (await more.isVisible()) await more.click();
  await expect(outside).toContainText("Ada Example");
  await expect(outside).toContainText("Lin Example");
  await expect(outside.locator(".gv-changes")).toHaveCSS("max-height", "none");

  await map.getByRole("radio", { name: "Matrix" }).locator("xpath=..").click();

  const matrix = map.getByRole("table");
  await expect(matrix.getByRole("rowheader", { name: "Work items" })).toBeVisible();

  // A member name opens its collection.
  await members.getByRole("button", { name: "Areas", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Areas", exact: true })).toBeVisible();
});

test("All notes opens on its Briefing and offers the Overview", async ({ page }) => {
  await page.goto("/notes");
  await expectView(page, "Briefing");
  const frame = customFrame(page);

  for (const name of ["Needs attention", "In motion", "Recent changes"])
    await expect(frame.getByRole("region", { name })).toBeVisible();

  await expect(page.locator("canvas")).toHaveCount(0);

  await chooseView(page, "Overview");
  await expect(frame.getByRole("region", { name: "Map" })).toBeVisible();
  await expect(frame.getByRole("region", { name: "Untyped notes by folder" })).toBeVisible();
  await expect(frame.getByRole("region", { name: "Members" })).toBeVisible();
});

for (const { time, heading } of [
  { time: "2026-10-05T23:59:00-04:00", heading: "1m" },
  { time: "2026-10-06T00:01:00-04:00", heading: "3m" },
]) {
  test(`Briefing dates a recent change by its age (${heading}) across midnight`, async ({
    page,
  }) => {
    await page.clock.setFixedTime(new Date(time));
    await page.goto("/notes");
    await openGroup(page, "E2E Planning");
    await expectView(page, "Briefing");

    const recent = customFrame(page).getByRole("region", { name: "Recent changes" });
    // One list on every scope: no day headings, the age in its own column.
    await expect(recent.getByRole("heading")).toHaveText(["Recent changes"]);
    // The heading also matches the outgoing All notes frame; wait for the group's one change.
    await expect(recent.locator("time")).toHaveCount(1);
    await expect(recent.locator("time")).toHaveAttribute("datetime", "2026-10-06T03:58:00.000Z");
    await expect(recent.locator("time span[aria-hidden]")).toHaveText(heading);
  });
}
