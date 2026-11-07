import { expect, test, type FrameLocator, type Page } from "@playwright/test";

// SPEC-0133 R7 / Batch 4: knowledge browsing reaches product-model records through their
// evidence, and each YAML row keeps its own identity in the list, the record, and Trace.

const frame = (page: Page) => page.frameLocator("iframe");

const traceFrame = (page: Page) => page.frameLocator('iframe[src*="/views/ao-kb.trace?"]');

const FEATURES = "artifacts/_assets/_model/features.yaml";

async function heading(f: FrameLocator, name: string) {
  await expect(f.getByRole("heading", { name, exact: true, level: 1 })).toBeVisible();
}

/** Browse Assertions to the claim, read its recorded passage, and open the feature resting on it. */
async function featureFromClaim(page: Page) {
  await page.goto("/notes/group/Knowledge?presentation=ao-kb.assertions&find=Patients+book");
  const f = frame(page);
  await f.getByRole("link", { name: "Patients book their own slots", exact: true }).click();
  await heading(f, "Patients book their own slots");
  await expect(f.getByText("Recorded passage · L12-L18", { exact: true })).toBeVisible();
  const citers = f.getByRole("region", { name: "Cited by", exact: true });
  const feature = citers.getByRole("listitem").filter({ hasText: "Feature · FEAT-book" });
  await expect(feature).toContainText("knowledge updated since file synced (Oct 20, 2026)");
  await feature.getByRole("link", { name: "Book an appointment", exact: true }).click();
  await heading(f, "Book an appointment");

  return f;
}

