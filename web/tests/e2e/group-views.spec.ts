import { readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, test, type APIRequestContext, type FrameLocator, type Page } from "./fixtures";
import { chooseView, expectView, workspaceView } from "./workspaceView";

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

const customFrame = (page: Page) => page.frameLocator("iframe.custom-view-frame");

const viewChoice = (page: Page, name: string) =>
  workspaceView(page).getByRole("button", { name, exact: true });

async function openGroup(page: Page, group: string) {
  await page
    .getByRole("region", { name: "Collections" })
    .getByRole("button", { name: group, exact: true })
    .click();
  await expect(page.getByRole("heading", { name: group, exact: true })).toBeVisible();
}

async function vaultFile(request: APIRequestContext, ...parts: string[]) {
  const status = await (await request.get("/api/v1/status")).json();

  return path.join(status.vaultPath, ...parts);
}

// Saves the way editors do, by renaming a temporary file over the target. On
// macOS, rewriting a file in place right after the watcher reported it does not
// always trigger a reindex, so the journey would wait out its timeout; a
// rename always does.
async function save(file: string, text: string) {
  await writeFile(`${file}.tmp`, text);
  await rename(`${file}.tmp`, file);
}

// A marker on the frame's window survives re-renders and is lost on reload.
const markFrame = (frame: FrameLocator) =>
  frame.locator("html").evaluate(() => Object.assign(window, { gvMarker: true }));

const frameMarked = (frame: FrameLocator) =>
  frame.locator("html").evaluate(() => "gvMarker" in window);

test("a group opens the bundled view its shape calls for and remembers a choice", async ({
  page,
}) => {
  await page.goto("/notes");

  await openGroup(page, "E2E Planning");
  await expectView(page, "Briefing");
  await expect(page.locator("iframe.custom-view-frame")).toBeVisible();

  for (const name of ["Overview", "Briefing", "Trace", "Sections"]) {
    await expect(viewChoice(page, name)).toHaveCount(1);
  }

  await openGroup(page, "E2E Library");
  await expectView(page, "Briefing");
  await expect(viewChoice(page, "Trace")).toHaveCount(0);

  await openGroup(page, "E2E Contacts");
  await expectView(page, "Sections");
  await expect(viewChoice(page, "Trace")).toHaveCount(0);
  await expect(
    customFrame(page)
      .getByRole("region", { name: "Contacts" })
      .getByRole("button", { name: "Ada Example" }),
  ).toBeVisible();

  await chooseView(page, "Overview");
  await expect(page.locator("iframe.custom-view-frame")).toHaveCount(0);
  await openGroup(page, "E2E Library");
  await expectView(page, "Briefing");
  await openGroup(page, "E2E Contacts");
  await expectView(page, "Overview");
  await page.reload();
  await expectView(page, "Overview");

  await chooseView(page, "Sections");
  await expect(page.locator("iframe.custom-view-frame")).toBeVisible();
  await page.reload();
  await expectView(page, "Sections");
});

