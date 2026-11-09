import { expect, test, type Page } from "@playwright/test";

const workspace = (presentation: string) =>
  `/notes/group/Knowledge?presentation=ao-kb.${presentation}`;

const frame = (page: Page) => page.frameLocator("iframe");

async function assertion(page: Page, slug: string) {
  await page.goto(
    `/notes?note=${encodeURIComponent(`assertions/${slug}.md`)}&presentation=ao-kb.assertion`,
  );
  await expect(
    frame(page).getByRole("heading", { name: "Recorded facts", exact: true }),
  ).toBeVisible();
}

test("find an assertion, read the recorded passage, and follow browser Back and Forward", async ({
  page,
}) => {
  await page.goto(workspace("assertions"));
  const f = frame(page);
  await f.getByLabel("Find text", { exact: true }).fill("Patients book");
  await f.getByLabel("Find text", { exact: true }).press("Enter");
  await expect(page).toHaveURL(/find=Patients\+book|find=Patients%20book/);
  await f.getByRole("link", { name: "Patients book their own slots", exact: true }).click();
  await expect(
    f.getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await expect(f.getByText("recorded in provenance.source", { exact: true })).toBeVisible();
  await expect(f.getByText("Recorded passage · L12-L18", { exact: true })).toBeVisible();
  await expect(f.getByText("Passage hash not verified by Rhizome", { exact: true })).toBeVisible();
  await expect(
    f.getByRole("link", { name: "Open source at line 12", exact: true }),
  ).toHaveAttribute("href", /file=sources%2F_raw%2Fmeetings%2Fclinic-operations-sync.txt&line=12/);
  await expect(f.getByText("Also cited · Sources link #1", { exact: true })).toBeVisible();
  await expect(f.getByText("Also cited · Sources link #3", { exact: true })).toBeVisible();
  await expect(f.getByText("Morgan, clinic operations", { exact: true })).toBeVisible();
  await f.getByRole("link", { name: "Open source at line 12", exact: true }).click();
  await expect(page).toHaveURL(
    /\/explorer\?file=sources%2F_raw%2Fmeetings%2Fclinic-operations-sync.txt&line=12/,
  );
  await expect(page.locator('[data-line="12"][aria-current="location"]')).toHaveText(
    "Morgan, clinic operations: Patients can choose an available slot.",
  );
  await page.goBack();
  await expect(
    frame(page).getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(frame(page).getByLabel("Find text", { exact: true })).toHaveValue("Patients book");
  await page.goForward();
  await expect(
    frame(page).getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
});

test("document-only evidence opens the document without a substitute passage", async ({ page }) => {
  await assertion(page, "same-day-video-question");
  const f = frame(page);
  await expect(f.getByText("No passage locator recorded.", { exact: false }).first()).toBeVisible();
  await expect(f.getByText("Passage hash not verified by Rhizome", { exact: true })).toHaveCount(0);
  await f.getByRole("link", { name: "Open source document", exact: true }).click();
  await expect(
    f.getByRole("heading", { name: "Clinic operations sync", exact: true }),
  ).toBeVisible();
  await f.getByRole("button", { name: "Load source text", exact: true }).click();
  await expect(
    f.getByText("Clinic operations sync. Illustrative transcript.", { exact: true }),
  ).toBeVisible();
});

test("unresolved and malformed locators stay explicit without substitute excerpts", async ({
  page,
}) => {
  for (const [slug, diagnostic] of [
    ["minors-need-guardian", "Unresolved or ambiguous source. No substitute excerpt."],
    ["unresolved-passage-range", "Unresolved locator: span not found. No substitute excerpt."],
    ["malformed-passage-locator", "Malformed locator; no passage excerpt is available."],
  ]) {
    await assertion(page, slug);
    await expect(frame(page).getByText(diagnostic, { exact: true })).toBeVisible();
    await expect(
      frame(page).getByText("Passage hash not verified by Rhizome", { exact: true }),
    ).toHaveCount(0);
  }
});

test("timestamp evidence remains visible and unsupported inline", async ({ page }) => {
  await assertion(page, "timestamp-arrival-buffer");
  await expect(
    frame(page).getByText("Timestamp locator; open the source to read it", { exact: false }),
  ).toBeVisible();
  await expect(
    frame(page).getByText("Unsupported locator · 01:10-01:30", { exact: true }),
  ).toBeVisible();
  await expect(
    frame(page).getByText("Passage hash not verified by Rhizome", { exact: true }),
  ).toHaveCount(0);
});

test("source-note fallback opens its physical source line", async ({ page }) => {
  await assertion(page, "source-note-fallback");
  await frame(page).getByRole("link", { name: "Open source at line 19", exact: true }).click();
  await expect(page).toHaveURL(/file=sources%2Freception-policy-note.md&line=19/);
  await expect(page.locator('[data-line="19"][aria-current="location"]')).toHaveText(
    "# Reception policy note",
  );
  await expect(page.locator('[data-line="21"]')).toHaveText(
    "Reception checks the referral before confirming a slot.",
  );
});

test("supersession history retains the whole three-claim chain and validity facts", async ({
  page,
}) => {
  await assertion(page, "reminders-forty-eight-hours");

  const history = frame(page)
    .locator("section")
    .filter({ has: frame(page).getByRole("heading", { name: "History", exact: true }) });

  await expect(
    history.getByRole("link", { name: "Reminders arrive 2h before", exact: true }),
  ).toBeVisible();
  await expect(
    history.getByRole("link", { name: "Reminders arrive 24h before", exact: true }).first(),
  ).toBeVisible();
  await expect(
    history.getByRole("link", { name: "Reminders arrive 48h before", exact: true }),
  ).toBeVisible();
  await expect(history.getByText("Successor valid from", { exact: false }).first()).toBeVisible();
  await expect(
    frame(page).getByRole("heading", { name: /Reminders arrive 48h before.*Superseded/ }),
  ).toBeVisible();
});

test("conflict reader shows the recorded registry fields and both complete claims", async ({
  page,
}) => {
  await assertion(page, "same-day-cancellation-free");
  const conflict = frame(page).getByRole("region", { name: "Contradiction recorded", exact: true });
  await expect(
    conflict.getByText("Clinic lead must confirm the policy.", { exact: true }),
  ).toBeVisible();
  await expect(
    conflict.getByText("Keep both authored claims visible.", { exact: true }),
  ).toBeVisible();
  await expect(conflict.getByText("open", { exact: true })).toBeVisible();
  await expect(
    conflict.getByRole("link", { name: "Same-day cancellation has fee", exact: true }),
  ).toBeVisible();
  await expect(
    conflict.getByRole("link", { name: "Same-day cancellation is free", exact: true }),
  ).toBeVisible();
  await expect(conflict.getByText(/Rhizome does not pick a winner/)).toBeVisible();
  await expect(conflict.getByRole("link", { name: /cf-001:/ })).toHaveAttribute(
    "href",
    /note=.*_conflicts.md.*#/,
  );
  await expect(frame(page).getByText("May be stale", { exact: true })).toBeVisible();
});

test("source passage margins lead to the claim, its concepts, and affected artifacts", async ({
  page,
}) => {
  await page.goto("/notes?note=sources%2Fclinic-operations-sync.md&presentation=ao-kb.source");
  const f = frame(page);
  await f.getByRole("button", { name: "Load source text", exact: true }).click();
  await expect(
    f.locator(".ak-source-text").getByText("1 citing claim", { exact: true }),
  ).toHaveCount(12);
  await expect(
    f
      .locator(".ak-source-text")
      .getByRole("link", { name: "Video visits are approved", exact: true }),
  ).toBeVisible();
  await f
    .locator(".ak-source-text")
    .getByRole("link", { name: "Patients book their own slots", exact: true })
    .click();
  await expect(
    f.getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await f.getByRole("link", { name: "Self Service concept", exact: true }).click();
  await expect(page).toHaveURL(/note=concepts%2Fself-service.md/);
  await page.goBack();
  await frame(page).getByRole("link", { name: "Booking product brief", exact: true }).click();
  await expect(page).toHaveURL(/note=artifacts%2Fbooking-prd.md/);
});

test("fresh links restore Assertions find, concept, and sort and Concepts filters", async ({
  page,
  context,
}) => {
  await page.goto(workspace("assertions"));
  const f = frame(page);
  await f.getByLabel("Concept", { exact: true }).selectOption("concepts/self-service.md");
  await f.getByLabel("Find text", { exact: true }).fill("Patients");
  await f.getByLabel("Find text", { exact: true }).press("Enter");
  await f.getByRole("button", { name: "Updated", exact: true }).click();
  await expect(page).toHaveURL(/sort=updatedAt-asc/);
  const copy = await context.newPage();
  await copy.goto(page.url());
  await expect(frame(copy).getByLabel("Find text", { exact: true })).toHaveValue("Patients");
  await expect(frame(copy).getByLabel("Concept", { exact: true })).toHaveValue(
    "concepts/self-service.md",
  );
  await expect(frame(copy).getByRole("columnheader", { name: /Updated/ })).toHaveAttribute(
    "aria-sort",
    "ascending",
  );
  await expect(
    frame(copy).getByRole("link", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
  await page.goto(`${workspace("concepts")}&find=Self&concept=concepts%2Fself-service.md`);
  await copy.goto(page.url());
  await expect(frame(copy).getByLabel("Find text", { exact: true })).toHaveValue("Self");
  await expect(
    frame(copy).getByRole("link", { name: "Self Service concept", exact: true }),
  ).toBeVisible();
  await expect(
    frame(copy).getByRole("link", { name: "Cancellation concept", exact: true }),
  ).toHaveCount(0);
  await copy.close();
});

test("keyboard operates Attention basis buttons and assertion table links", async ({ page }) => {
  await page.goto(workspace("attention"));
  const basis = frame(page).getByRole("button", { name: "Basis", exact: false }).first();
  await basis.focus();
  await page.keyboard.press("Enter");
  await expect(basis).toHaveAttribute("aria-expanded", "true");
  await page.keyboard.press("Space");
  await expect(basis).toHaveAttribute("aria-expanded", "false");
  await page.goto(workspace("assertions"));

  const link = frame(page).getByRole("link", {
    name: "Patients book their own slots",
    exact: true,
  });

  await link.focus();
  await page.keyboard.press("Enter");
  await expect(
    frame(page).getByRole("heading", { name: "Patients book their own slots", exact: true }),
  ).toBeVisible();
});

test("Open in Trace preserves native focus and lands on its presentation", async ({ page }) => {
  await assertion(page, "clinic-patient-books-own-slot");
  await frame(page).getByRole("link", { name: "Open in Trace", exact: true }).click();
  await expect(page).toHaveURL(/presentation=ao-kb.trace/);
  await expect(page).toHaveURL(/focus=assertions%2Fclinic-patient-books-own-slot.md/);
  await expect(frame(page).getByRole("combobox", { name: "Trace focus" })).toHaveValue(
    "assertions/clinic-patient-books-own-slot.md",
  );
  await expect(frame(page).getByText("Focus · open the record").first()).toBeVisible();
});

test("claim prose, registry links, review defaults, and incoming impact stay readable", async ({
  page,
}) => {
  await assertion(page, "same-day-cancellation-free");
  const f = frame(page);
  const claim = f.getByRole("region", { name: "Claim", exact: true });
  await expect(claim.locator(".ak-claim > p")).toHaveCount(2);
  await expect(claim.getByRole("link", { name: "cancellation", exact: true })).toHaveAttribute(
    "href",
    /note=concepts%2Fcancellation.md/,
  );
  await expect(claim).not.toContainText("[[");
  const conflict = f.getByRole("region", { name: "Contradiction recorded", exact: true });
  await expect(conflict).not.toContainText("[[");
  await expect(
    conflict
      .locator(".ak-field")
      .filter({ has: f.getByText("Assertions", { exact: true }) })
      .getByRole("link"),
  ).toHaveCount(2);
  await expect(
    conflict.locator(".ak-field").filter({ has: f.getByText("Winner", { exact: true }) }),
  ).toContainText("Not recorded");
  await expect(f.getByText("AO default: never reviewed", { exact: true })).toBeVisible();
  await expect(f.getByText("Not recorded · Not recorded", { exact: true })).toHaveCount(0);
  const related = f.getByRole("region", { name: "Related claims", exact: true });
  await expect(related.getByRole("listitem")).toHaveCount(1);
  await expect(related.getByText("Recorded at both ends", { exact: true })).toBeVisible();
  const impact = f.getByRole("region", { name: "Cited by", exact: true });
  await expect(
    impact
      .getByRole("listitem")
      .filter({ has: f.getByRole("link", { name: "Booking product brief", exact: true }) }),
  ).toContainText("Supports Booking product brief · Artifact · art-000000000001");
  await expect(impact.getByText("sync date not recorded", { exact: true })).toBeVisible();
  await expect(
    impact.getByText("knowledge updated since sync (Oct 21, 2026)", { exact: true }),
  ).toBeVisible();
  await assertion(page, "reminders-twenty-four-hours");

  const winner = frame(page)
    .getByRole("region", { name: "Contradiction recorded", exact: true })
    .locator(".ak-field")
    .filter({ has: frame(page).getByText("Winner", { exact: true }) });

  await expect(
    winner.getByRole("link", { name: "reminders-twenty-four-hours", exact: true }),
  ).toHaveAttribute("href", /note=assertions%2Freminders-twenty-four-hours.md/);

  await page.route("**/api/v1/graphql", async (route) => {
    const response = await route.fetch();

    const body = (await response.text()).replace(
      /"value":\s*"reminders-twenty-four-hours"/g,
      '"value":"[[reminders-twenty-four-hours|24-hour reminder]]"',
    );

    await route.fulfill({ response, body });
  });
  await assertion(page, "reminders-twenty-four-hours");
  await expect(winner.getByRole("link", { name: "24-hour reminder", exact: true })).toHaveAttribute(
    "href",
    /note=assertions%2Freminders-twenty-four-hours.md/,
  );
  await expect(winner).not.toContainText("[[");
  // A read still in flight when the test ends would throw into the next test from route.fetch.
  await page.unrouteAll({ behavior: "ignoreErrors" });
});

test("saved band counts exclude claims absent from the snapshot", async ({ page }) => {
  await page.goto(workspace("attention"));
  const f = frame(page);

  const bands = f
    .locator("section")
    .filter({ has: f.getByRole("button", { name: /Saved assertion bands and basis/ }) });

  const toggle = bands.getByRole("button", { name: /Saved assertion bands and basis/ });
  await expect(toggle).toContainText("21");
  await expect(f.getByText("1 claim is not in the snapshot.", { exact: true })).toBeVisible();

  if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
  await expect(bands.getByRole("listitem")).toHaveCount(21);
  await expect(
    bands.getByRole("link", { name: "Waitlist offers expire hourly", exact: true }),
  ).toHaveCount(0);
});

test("coverage categories use five distinct colors and singular labels", async ({ page }) => {
  await page.goto(workspace("concepts"));
  const f = frame(page);
  await expect(f.getByText("1 explicitly related concept", { exact: true }).first()).toBeVisible();
  await expect(f.getByText("1 explicitly related concepts", { exact: true })).toHaveCount(0);

  for (const colorScheme of ["light", "dark"] satisfies ("light" | "dark")[]) {
    await page.emulateMedia({ colorScheme });
    const swatches = f.locator(".ak-coverage-legend > span > span:first-child");
    await expect(swatches).toHaveCount(5);

    const colors = await swatches.evaluateAll((elements) =>
      elements.map((e) => getComputedStyle(e).backgroundColor),
    );

    expect(new Set(colors).size).toBe(5);
    await expect(swatches.first()).toHaveAttribute("style", /--series-link/);
  }

  await page.goto(workspace("assertions"));
  await expect(frame(page).getByText("1 Sources links", { exact: false })).toHaveCount(0);
  await expect(
    frame(page)
      .getByRole("cell")
      .filter({ hasText: /^1 Sources link/ })
      .first(),
  ).toBeVisible();
});

test("assertion presentations use the pane width and reflow at 728px in both appearances", async ({
  page,
}) => {
  for (const colorScheme of ["light", "dark"] satisfies ("light" | "dark")[]) {
    await page.emulateMedia({ colorScheme });
    await page.setViewportSize({ width: 1280, height: 1000 });
    await assertion(page, "same-day-cancellation-free");

    for (const name of ["Hide sidebar", "Hide right pane"]) {
      const button = page.getByRole("button", { name, exact: true }).first();

      if (await button.count()) await button.click();
    }

    const layout = frame(page).locator(".ak-record-layout");
    await expect(layout).toHaveCSS("grid-template-columns", /^\S+ \S+$/);
    expect(await layout.evaluate((e) => e.clientWidth)).toBeGreaterThanOrEqual(1100);
    await page.setViewportSize({ width: 728, height: 1000 });
    await expect(layout).toHaveCSS("grid-template-columns", /^\S+$/);
    expect(
      await frame(page)
        .locator("body")
        .evaluate((body) => body.scrollWidth > body.clientWidth),
    ).toBe(false);
  }
});
