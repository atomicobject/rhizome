import { isJsonObject, isString } from "../../src/api/parse";
import { buildNoteWebHref } from "../../src/lib/content";
import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

test("reads owned YAML records as native values in both appearances", async ({
  page,
  context,
}, testInfo) => {
  const response = await page.request.post("/api/v1/graphql", {
    data: {
      query: '{ nativeService(filters:[{field:"id",op:eq,value:"booking"}]) { ref { ref } } }',
    },
  });

  expect(response.ok()).toBe(true);
  const payload: unknown = await response.json();

  if (
    !isJsonObject(payload) ||
    !isJsonObject(payload.data) ||
    !Array.isArray(payload.data.nativeService)
  ) {
    throw new Error("Missing native service rows");
  }

  const row: unknown = payload.data.nativeService[0];

  if (!isJsonObject(row) || !isJsonObject(row.ref) || !isString(row.ref.ref)) {
    throw new Error("Missing native service reference");
  }

  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(buildNoteWebHref(row.ref.ref));
  const value = page.getByRole("region", { name: "Record value", exact: true });
  await expect(value).toContainText("Literal **text**, never Markdown");
  await expect(value.locator("strong")).toHaveCount(0);
  await expect(
    page.getByText("Change this record with the owning tool.", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(value.locator("textarea, [contenteditable=true]")).toHaveCount(0);
  await expect(
    page.locator(".body-collection__row").filter({ hasText: "Check availability" }),
  ).toBeVisible();

  for (const appearance of ["light", "dark"]) {
    await page.evaluate((value) => {
      document.documentElement.dataset.appearance = value;
    }, appearance);
    await page.screenshot({
      path: testInfo.outputPath(`yaml-record-${appearance}.png`),
      fullPage: true,
    });
  }

  await chooseView(page, "Source");
  await expect(page.getByLabel("Note source")).toContainText("services:\n");
  await expect(page.getByLabel("Note source")).toContainText("Billing service");
  await chooseView(page, "Preview");
  await expect(value).toBeVisible();
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy link", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("services.yaml#record:v1:");
  const copied = await page.evaluate(() => navigator.clipboard.readText());
  expect(copied).toContain("services.yaml#record:v1:");
  expect(copied).not.toContain("#^");
  await page.locator(".body-collection__row").filter({ hasText: "Check availability" }).click();
  await expect(
    page.getByRole("region", { name: "Record value", exact: true }).last(),
  ).toContainText("Check availability");
});
