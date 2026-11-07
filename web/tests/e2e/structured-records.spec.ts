import { isJsonObject, isString } from "../../src/api/parse";
import { buildNoteWebHref } from "../../src/lib/content";
import { expect, test, type APIRequestContext } from "./fixtures";
import { chooseView } from "./workspaceView";

async function recordRef(request: APIRequestContext, root: string, id: string) {
  const response = await request.post("/api/v1/graphql", {
    data: { query: `{ ${root}(filters:[{field:"id",op:eq,value:"${id}"}]) { ref { ref } } }` },
  });

  expect(response.ok()).toBe(true);
  const payload: unknown = await response.json();

  if (!isJsonObject(payload) || !isJsonObject(payload.data)) throw new Error("Missing record data");
  const rows = payload.data[root];
  const row = Array.isArray(rows) ? rows[0] : undefined;

  if (!isJsonObject(row) || !isJsonObject(row.ref) || !isString(row.ref.ref))
    throw new Error("Missing record reference");

  return row.ref.ref;
}

test("reads owned YAML records as first-class records in both appearances and widths", async ({
  page,
  context,
}, testInfo) => {
  const ref = await recordRef(page.request, "nativeService", "booking");
  await page.setViewportSize({ width: 1280, height: 1000 });
  await page.goto(buildNoteWebHref(ref));
  const value = page.getByRole("region", { name: "Record value", exact: true });
  await expect(value).toContainText("Literal **text**, never Markdown");
  await expect(value.locator("strong")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Booking service", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(
    page.getByText("Managed by AO KB. Change this record with the owning tool.", { exact: true }),
  ).toBeVisible();
  await expect(value.locator("textarea, [contenteditable=true]")).toHaveCount(0);
  await expect(value.getByLabel("Empty string").first()).toBeVisible();
  await expect(value.getByLabel("Empty map").first()).toBeVisible();
  await expect(value.getByText("null", { exact: true }).first()).toBeVisible();
  await expect(value.getByText("9007199254740993", { exact: true })).toBeVisible();
  await expect(value.locator("dt").filter({ hasText: /^absent$/ })).toHaveCount(0);
  await expect(value.getByText("Booking service", { exact: true })).toHaveCount(0);
  const job = page.locator(".body-collection__row").filter({ hasText: "Check availability" });
  await expect(job).toHaveText("NativeJobCheck availability");
  const info = page.getByRole("complementary", { name: "Note context" });
  await expect(info.getByRole("heading", { name: /^Relations/ })).not.toHaveText("Relations0");
  await expect(info.getByRole("button", { name: "services", exact: true })).toBeVisible();
  await expect(info.getByRole("button", { name: "Check availability", exact: true })).toBeVisible();
  await expect(info.getByRole("button", { name: "Billing service", exact: true })).toHaveCount(2);
  await expect(info.getByRole("button", { name: "Booking workflow", exact: true })).toBeVisible();
  await expect(info.getByText("No relationship context", { exact: false })).toHaveCount(0);
  await expect(info.locator(".lucide-arrow-left").first()).toBeVisible();
  await expect(info.locator(".lucide-arrow-right").first()).toBeVisible();

  for (const width of [1280, 728]) {
    await page.setViewportSize({ width, height: 1000 });

    if (width === 728) {
      await page.getByRole("button", { name: "Hide sidebar", exact: true }).click();
      await expect(
        page.getByRole("button", { name: "Show right pane", exact: true }),
      ).toBeVisible();
    }

    for (const appearance of ["light", "dark"]) {
      await page.evaluate((appearance) => {
        document.documentElement.dataset.appearance = appearance;
      }, appearance);
      await expect(value.getByText("Literal **text**, never Markdown")).toBeVisible();
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      expect(
        await page
          .locator(".ontology-pane__main")
          .evaluate((element) => element.scrollWidth <= element.clientWidth),
      ).toBe(true);
      await page.screenshot({
        path: testInfo.outputPath(`yaml-record-${width}-${appearance}.png`),
        fullPage: true,
      });
    }
  }

  await value.getByText("All record properties", { exact: true }).click();
  await expect(value.getByText("Booking service", { exact: true })).toBeVisible();
  await value.getByText("All record properties", { exact: true }).click();
  await chooseView(page, "Source");
  await expect(page.getByLabel("Note source")).toContainText("services:\n");
  await expect(page.getByLabel("Note source")).toContainText("Billing service");
  await chooseView(page, "Preview");
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy link", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("services.yaml#record:v1:");
  expect(await page.evaluate(() => navigator.clipboard.readText())).not.toContain("#^");
  await job.click();
  await expect(page.getByRole("region", { name: "Record value", exact: true })).toContainText(
    "Check availability",
  );
  await expect(page.getByRole("tab", { name: "Check availability", exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("tab", { name: "Booking service", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("tab", { name: "Check availability", exact: true })).toBeVisible();
});

test("reads JSON trees with native empty values and no edit affordances", async ({ page }) => {
  const ref = await recordRef(page.request, "nativeExport", "snapshot");
  await page.goto(buildNoteWebHref(ref));
  const value = page.getByRole("region", { name: "Record value", exact: true });
  await expect(page.getByRole("tab", { name: "Booking snapshot", exact: true })).toBeVisible();
  await expect(value.getByText("Literal **JSON**, never Markdown")).toBeVisible();
  await expect(value.getByLabel("Empty string")).toBeVisible();
  await expect(value.getByLabel("Empty list")).toBeVisible();
  await expect(value.getByLabel("Empty map")).toBeVisible();
  await expect(value.getByText("null", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await chooseView(page, "Source");
  await expect(page.getByLabel("Note source")).toContainText('"records"');
});

test("keeps the full collection count and names children outside the body cap", async ({
  page,
}) => {
  const ref = await recordRef(page.request, "nativeService", "dense");
  await page.goto(buildNoteWebHref(ref));
  await expect(page.locator(".body-collection__count")).toHaveText("201");
  await expect(page.locator(".body-collection__row")).toHaveCount(199);
  await expect(page.getByRole("status").filter({ hasText: "2 more not loaded" })).toBeVisible();
  await chooseView(page, "Source");
  await expect(page.getByLabel("Note source")).toContainText("Job 201");
});
