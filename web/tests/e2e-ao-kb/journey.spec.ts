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
    "Reception: Patients can choose an available slot.",
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
    f.locator(".ak-source-text").getByText("12 citing claims", { exact: true }),
  ).toBeVisible();
  await f
    .locator(".ak-source-text")
    .getByRole("button", { name: "Show 10 more", exact: false })
    .click();
  await expect(
    f
      .locator(".ak-source-text")
      .getByRole("link", { name: "Video visits are approved", exact: true }),
  ).toBeVisible();
  await f
    .locator(".ak-source-text")
    .getByRole("button", { name: "Show fewer", exact: false })
    .click();
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
