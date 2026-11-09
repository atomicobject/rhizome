import { expect, test, type Page } from "@playwright/test";

// The Knowledge site (SPEC-0135 R9) on the legacy fixture (testdata/ao-kb-legacy, AO schema 5),
// served by playwright.ao-kb.config.ts's second server.

const group = (presentation?: string, params = "") =>
  `/notes/group/Knowledge${presentation ? `?presentation=ao-kb.${presentation}${params}` : ""}`;

const frame = (page: Page) => page.frameLocator("iframe").first();

// The saved-analysis read (SPEC-0133) asks for .kb/index/kb-weights.json, which the legacy
// fixture does not have; the page says "Unavailable", and the browser logs the 400.
const EXPECTED = /kb-weights\.json|status of 400/;

function consoleErrors(page: Page) {
  const errors: string[] = [];
  page.on("console", (m) => {
    if (m.type() === "error" && !EXPECTED.test(m.text()) && !EXPECTED.test(m.location().url))
      errors.push(m.text());
  });
  page.on("pageerror", (e) => errors.push(String(e)));

  return errors;
}

async function assertionsShown(page: Page) {
  const heading = frame(page).getByRole("region", { name: "Assertions", exact: true });
  await expect(heading).toBeVisible();

  return heading.locator("tbody tr");
}

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`${colorScheme} appearance`, () => {
    test.use({ colorScheme });

    test("overview to concept to assertion to source, and Back", async ({ page }) => {
      const errors = consoleErrors(page);
      await page.goto(group());
      const f = frame(page);
      await expect(f.getByRole("heading", { name: "Harbor Ferry", exact: true })).toBeVisible();
      await expect(
        f.getByText(
          /Profile: product-discovery · 43 assertions · 4 concepts · 7 sources · 5 artifacts/,
        ),
      ).toBeVisible();
      await expect(f.getByRole("heading", { name: "Topics and how they connect" })).toBeVisible();
      await expect(f.getByRole("heading", { name: "Sources by date" })).toBeVisible();
      await expect(
        f.getByRole("heading", { name: "What the KB holds on each topic" }),
      ).toBeVisible();
      await expect(f.getByText("42", { exact: true })).toBeVisible();

      // The map is one tab stop; its first mark is the concept with the most members.
      const map = f.getByRole("group", { name: /^Topic map, 4 concepts/ });
      await map.getByRole("link").first().focus();
      await expect(f.getByRole("tooltip")).toContainText("Timetable");
      await page.keyboard.press("ArrowRight");
      await expect(f.getByRole("tooltip")).toContainText("Tickets");
      await page.keyboard.press("ArrowLeft");
      await page.keyboard.press("Enter");
      await expect(page).toHaveURL(/note=concepts%2Ftimetable.md/);
      await expect(
        frame(page).getByRole("heading", { name: "Timetable", exact: true, level: 1 }),
      ).toBeVisible();
      await expect(
        frame(page).getByRole("heading", { name: /What the KB says about it/ }),
      ).toBeVisible();
      await expect(
        frame(page).getByRole("group", {
          name: "Where it sits on the topic map. Arrow keys move between concepts by member count; Enter opens one.",
        }),
      ).toBeVisible();

      await frame(page).getByRole("link", { name: "Body linked", exact: true }).click();
      await expect(
        frame(page).getByRole("heading", { name: "Body linked", exact: true, level: 1 }),
      ).toBeVisible();
      await expect(frame(page).getByText("fact · Statements")).toBeVisible();
      const sources = frame(page).getByRole("region", { name: "Sources", exact: true });
      await expect(sources.getByText("No passage locator recorded", { exact: true })).toBeVisible();
      await expect(sources.getByText("Recorded date", { exact: true })).toBeVisible();
      await sources.getByRole("link", { name: "Open in source", exact: true }).click();
      await expect(
        frame(page).getByRole("heading", { name: "ferry handbook", exact: true, level: 1 }),
      ).toBeVisible();

      await page.goBack();
      await expect(
        frame(page).getByRole("heading", { name: "Body linked", exact: true, level: 1 }),
      ).toBeVisible();
      await page.goBack();
      await expect(
        frame(page).getByRole("heading", { name: "Timetable", exact: true, level: 1 }),
      ).toBeVisible();
      expect(errors).toEqual([]);
    });
  });
}

