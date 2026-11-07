import { expect, test } from "@playwright/test";

// SPEC-0133 R5 and R7: the shell around AO KB records. Owned notes offer no way into
// editing, a custom record presentation shares the shell's left edge, and the sidebar's
// product model lists the kinds that hold records.

test("an AO-owned Markdown note offers no Edit and says why", async ({ page }) => {
  await page.goto("/notes?note=assertions%2Fclinic-patient-books-own-slot.md");
  await expect(page.getByText(/^Managed by AO KB\./)).toBeVisible();
  await expect(page.getByRole("tab", { name: /clinic-patient-books-own-slot/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await page.keyboard.press("ControlOrMeta+Shift+E");
  await expect(page.locator(".ontology-edit-bar")).toHaveCount(0);
});

test("a model record's node path starts on the presentation's left edge", async ({ page }) => {
  await page.goto("/notes/group/Knowledge?presentation=ao-kb.assertions&find=Patients+book");
  const frame = page.frameLocator("iframe");
  await frame.getByRole("link", { name: "Patients book their own slots", exact: true }).click();
  await frame
    .getByRole("region", { name: "Cited by", exact: true })
    .getByRole("listitem")
    .filter({ hasText: "Feature · FEAT-book" })
    .getByRole("link", { name: "Book an appointment", exact: true })
    .click();
  const heading = frame.getByRole("heading", { name: "Book an appointment", level: 1 });
  await expect(heading).toBeVisible();
  const panel = page.locator(".notes-shell__panel.is-active");
  const crumbs = panel.getByRole("navigation", { name: "Node path" });
  const controls = panel.locator(".ontology-pane__controls");
  await expect(crumbs).toContainText("Book an appointment");

  for (const width of [1280, 1000]) {
    await page.setViewportSize({ width, height: 900 });

    const [path, title, row, inset] = await Promise.all([
      crumbs.getByRole("button").first().boundingBox(),
      heading.boundingBox(),
      controls.boundingBox(),
      controls.evaluate((el) => parseFloat(getComputedStyle(el).paddingLeft)),
    ]);

    // The path, the control row's contents, and the record title share one left edge.
    expect(Math.abs(path!.x - (row!.x + inset))).toBeLessThanOrEqual(1);
    expect(Math.abs(title!.x - path!.x)).toBeLessThanOrEqual(1);
  }
});

test("the sidebar's product model lists only kinds that hold records", async ({ page }) => {
  await page.goto("/notes");
  const group = page.getByRole("button", { name: "Expand Product model" });

  await group.click();
  await page.getByRole("button", { name: "Expand Model records" }).click();

  const kinds = page
    .locator(".ontology-type-group--presentation")
    .filter({ has: page.getByRole("button", { name: "Collapse Product model" }) })
    .locator(".ontology-type-row--embedded .ontology-type-row__label");

  await expect(kinds).toHaveText([
    "Capability",
    "Component",
    "Feature",
    "Requirement",
    "Screen",
    "Use case",
  ]);
});

test("at 728px the note controls wrap and the knowledge tables stack instead of clipping", async ({
  page,
}) => {
  await page.setViewportSize({ width: 728, height: 900 });
  await page.goto("/notes?note=assertions%2Fclinic-patient-books-own-slot.md");
  const panel = page.locator(".notes-shell__panel.is-active");
  const row = panel.locator(".ontology-pane__controls");
  const copy = row.getByRole("button", { name: "Copy link", exact: true });

  await expect(copy).toBeVisible();

  for (const control of [copy, row.getByRole("button", { name: "Assertion", exact: true })]) {
    const [box, bounds] = await Promise.all([control.boundingBox(), row.boundingBox()]);

    expect(box!.x + box!.width).toBeLessThanOrEqual(bounds!.x + bounds!.width);
    expect(box!.y + box!.height).toBeLessThanOrEqual(bounds!.y + bounds!.height);
  }

  for (const presentation of ["ao-kb.assertions", "ao-kb.concepts"]) {
    await page.goto(`/notes/group/Knowledge?presentation=${presentation}`);
    const wrap = page.frameLocator("iframe").locator(".ak-table-wrap");

    await expect(wrap).toBeVisible();
    expect(await wrap.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  }
});