test("an assertion's exact evidence leads to the model record resting on it, and back", async ({
  page,
}) => {
  const f = await featureFromClaim(page);
  await expect(page).toHaveURL(/note=artifacts%2F_assets%2F_model%2Ffeatures.yaml/);
  expect(decodeURIComponent(new URL(page.url()).hash)).toMatch(/^#record:v1:[0-9a-f]+$/);
  await expect(f.locator(".ak-lead")).toContainText("Feature");
  await expect(f.locator(".ak-lead")).toContainText("FEAT-book");
  const fields = f.getByRole("region", { name: "Fields", exact: true });
  await expect(
    fields.getByRole("link", { name: "Appointment booking", exact: true }),
  ).toBeVisible();
  await expect(
    fields.getByText("No assertion with this ID in the selected product", { exact: true }),
  ).toBeVisible();
  await expect(fields.getByText("Oct 20, 2026", { exact: true })).toBeVisible();
  const evidence = f.getByRole("region", { name: "Evidence it rests on", exact: true });
  await expect(evidence).toContainText(
    "Recorded passage · updated Oct 28, 2026, after file synced",
  );
  await expect(evidence).toContainText("listed 2 times");
  await expect(evidence.getByText("Unresolved", { exact: true })).toBeVisible();
  await page.goBack();
  await heading(frame(page), "Patients book their own slots");
  await page.goForward();
  await heading(frame(page), "Book an appointment");
});

test("two rows of one YAML file open as distinct records with their own locators", async ({
  page,
}) => {
  const f = await featureFromClaim(page);
  const book = page.url();
  const sameFile = f.getByRole("region", { name: "Same file", exact: true });
  await expect(sameFile.getByText("FEAT-cancel", { exact: true })).toBeVisible();
  await sameFile.getByRole("link", { name: "Cancel an appointment", exact: true }).click();
  await heading(f, "Cancel an appointment");
  await expect(f.locator(".ak-lead")).toContainText("FEAT-cancel");
  await expect(f.locator(".ak-lead")).toContainText("features.yaml › /features/1");
  expect(page.url()).not.toBe(book);
  expect(decodeURIComponent(new URL(page.url()).hash)).toMatch(/^#record:v1:[0-9a-f]+$/);
  await page.goBack();
  await heading(frame(page), "Book an appointment");
  expect(page.url()).toBe(book);
});

test("a direct link and a Trace link restore the record and its Trace with Back and Forward", async ({
  page,
  context,
}) => {
  await featureFromClaim(page);
  const direct = page.url();
  let traceLink = "";
  const fresh = await context.newPage();

  try {
    await fresh.goto(direct);
    const f = frame(fresh);
    await heading(f, "Book an appointment");
    await expect(f.locator(".ak-lead")).toContainText("FEAT-book");
    await f.getByRole("link", { name: "Open in Trace", exact: true }).click();
    await expect(fresh).toHaveURL(/presentation=ao-kb.trace/);
    await expect(fresh).toHaveURL(
      new RegExp(`focus=${encodeURIComponent(`${FEATURES}#record:v1:`)}`),
    );
    const trace = traceFrame(fresh);
    const focus = trace.locator(".ao-trace-focus");
    await expect(focus).toContainText("Feature");
    await expect(focus).toContainText("FEAT-book");
    // The graph names the same native identity the record and the list use.
    const key = await focus.getAttribute("data-trace-card");
    expect(key).toMatch(new RegExp(`^${FEATURES}#record:v1:[0-9a-f]+$`));
    expect(decodeURIComponent(new URL(direct).hash.slice(1))).toBe(key?.split("#")[1]);
    // Upstream: the claim it rests on; downstream: the use case it declares.
    await expect(
      trace
        .locator('[data-column="u1"]')
        .getByText("Patients book their own slots", { exact: true }),
    ).toBeVisible();
    await expect(trace.locator('[data-column="d1"]')).toContainText("Use case");
    await expect(trace.locator('[data-column="d1"]')).toContainText("Served by the focus");
    const downstream = trace.getByRole("radiogroup", { name: "Downstream" });
    await downstream.locator('input[value="2"]').focus();
    await downstream.locator('input[value="2"]').press("ArrowRight");
    await expect(fresh).toHaveURL(/down=3/);
    traceLink = fresh.url();
    await fresh.goBack();
    await expect(fresh).not.toHaveURL(/down=3/);
    await expect(
      traceFrame(fresh).getByRole("radiogroup", { name: "Downstream" }).locator('input[value="2"]'),
    ).toBeChecked();
    await fresh.goForward();
    await expect(
      traceFrame(fresh).getByRole("radiogroup", { name: "Downstream" }).locator('input[value="3"]'),
    ).toBeChecked();
    await fresh.goBack();
    await fresh.goBack();
    await heading(frame(fresh), "Book an appointment");
  } finally {
    await fresh.close();
  }

  const linked = await context.newPage();

  try {
    await linked.goto(traceLink);
    await expect(traceFrame(linked).locator(".ao-trace-focus")).toContainText("FEAT-book");
    await expect(
      traceFrame(linked)
        .getByRole("radiogroup", { name: "Downstream" })
        .locator('input[value="3"]'),
    ).toBeChecked();
  } finally {
    await linked.close();
  }
});

test("Attention groups model sync by file and opens each affected row as its own record", async ({
  page,
}) => {
  await page.goto("/notes/group/Knowledge?presentation=ao-kb.attention");
  const f = frame(page);

  const file = f
    .locator(".ak-observation")
    .filter({ has: f.getByText("features.yaml", { exact: true }) })
    .first();

  await expect(file).toContainText("Model file");
  await expect(file).toContainText("synced Oct 20, 2026 · 1 row cites claims updated later");
  await expect(file).toContainText("FEAT-book cites Patients book their own slots");
  await expect(f.getByText("flows.yaml", { exact: true })).toHaveCount(0);
  await expect(f.getByText("Unresolved declared evidence", { exact: true })).toBeVisible();
  await file.getByRole("link", { name: "Book an appointment", exact: true }).click();
  await heading(f, "Book an appointment");
  await expect(f.locator(".ak-lead")).toContainText("FEAT-book");
});

test("a component's code paths open only at their exact path", async ({ page }) => {
  await featureFromClaim(page);
  const f = frame(page);
  await page.goBack();
  const citers = f.getByRole("region", { name: "Cited by", exact: true });
  await citers
    .getByRole("listitem")
    .filter({ hasText: "Component · booking-form" })
    .getByRole("link", { name: "Booking form", exact: true })
    .click();
  await heading(f, "Booking form");
  const declared = f.getByRole("region", { name: "Declared by other records", exact: true });
  await expect(declared.getByRole("listitem")).toHaveCount(2);
  await expect(declared).toContainText("screens.yaml › SCR-BOOK › regions");
  const code = f.getByRole("region", { name: "Referenced code paths", exact: true });
  await expect(code.getByRole("link", { name: "src/booking.go", exact: true })).toHaveAttribute(
    "href",
    /file=src%2Fbooking.go/,
  );
  await expect(code.getByRole("link")).toHaveCount(1);
  await expect(code.getByText("src/not-implemented.go", { exact: true })).toBeVisible();
  await expect(code.getByText("Not available in this repository", { exact: true })).toBeVisible();
});