test("Sections renders member tables from metadata and navigates to records, collections, and issues", async ({
  page,
}) => {
  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await chooseView(page, "Sections");
  const frame = customFrame(page);

  const work = frame.getByRole("region", { name: "Work items" });
  await expect(work.getByRole("columnheader")).toHaveText(["Title", "Type", "Stage", "Changed"]);

  const indexing = work
    .getByRole("row")
    .filter({ has: frame.getByRole("button", { name: "Faster indexing" }) });

  await expect(indexing.getByRole("cell")).toContainText(["Faster indexing", "Story", "Doing"]);

  const crash = work
    .getByRole("row")
    .filter({ has: frame.getByRole("button", { name: "Crash on empty vault" }) });

  await expect(crash.getByRole("cell")).toContainText(["Crash on empty vault", "Bug", "Blocked"]);
  // Most advanced first: blocked, doing, backlog, then done (terminal) last.
  await expect(work.locator("tbody tr").getByRole("button", { name: /^[A-Z]/ })).toHaveText([
    "Crash on empty vault",
    "Faster indexing",
    "Pinned filters",
    "Invoice export",
  ]);

  // The hierarchical Areas type renders as a tree: Search nests under Platform.
  const areas = frame.getByRole("region", { name: "Areas" });
  await expect(areas.locator("tbody tr")).toHaveCount(3);
  const search = areas.locator("tbody tr").filter({ hasText: "under Platform" });
  await expect(search.getByRole("button", { name: "Search", exact: true })).toBeVisible();
  await expect(search).toHaveAttribute("data-depth", "1");
  await expect(areas.locator("tbody tr").nth(2)).toContainText("Search");
  await expect(areas.locator("tbody tr").nth(1)).toContainText("Platform");

  // A record title opens the record in a tab.
  await indexing.getByRole("button", { name: "Faster indexing" }).click();
  await expect(page).toHaveURL(/note=notes%2Fgroup-views%2Fwork%2Fstory-indexing\.md/);
  await expect(
    page
      .getByRole("tablist", { name: "Open notes" })
      .getByRole("tab", { name: /Faster indexing|story-indexing/ }),
  ).toHaveCount(1);

  // A type header opens the collection.
  await page.goBack();
  await expect(work.getByRole("button", { name: "Work items", exact: true })).toBeVisible();
  await work.getByRole("button", { name: "Work items", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Work items", exact: true })).toBeVisible();

  // Issue counts open the issues panel scoped to the member or the record.
  await page.goBack();
  await work.locator("header").getByRole("button", { name: "1 issue" }).click();
  const problems = page.getByRole("region", { name: "Problems" });
  await expect(problems).toContainText("interface · GvWork");
  await expect(problems.locator("[data-issue-key]")).toHaveCount(1);
  await expect(problems.locator("[data-issue-key]")).toContainText("severity");
  await page.goBack();
  await crash.getByRole("button", { name: "1 issue" }).click();
  await expect(problems).toContainText("notes/group-views/work/bug-crash.md");
  await expect(problems.locator("[data-issue-key]")).toHaveCount(1);
  await expect(problems.locator("[data-issue-key]")).toContainText("severity");
});

test("Briefing summarizes the group from metadata and navigates to records, collections, and issues", async ({
  page,
}) => {
  await page.clock.setFixedTime(new Date("2026-10-05T23:59:00-04:00"));
  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await expectView(page, "Briefing");
  const frame = customFrame(page);

  const signals = frame.getByRole("region", { name: "Needs attention" }).getByRole("listitem");
  const signal = (text: string) => signals.filter({ hasText: text });
  await expect(signal("Validation issues on 1 record")).toContainText("Crash on empty vault");
  await expect(signal("Strained health: 1 area")).toContainText("Billing");
  await expect(signal("Blocked stage: 1 work item")).toContainText("Crash on empty vault");
  const context = frame.getByRole("region", { name: "Context", exact: true });
  await expect(
    context.getByRole("listitem").filter({ hasText: "Lead empty on 2 of 3 areas" }),
  ).toContainText("Billing, Search");
  await expect(signal("Lead empty")).toHaveCount(0);

  const motion = frame.getByRole("region", { name: "In motion" });
  await expect(
    motion.getByRole("heading", { name: "Work items · Doing, Blocked 2" }),
  ).toBeVisible();
  await expect(motion.getByRole("listitem")).toHaveCount(2);
  // The fixture's records share one change time, so their order is not fixed.
  await expect(motion.getByRole("listitem").filter({ hasText: "Faster indexing" })).toHaveCount(1);
  await expect(
    motion.getByRole("listitem").filter({ hasText: "Crash on empty vault" }),
  ).toHaveCount(1);

  const recent = frame.getByRole("region", { name: "Recent changes" });
  await expect(recent.getByRole("heading", { name: "Today" })).toBeVisible();
  await recent
    .getByRole("button", { name: /^Expand the \d+ records changed at/ })
    .first()
    .click();
  await expect(recent.getByRole("button", { name: "Autumn release" })).toBeVisible();

  // Ada is the typed owner of one work item and watches another from outside;
  // Lin is named only in a body link.
  const outside = frame.getByRole("region", { name: "Outside the group" });
  await expect(outside.getByRole("heading")).toHaveText(/^Outside the group\s*2 notes$/);
  await expect(outside.getByRole("listitem")).toHaveText([
    /^Ada Example contact\s*2\s*linked records$/,
    /^Lin Example contact\s*1\s*linked record$/,
  ]);

  const members = frame.getByRole("region", { name: "In this group" }).getByRole("listitem");
  const work = members.filter({ has: frame.getByRole("button", { name: "Work items" }) });
  await expect(work).toContainText(/Backlog\s*1\s*Doing\s*1\s*Blocked\s*1\s*Done\s*1/);
  await expect(work).toContainText(/Story\s*3\s*Bug\s*1/);

  const matrix = frame.getByRole("region", { name: "Connections" }).getByRole("table");
  await expect(matrix.getByRole("columnheader")).toHaveText(["Areas", "Releases", "Work items"]);

  const matrixRow = (name: string) =>
    matrix.getByRole("row").filter({ has: frame.getByRole("rowheader", { name }) });

  await expect(matrixRow("Work items").getByRole("cell")).toHaveText([
    "4",
    "No link field",
    "Same type",
  ]);
  await expect(matrixRow("Releases").getByRole("cell")).toHaveText([
    "No link field",
    "Same type",
    "1",
  ]);

  // A record opens in a tab.
  await motion.getByRole("button", { name: "Faster indexing" }).click();
  await expect(page).toHaveURL(/note=notes%2Fgroup-views%2Fwork%2Fstory-indexing\.md/);
  await expect(
    page.getByRole("tablist", { name: "Open notes" }).getByRole("tab", { name: /Faster indexing/ }),
  ).toHaveCount(1);

  // A member name opens its collection.
  await page.goBack();
  await members.getByRole("button", { name: "Areas", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Areas", exact: true })).toBeVisible();

  // The issue action opens the issues of the member that holds them.
  await page.goBack();
  await signal("Validation issues on 1 record")
    .getByRole("button", { name: "Open issues" })
    .click();
  const problems = page.getByRole("region", { name: "Problems" });
  await expect(problems).toContainText("interface · GvWork");
  await expect(problems.locator("[data-issue-key]")).toHaveCount(1);
  await expect(problems.locator("[data-issue-key]")).toContainText("bug-crash.md");
});

for (const { time, heading } of [
  { time: "2026-10-05T23:59:00-04:00", heading: "Today" },
  { time: "2026-10-06T00:01:00-04:00", heading: "Yesterday, Oct 5" },
]) {
  test(`Briefing groups recent changes as ${heading} across midnight`, async ({ page }) => {
    await page.clock.setFixedTime(new Date(time));
    await page.goto("/notes");
    await openGroup(page, "E2E Planning");
    await expectView(page, "Briefing");

    const recent = customFrame(page).getByRole("region", { name: "Recent changes" });
    await expect(recent.getByRole("heading")).toHaveText(["Recent changes", heading]);
    await expect(recent.locator("time")).toHaveAttribute("datetime", "2026-10-06T03:58:00.000Z");
    await expect(recent.locator("time")).toHaveText("23:58");
  });
}

test("Trace defaults to the area tree, nests and rolls up rows, and switches its rows", async ({
  page,
}) => {
  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await chooseView(page, "Trace");
  const frame = customFrame(page);
  const rowsControl = frame.getByRole("radiogroup", { name: "Rows" });
  const table = frame.getByRole("table");

  const row = (title: string) =>
    table.getByRole("row").filter({ has: frame.getByRole("rowheader", { name: title }) });

  // Areas is the hierarchical type that work items link to, so it is the default.
  await expect(rowsControl.getByRole("radio", { name: "Areas 3 (default)" })).toBeChecked();
  await expect(table.getByRole("columnheader")).toHaveText([/^Areas/, /^Work items/, /^Releases/]);

  // Search nests under Platform, whose cells roll up the subtree.
  const platform = row("Rows under Platform Platform");
  const search = row("Search , under Platform");
  await expect(search).toHaveAttribute("data-depth", "1");
  await expect(platform.getByRole("cell")).toHaveText([
    "BlockedCrash on empty vault3 in subtree",
    "1 in subtree",
  ]);
  const twisty = platform.getByRole("button", { name: "Rows under Platform" });
  await twisty.click();
  await expect(twisty).toHaveAttribute("aria-expanded", "false");
  await expect(search).toHaveCount(0);
  await expect(platform).toContainText("1 nested");
  await twisty.click();
  await expect(search).toBeVisible();

  // The release links to a work item, not to the area, so it is indirect.
  await expect(search.getByRole("button", { name: "Autumn release (indirect)" })).toBeVisible();
  await expect(search.getByRole("button", { name: /Faster indexing/ })).not.toContainText(
    "indirect",
  );

  // Rows switch to another member; the choice lasts for the session.
  await rowsControl.getByText(/^Work items/).click();
  await expect(rowsControl.getByRole("radio", { name: "Work items 4" })).toBeChecked();
  await expect(table.getByRole("columnheader")).toHaveText([
    /^Work items/,
    /^Areas/,
    /^Releases/,
    /^Owner.*in E2E Contacts/,
  ]);
  await expect(row("Doing Faster indexing").getByRole("cell")).toHaveText([
    "Search",
    "Autumn release",
    "Ada Example",
  ]);

  // Pointing at a record lights every occurrence of it.
  const searchChips = table.getByRole("button", { name: "Search", exact: true });
  await expect(searchChips).toHaveCount(2);
  await searchChips.first().hover();
  await expect(searchChips.nth(0)).toHaveAttribute("data-highlighted", "true");
  await expect(searchChips.nth(1)).toHaveAttribute("data-highlighted", "true");
  await expect(table.getByRole("button", { name: "Billing" })).not.toHaveAttribute(
    "data-highlighted",
  );

  await chooseView(page, "Sections");
  await chooseView(page, "Trace");
  await expect(rowsControl.getByRole("radio", { name: "Work items 4" })).toBeChecked();
  await rowsControl.getByText(/^Areas/).click();
  await expect(rowsControl.getByRole("radio", { name: "Areas 3 (default)" })).toBeChecked();
});

test("a hosted group view refreshes after a record changes without reloading the frame", async ({
  page,
  request,
}) => {
  test.setTimeout(60000);
  const notePath = await vaultFile(request, "notes", "group-views", "contacts", "lin.md");
  const original = await readFile(notePath, "utf8");
  await page.goto("/notes");
  await openGroup(page, "E2E Contacts");
  const frame = customFrame(page);
  const contacts = frame.getByRole("region", { name: "Contacts" });
  await expect(contacts.getByRole("button", { name: "Lin Example" })).toBeVisible();
  await markFrame(frame);

  try {
    await save(notePath, original.replace("Lin Example", "Lin Renamed"));
    await expect(contacts.getByRole("button", { name: "Lin Renamed" })).toBeVisible({
      timeout: 30000,
    });
    await expect(contacts.getByRole("button", { name: "Lin Example" })).toHaveCount(0);
    expect(await frameMarked(frame)).toBe(true);
  } finally {
    await save(notePath, original);
  }

  await expect(contacts.getByRole("button", { name: "Lin Example" })).toBeVisible({
    timeout: 30000,
  });
});

test("editing a repository view's folder reloads only the views from that folder", async ({
  page,
  context,
  request,
}) => {
  test.setTimeout(60000);
  const sourcePath = await vaultFile(request, ".rhizome", "views", "e2e-groups", "marker.tsx");
  const original = await readFile(sourcePath, "utf8");

  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await chooseView(page, "Planning notes");
  const edited = customFrame(page);
  await expect(edited.getByRole("heading", { name: "Planning notes", exact: true })).toBeVisible();
  await markFrame(edited);

  // Another repository view, from another folder, in a second window.
  const other = await context.newPage();
  await other.goto("/notes");
  const collections = other.getByRole("region", { name: "Collections" });
  await collections.getByRole("button", { name: "Expand E2E Views", exact: true }).click();
  await collections.getByRole("button", { name: "View demo efforts (2)", exact: true }).click();
  const untouched = customFrame(other);
  await expect(untouched.getByRole("heading", { name: "Demo dashboard" })).toBeVisible();
  await markFrame(untouched);

  try {
    await save(
      sourcePath,
      original.replace("<h1>Planning notes</h1>", "<h1>Planning notes, edited</h1>"),
    );
    await expect(
      edited.getByRole("heading", { name: "Planning notes, edited", exact: true }),
    ).toBeVisible({ timeout: 30000 });
    expect(await frameMarked(edited)).toBe(false);
    expect(await frameMarked(untouched)).toBe(true);
  } finally {
    await save(sourcePath, original);
  }

  await expect(edited.getByRole("heading", { name: "Planning notes", exact: true })).toBeVisible({
    timeout: 30000,
  });
});