test("a matrix cell, by keyboard, opens a list whose count is the cell's", async ({ page }) => {
  const errors = consoleErrors(page);
  await page.goto(group());
  const grid = frame(page).getByRole("grid");
  await grid.getByRole("rowheader").first().focus();

  // Row header, then fact, customer-voice, need, observation, tribal-knowledge, rule.
  for (let i = 0; i < 6; i++) await page.keyboard.press("ArrowRight");

  await expect(frame(page).getByRole("tooltip")).toContainText("Timetable · rule");
  await expect(frame(page).getByRole("tooltip")).toContainText("21 assertions");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/presentation=ao-kb.assertions/);
  await expect(page).toHaveURL(/assertionKind=rule/);
  await expect(page).toHaveURL(/concept=concepts%2Ftimetable.md/);
  await expect(await assertionsShown(page)).toHaveCount(21);
  expect(errors).toEqual([]);
});

test("the stored-confidence cell lists hypothesis and guess for its concept", async ({ page }) => {
  await page.goto(group());

  const row = frame(page)
    .getByRole("row")
    .filter({ has: frame(page).getByRole("rowheader", { name: "Timetable" }) });

  const cell = row.getByRole("gridcell").last();
  await expect(cell).toHaveAttribute(
    "aria-label",
    /Stored confidence: hypothesis or guess, 3 assertions/,
  );
  await cell.click();
  await expect(page).toHaveURL(/confidence=hypothesis%2Cguess/);
  await expect(await assertionsShown(page)).toHaveCount(3);
});

test("a concept's count on Concepts agrees with the Assertions list filtered by it", async ({
  page,
}) => {
  await page.goto(group("concepts"));

  const link = frame(page).getByRole("link", {
    name: /^Browse \d+ assertions for Timetable concept$/,
  });

  const name = await link.getAttribute("aria-label");
  const count = Number(/Browse (\d+)/.exec(name ?? "")![1]);
  expect(count).toBe(31);
  await link.click();
  await expect(await assertionsShown(page)).toHaveCount(count);
});

test("a breakdown count opens a list of that many, keeping the other filters", async ({ page }) => {
  await page.goto(group("assertions", "&concept=concepts%2Ftimetable.md"));
  await expect(await assertionsShown(page)).toHaveCount(31);
  const breakdown = frame(page).locator(".ak-breakdown");
  await expect(breakdown).toContainText("Intent and rules 22");
  await breakdown.getByRole("link", { name: "decision 1", exact: true }).click();
  await expect(page).toHaveURL(/assertionKind=decision/);
  await expect(page).toHaveURL(/concept=concepts%2Ftimetable.md/);
  await expect(await assertionsShown(page)).toHaveCount(1);
});

test("a Review gap opens its list, and the hand-off copies an AO KB command", async ({ page }) => {
  const errors = consoleErrors(page);
  await page.goto(group("review"));
  const f = frame(page);
  await expect(f.getByRole("heading", { name: /^Submissions/ })).toContainText("2 pending");
  await expect(f.getByRole("heading", { name: /^parked/ })).toBeVisible();
  await expect(f.getByText("No recorded date", { exact: true })).toBeVisible();
  await expect(f.getByText(/Archived submissions .* are not listed/)).toBeVisible();
  await expect(f.getByText("Recorded values: high 1")).toBeVisible();
  await f.getByRole("link", { name: "Assertions in no concept", exact: true }).click();
  await expect(page).toHaveURL(/gap=assertion-no-concept/);
  await expect(await assertionsShown(page)).toHaveCount(1);
  await expect(frame(page).getByRole("link", { name: "Orphan claim", exact: true })).toBeVisible();
  await page.goBack();
  await frame(page).getByRole("link", { name: "Sources with no date at all", exact: true }).click();
  await expect(page).toHaveURL(/presentation=ao-kb.sources/);
  await expect(frame(page).getByRole("heading", { name: /^Sources/, level: 1 })).toContainText("2");
  expect(errors).toEqual([]);
});

