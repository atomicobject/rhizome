import { expect, test } from "@playwright/test";

import { chooseView } from "../e2e/workspaceView";

// SPEC-0133 R5 and R7: the shell around AO KB records. KB notes are edited in place
// from their custom presentation, a custom record presentation shares the shell's left
// edge, and the sidebar's product model lists the kinds that hold records.

// The fixture is an installation from before editing was enabled; the server's refresh
// lifted the starter's former read-only declaration, so nothing refuses the edit.
test("an AO KB note opened in its presentation is edited through the Markdown editor and keeps the edit", async ({
  page,
}) => {
  const fileURL = "/api/v1/files/view?path=assertions%2Fclinic-patient-books-own-slot.md";
  const before = await (await page.request.get(fileURL)).json();

  const updated = before.content.replace(
    "\n## Connections\n",
    "\nEdited through Rhizome.\n\n## Connections\n",
  );

  expect(updated).not.toBe(before.content);
  await page.goto(
    "/notes?note=assertions%2Fclinic-patient-books-own-slot.md&presentation=ao-kb.assertion",
  );
  const frame = page.frameLocator("iframe:not(.panel-frame)");
  await expect(
    frame.getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await expect(page.getByText(/^Managed by AO KB\./)).toHaveCount(0);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page.locator(".ontology-edit-bar")).toBeVisible();
  await chooseView(page, "Markdown");
  const editor = page.getByRole("textbox", { name: "Markdown source" });
  await expect(editor).toBeVisible();

  await editor.click();
  await editor.press("ControlOrMeta+a");
  await page.keyboard.insertText(updated);
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();

  const committed = page.waitForResponse(
    (response) => response.url().endsWith("/commit") && response.request().method() === "POST",
  );

  await page.getByRole("button", { name: "Save", exact: true }).click();
  expect(await (await committed).json()).toMatchObject({
    outcome: "committed",
    hasUncommittedChanges: false,
  });
  await expect(page.locator(".ontology-edit-bar")).toHaveCount(0);
  await expect
    .poll(async () => (await (await page.request.get(fileURL)).json()).content)
    .toBe(updated);

  await page.reload();
  await expect(page.getByRole("button", { name: "Assertion", exact: true })).toBeVisible();
  await chooseView(page, "Structure");
  await expect(page.getByText("Edited through Rhizome.", { exact: true })).toBeVisible();
  await chooseView(page, "Assertion");
  await expect(
    frame.getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await expect(frame.getByText("Edited through Rhizome.", { exact: true })).toBeVisible();
});

test("a model record's node path starts on the presentation's left edge", async ({ page }) => {
  await page.goto("/notes/group/Knowledge?presentation=ao-kb.assertions&find=Patients+book");
  const frame = page.frameLocator("iframe:not(.panel-frame)");
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
    const wrap = page.frameLocator("iframe:not(.panel-frame)").locator(".ak-table-wrap");

    await expect(wrap).toBeVisible();
    expect(await wrap.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  }
});
