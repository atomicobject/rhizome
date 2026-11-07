import { expect, test, type Page } from "@playwright/test";

const CLAIM = "assertions/clinic-patient-books-own-slot.md";

const SOURCE = "sources/clinic-operations-sync.md";

const url = (params: Record<string, string> = {}) =>
  `/notes/group/Knowledge?${new URLSearchParams({ presentation: "ao-kb.trace", ...params })}`;

const frame = (page: Page) => page.frameLocator('iframe[src*="/views/ao-kb.trace?"]');

async function openTrace(page: Page, params: Record<string, string> = {}) {
  await page.goto(url(params));
  await expect(frame(page).locator("[data-trace-card]").first()).toBeVisible();

  return frame(page);
}

test("bounded depths, authored arrows, and the counted relationship list agree", async ({
  page,
}) => {
  const view = await openTrace(page);
  await expect(view.getByText("12 records", { exact: true })).toBeVisible();
  await expect(view.getByText("15 relationships", { exact: true })).toBeVisible();
  await expect(view.locator("[data-trace-line]")).toHaveCount(8);
  await expect(view.getByText(/Product model not bound in this slice/)).toBeVisible();
  await expect(view.getByRole("radio")).toHaveCount(6);
  await expect(view.getByRole("button", { name: "All", exact: true })).toHaveCount(0);
  const downstream = view.getByRole("radiogroup", { name: "Downstream" });
  await downstream.locator('input[value="2"]').focus();
  await downstream.locator('input[value="2"]').press("ArrowRight");
  await expect(downstream.locator('input[value="3"]')).toBeChecked();
  await expect(page).toHaveURL(/down=3/);
  const table = view.getByRole("table", { name: "Trace relationships" });
  await expect(table.getByRole("row")).toHaveCount(11);
  await view.getByRole("button", { name: "Show 16 more", exact: true }).click();
  await expect(table.getByRole("row")).toHaveCount(27);
  await expect(table.getByText("Second path", { exact: true }).first()).toBeVisible();
  await expect(
    table.getByText("Unclassified · list only, outside traversal counts").first(),
  ).toBeVisible();
  await expect(table.getByText(/Sources #2 \(origin\)/)).toBeVisible();
  await expect(table.getByText(/Sources #3/)).toBeVisible();
  await expect(table.getByRole("link", { name: "Open record", exact: true })).toHaveCount(0);
  await expect(
    table.locator(".ao-trace-row-meta").filter({ hasText: "s-000000000001" }).first(),
  ).toContainText("Source");
  await expect(table.locator(".ao-trace-authored-link").first()).toHaveCSS(
    "text-decoration-line",
    "none",
  );
});

test("dense columns disclose hidden types and expand without changing the counted scope", async ({
  page,
}) => {
  const view = await openTrace(page, { focus: SOURCE, down: "3" });
  await expect(view.getByText("20 records", { exact: true })).toBeVisible();
  await expect(view.getByText("35 relationships", { exact: true })).toBeVisible();
  const column = view.locator('[data-column="d1"]');
  await expect(column.locator("[data-trace-card]")).toHaveCount(6);
  await expect(column.getByText("9 assertions, 4 artifacts", { exact: true })).toBeVisible();
  const showMore = column.getByRole("button", { name: "Show 13 more", exact: true });
  await showMore.focus();
  await showMore.press("Enter");
  await expect(column.locator("[data-trace-card]")).toHaveCount(19);
  await expect(page).toHaveURL(/expand=d1/);
  await expect(view.getByText("20 records", { exact: true })).toBeVisible();
  await expect(view.getByText("35 relationships", { exact: true })).toBeVisible();
  await expect(column.getByRole("button", { name: "Show fewer" })).toBeVisible();
});

test("definition configuration lowers the real query budget independently of the display cap", async ({
  page,
}) => {
  await page.route("**/views/ao-kb.trace?*", async (route) => {
    const response = await route.fetch();
    const body = (await response.text()).replace(/"queryBudget":200/g, '"queryBudget":12');
    await route.fulfill({ response, body });
  });
  const view = await openTrace(page, { focus: SOURCE, down: "3" });
  await expect(
    view.getByText(
      /Incomplete: the query stopped at its 12-record budget while reading downstream hop 1/,
    ),
  ).toBeVisible();
  await expect(view.getByText("12 records", { exact: true })).toBeVisible();
  await expect(view.locator('[data-column="d1"] [data-trace-card]')).toHaveCount(6);
});

test("refocus and record opens preserve browser Back and Forward", async ({ page }) => {
  let view = await openTrace(page);
  await view.getByRole("button", { name: /^Focus on Clinic operations sync\./ }).click();
  await expect(page).toHaveURL(/focus=sources%2Fclinic-operations-sync.md/);
  await expect(view.getByLabel("Trace focus")).toHaveValue(SOURCE);
  await page.goBack();
  await expect(view.getByLabel("Trace focus")).toHaveValue(CLAIM);
  await page.goForward();
  await expect(view.getByLabel("Trace focus")).toHaveValue(SOURCE);
  await view
    .locator(`[data-trace-card="${SOURCE}"]`)
    .getByRole("link", { name: "Open record" })
    .click();
  await expect(page).toHaveURL(/note=sources%2Fclinic-operations-sync.md/);
  await page.goBack();
  view = frame(page);
  await expect(view.getByLabel("Trace focus")).toHaveValue(SOURCE);
  await page.goForward();
  await expect(page).toHaveURL(/note=sources%2Fclinic-operations-sync.md/);
});

test("a fresh workspace link restores focus, depths, expansion, and relationship rows", async ({
  page,
  context,
}) => {
  const view = await openTrace(page, {
    focus: SOURCE,
    up: "1",
    down: "3",
    expand: "d1",
    rows: "true",
    mentions: "false",
  });

  const href = await view.getByRole("link", { name: "Link to this trace" }).getAttribute("href");
  expect(href).toBeTruthy();
  const fresh = await context.newPage();

  try {
    await fresh.goto(href ?? "");
    const restored = frame(fresh);
    await expect(restored.getByLabel("Trace focus")).toHaveValue(SOURCE);
    await expect(
      restored.getByRole("radiogroup", { name: "Upstream" }).locator('input[value="1"]'),
    ).toBeChecked();
    await expect(
      restored.getByRole("radiogroup", { name: "Downstream" }).locator('input[value="3"]'),
    ).toBeChecked();
    await expect(restored.getByLabel("Concept mentions")).not.toBeChecked();
    await expect(restored.locator('[data-column="d1"] [data-trace-card]')).toHaveCount(19);
    await expect(
      restored.getByRole("table", { name: "Trace relationships" }).getByRole("row"),
    ).toHaveCount(36);
  } finally {
    await fresh.close();
  }
});

test("keyboard cards and list links work, source extraction is separate, and unknown focus is explicit", async ({
  page,
}) => {
  const view = await openTrace(page);
  const card = view.getByRole("button", { name: /^Focus on Clinic operations sync\./ });
  await card.focus();
  await expect(card).toBeFocused();
  await expect(card).toHaveCSS("outline-style", "solid");
  await expect(view.locator(".ao-trace-lines .is-active").first()).toBeAttached();
  await card.press("Enter");
  await expect(view.getByLabel("Trace focus")).toHaveValue(SOURCE);
  const history = view.getByRole("region", { name: "Extraction history" });
  // Section is a named section landmark; extraction never becomes another support edge.
  await expect(view.getByRole("heading", { name: "Extraction history" })).toBeVisible();
  await expect(history.getByText("Does not cite this source", { exact: true })).toHaveCount(2);
  await expect(history.getByText("Also cites this source", { exact: true })).toHaveCount(12);

  const recordButton = view
    .getByRole("table")
    .getByRole("link", { name: "Patients book their own slots", exact: true })
    .first();

  await recordButton.focus();
  await recordButton.press("Enter");
  await expect(page).toHaveURL(/note=assertions%2Fclinic-patient-books-own-slot.md/);
  await expect(
    page.frameLocator('iframe[src*="/views/ao-kb.assertion?"]').getByRole("heading", {
      name: "Patients book their own slots",
      exact: true,
    }),
  ).toBeVisible();
  await page.goto(url({ focus: "not-a-record.md" }));
  await expect(
    frame(page).getByText("No record named not-a-record.md here", { exact: true }),
  ).toBeVisible();
});

test("728px uses stacked sections with no drawn lines or page overflow in either appearance", async ({
  page,
}) => {
  await page.setViewportSize({ width: 728, height: 1000 });

  for (const colorScheme of ["light", "dark"] satisfies ("light" | "dark")[]) {
    await page.emulateMedia({ colorScheme });
    const view = await openTrace(page, { focus: SOURCE, down: "3" });
    await expect(view.locator(".ao-trace-lines")).toHaveCSS("display", "none");
    await expect(view.locator(".ao-trace-canvas")).toHaveCSS("display", "flex");
    await expect(view.locator('[data-column="d1"]')).toBeVisible();

    const overflow = await view
      .locator("body")
      .evaluate((body) => body.scrollWidth > body.clientWidth);

    expect(overflow).toBe(false);
  }
});

test("an exact focus lookup opens a record outside the bounded picker directory", async ({
  page,
}) => {
  await page.route("**/api/v1/graphql", async (route) => {
    if (!route.request().postData()?.includes("assertion(first: 201)")) {
      await route.continue();

      return;
    }

    const response = await route.fetch();
    const body: { data: { assertion: { path: string }[] } } = await response.json();
    body.data.assertion = body.data.assertion.filter((record) => record.path !== CLAIM);
    await route.fulfill({ response, json: body });
  });
  const view = await openTrace(page, { focus: CLAIM });
  await expect(view.getByLabel("Trace focus")).toHaveValue(CLAIM);
  await expect(view.getByText("12 records", { exact: true })).toBeVisible();
  await expect(view.getByText("15 relationships", { exact: true })).toBeVisible();
});

test("concept focus follows Related Concepts section links without reinterpreting incidental links", async ({
  page,
}) => {
  const view = await openTrace(page, { focus: "concepts/self-service.md", rows: "true" });
  const table = view.getByRole("table", { name: "Trace relationships" });

  const related = table
    .getByRole("row")
    .filter({ has: view.getByRole("cell", { name: "related concepts", exact: true }) });

  await expect(related).toHaveCount(2);
  await expect(related.getByRole("link", { name: "Reminders concept", exact: true })).toBeVisible();
  await expect(
    related.getByRole("link", { name: "Cancellation concept", exact: true }),
  ).toHaveCount(0);
});