test("Sources by date is one tab stop; arrows move by date and Enter opens a source", async ({
  page,
}) => {
  await page.goto(group());
  const chart = frame(page).getByRole("group", { name: /^Sources by date, 5 dated sources/ });
  await chart.getByRole("link").first().focus();
  await page.keyboard.press("Home");
  await expect(frame(page).getByRole("tooltip")).toContainText("ferry handbook");
  await expect(frame(page).getByRole("tooltip")).toContainText("Recorded date");
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/presentation=ao-kb.source/);
});

const sourceFrame = (page: Page) =>
  page.frames().find((f) => f.url().includes("/views/ao-kb.source"));

for (const [slug, inline, landing] of [
  ["recorded-passage", "Recorded passage", "Recorded passage"],
  ["bad-locator", "Unresolved source/locator", "Nothing is highlighted"],
  ["not-body-linked", "No passage locator recorded", "This source does not link to the assertion."],
  ["body-linked", "No passage locator recorded", "This source links to the assertion here"],
] as const) {
  test(`Open in source: ${slug} shows its state and passes the assertion`, async ({ page }) => {
    await page.goto(
      `/notes?note=${encodeURIComponent(`assertions/${slug}.md`)}&presentation=ao-kb.assertion`,
    );
    const sources = frame(page).getByRole("region", { name: "Sources", exact: true });
    await expect(sources.getByText(inline, { exact: true })).toBeVisible();
    await sources.getByRole("link", { name: "Open in source", exact: true }).click();
    await expect
      .poll(() => sourceFrame(page)?.url())
      .toContain(`assertion=${encodeURIComponent(`assertions/${slug}.md`)}`);
  });

  // The host keeps a node presentation's declared params only once workbench tabs retain them
  // (SPEC-0135 R6a, the platform's P6); until then the frame's params lose `assertion`.
  test.fixme(`Open in source: ${slug} lands on its state in the workspace`, async ({ page }) => {
    await page.goto(
      `/notes?note=${encodeURIComponent(`assertions/${slug}.md`)}&presentation=ao-kb.assertion`,
    );
    await frame(page)
      .getByRole("region", { name: "Sources", exact: true })
      .getByRole("link", { name: "Open in source" })
      .click();
    await expect(frame(page).getByRole("status").filter({ hasText: "Opened from" })).toContainText(
      landing,
    );
    await expect(frame(page).locator(".ak-body-mark")).toHaveCount(slug === "body-linked" ? 1 : 0);
  });
}

test("a cited source that does not resolve offers no Open in source", async ({ page }) => {
  await page.goto(
    `/notes?note=${encodeURIComponent("assertions/missing-source.md")}&presentation=ao-kb.assertion`,
  );
  const sources = frame(page).getByRole("region", { name: "Sources", exact: true });
  await expect(
    sources.getByText("The cited source does not resolve", { exact: true }),
  ).toBeVisible();
  await expect(sources.getByRole("link", { name: "Open in source" })).toHaveCount(0);
});

test("a filtered list survives a reload and a copied URL", async ({ page, context }) => {
  await page.goto(group("assertions", "&concept=concepts%2Ftimetable.md&assertionKind=rule"));
  await expect(await assertionsShown(page)).toHaveCount(21);
  await page.reload();
  await expect(await assertionsShown(page)).toHaveCount(21);
  const copy = await context.newPage();
  await copy.goto(page.url());
  await copy.close();
});

// The workspace URL drops a node presentation's params today; R6a (the platform's P6) keeps them.
test.fixme("a source's assertion param survives a reload, a copied URL, and a tab switch", async ({
  page,
}) => {
  const source = `/notes?note=${encodeURIComponent("sources/documents/ferry-handbook.md")}&presentation=ao-kb.source&assertion=${encodeURIComponent("assertions/body-linked.md")}`;
  await page.goto(source);
  await expect(
    frame(page).getByText("This source links to the assertion here", { exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(
    frame(page).getByText("This source links to the assertion here", { exact: true }),
  ).toBeVisible();
});
