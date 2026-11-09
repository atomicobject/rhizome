import { expect, test, type Page } from "@playwright/test";

// The Knowledge panel (SPEC-0135 US7, R6) in the Notes sidebar, on the legacy fixture
// (testdata/ao-kb-legacy) served by playwright.ao-kb.config.ts's second server.
//
// The cases marked fixme need the sidebar to host custom panel frames (SPEC-0136 R6, the
// platform's P7b); until then NotesLeftRail renders only the built-in Folder panel. Remove
// the fixme marks when P7b is integrated.

const record = (notePath: string) => `/notes?${new URLSearchParams({ note: notePath })}`;

const group = (presentation?: string) =>
  `/notes/group/Knowledge${presentation ? `?presentation=ao-kb.${presentation}` : ""}`;

const rail = (page: Page) => page.getByRole("complementary", { name: "Notes navigation" });

const knowledgePanel = (page: Page) => rail(page).getByRole("region", { name: "Knowledge panel" });

const panel = (page: Page) => knowledgePanel(page).frameLocator("iframe");

const recent = (page: Page) => panel(page).getByRole("list", { name: "Recent" });

// Tags the panel frame's document, so a later check can tell the same frame from a new one.
const tagFrame = (page: Page) =>
  knowledgePanel(page)
    .locator("iframe")
    .evaluate((frame: HTMLIFrameElement) =>
      frame.contentDocument?.documentElement.setAttribute("data-e2e-tag", "kept"),
    );

const frameTag = (page: Page) =>
  knowledgePanel(page)
    .locator("iframe")
    .evaluate((frame: HTMLIFrameElement) =>
      frame.contentDocument?.documentElement.getAttribute("data-e2e-tag"),
    );

async function openRecord(page: Page, notePath: string, title: string) {
  await page.goto(record(notePath));
  await expect(
    page.frameLocator("main iframe").first().getByRole("heading", { name: title, level: 1 }),
  ).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
});

test("the server gives Knowledge records and the group the Knowledge panel, and an untyped note Folder", async ({
  page,
}) => {
  const catalog = await (await page.request.get("/api/v1/views")).json();

  const panelOf = (kind: string, name: string) =>
    catalog.targets.find((t: { kind: string; name: string }) => t.kind === kind && t.name === name)
      ?.panel;

  for (const [kind, name] of [
    ["group", "Knowledge"],
    ["node", "Concept"],
    ["node", "Assertion"],
    ["node", "Source"],
    ["node", "Artifact"],
  ])
    expect(panelOf(kind, name)).toMatchObject({ kind: "view", viewId: "ao-kb.panel" });
  expect(catalog.issues ?? []).toEqual([]);
});

test("an untyped note outside the experience shows Folder, not the Knowledge panel", async ({
  page,
}) => {
  await page.goto(record("notes/untyped.md"));
  await expect(rail(page).getByRole("region", { name: "Folder panel" })).toBeVisible();
  await expect(knowledgePanel(page)).toHaveCount(0);
});

test.fixme("inside the experience: title, site links, counted lists, and the collapsed locator", async ({
  page,
}) => {
  await page.goto(group());
  const p = panel(page);
  await expect(p.getByRole("heading", { name: "Harbor Ferry", level: 1 })).toBeVisible();
  await expect(
    p.getByText("The locator shows where a record sits on the topic map."),
  ).toBeVisible();

  await p.getByText("Concepts", { exact: true }).click();
  await expect(p.getByRole("link", { name: /^Timetable\s*31$/ })).toBeVisible();
  await p.getByText("Source folders", { exact: true }).click();
  await p.getByRole("link", { name: "sources/documents, 3 sources" }).click();
  await expect(page).toHaveURL(/presentation=ao-kb\.sources.*recordFolder=sources%2Fdocuments/);
  await p.getByText("Assertion kinds", { exact: true }).click();
  await p.getByRole("link", { name: /^rule\s*21$/ }).click();
  await expect(page).toHaveURL(/presentation=ao-kb\.assertions.*assertionKind=rule/);

  await p
    .getByRole("navigation", { name: "Knowledge pages" })
    .getByRole("link", { name: "Review" })
    .click();
  await expect(page).toHaveURL(/presentation=ao-kb\.review/);
});

test.fixme("Find by title matches aliases, Enter opens the first match, and Search all text searches the vault", async ({
  page,
}) => {
  await page.goto(group());
  const find = panel(page).getByRole("searchbox", { name: "Find by title" });
  await find.fill("step-free");
  await expect(panel(page).getByRole("list", { name: "Title matches" })).toContainText(
    "Accessibility",
  );
  await find.press("Enter");
  await expect(page).toHaveURL(/note=concepts%2Faccessibility\.md/);
  // The panel stayed mounted, so its find text is still there.
  await expect(find).toHaveValue("step-free");

  await find.fill("gangway");
  await panel(page)
    .getByRole("button", { name: /^Search all text/ })
    .click();
  await expect(page).toHaveURL(/search=gangway/);
  await expect(page).not.toHaveURL(/folder=/);
});

test.fixme("on a record page the locator highlights its scope and lists its concepts", async ({
  page,
}) => {
  await openRecord(page, "concepts/timetable.md", "Timetable");
  const p = panel(page);
  await expect(
    p.getByRole("img", { name: /^Topic map with \d+ of 4 concepts highlighted$/ }),
  ).toBeVisible();
  const scope = p.getByRole("list", { name: "Concepts in this record's scope" });
  await expect(scope.getByRole("link", { name: "Tickets" })).toBeVisible();
  await tagFrame(page);
  await scope.getByRole("link", { name: "Tickets" }).click();
  await expect(page).toHaveURL(/note=concepts%2Ftickets\.md/);
  // A new subject rebinds the same frame; the component is not remounted.
  expect(await frameTag(page)).toBe("kept");
});

test.fixme("Recent survives the panel frame being evicted and recreated", async ({ page }) => {
  await openRecord(page, "concepts/timetable.md", "Timetable");
  await openRecord(page, "concepts/tickets.md", "Tickets");
  await expect(recent(page).getByRole("link")).toHaveText(["Tickets", "Timetable"]);
  await panel(page).getByRole("searchbox", { name: "Find by title" }).fill("draft");
  await tagFrame(page);

  // Leave the experience for an untyped note, and reload: every frame is created anew.
  await page.goto(record("notes/untyped.md"));
  await expect(rail(page).getByRole("region", { name: "Folder panel" })).toBeVisible();
  await page.reload();
  await openRecord(page, "concepts/accessibility.md", "Accessibility");

  expect(await frameTag(page)).toBeNull();
  await expect(recent(page).getByRole("link")).toHaveText([
    "Accessibility",
    "Tickets",
    "Timetable",
  ]);
  await expect(recent(page).getByRole("link", { name: "Accessibility" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  // Find text starts fresh in a new frame.
  await expect(panel(page).getByRole("searchbox", { name: "Find by title" })).toHaveValue("");
});

test.fixme("two windows keep separate Recent lists", async ({ page, context }) => {
  await openRecord(page, "concepts/timetable.md", "Timetable");
  const other = await context.newPage();
  await other.setViewportSize({ width: 1440, height: 900 });
  await openRecord(other, "concepts/tickets.md", "Tickets");

  await expect(recent(other).getByRole("link")).toHaveText(["Tickets"]);
  await page.bringToFront();
  await openRecord(page, "concepts/weather.md", "Weather");
  await expect(recent(page).getByRole("link")).toHaveText(["Weather", "Timetable"]);
  await other.close();
});
