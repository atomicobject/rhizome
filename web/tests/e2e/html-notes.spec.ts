import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

test.describe.configure({ mode: "serial", timeout: 60_000 });

const prototypeRoute = "/notes?note=reports%2Fprototype.html&noteQuery=mode%3Dwide#chart";

test("runs an admitted HTML prototype in a retained isolated frame", async ({ page }) => {
  await page.goto(prototypeRoute);
  const frameElement = page.getByTitle("Interactive HTML note: reports/prototype.html");
  await expect(frameElement).toBeVisible({ timeout: 20_000 });
  await expect(frameElement).toHaveAttribute("sandbox", "allow-scripts");

  const metadata = page.locator("details.ontology-properties--html-root");
  await expect(metadata).not.toHaveAttribute("open");
  const summary = metadata.locator("summary");
  await summary.focus();
  await page.keyboard.press("Enter");
  await expect(metadata).toHaveAttribute("open", "");
  await page.keyboard.press("Enter");
  await expect(metadata).not.toHaveAttribute("open");

  const main = page.locator(".ontology-pane__main--html");
  const bounds = await main.boundingBox();
  const frameBounds = await frameElement.boundingBox();
  expect(bounds).not.toBeNull();
  expect(frameBounds).not.toBeNull();
  expect(frameBounds!.width).toBeGreaterThan(bounds!.width - 80);
  expect(frameBounds!.y + frameBounds!.height).toBeGreaterThan(bounds!.y + bounds!.height - 40);
  await page.setViewportSize({ width: 1440, height: 1100 });
  await expect
    .poll(async () => (await frameElement.boundingBox())!.height)
    .toBeGreaterThan(frameBounds!.height + 200);

  const prototype = page.frameLocator(
    'iframe[title="Interactive HTML note: reports/prototype.html"]',
  );

  await expect(prototype.getByText("2 sales rows loaded")).toBeVisible();
  await prototype.locator("body").evaluate((body) => {
    body.dataset.runtimeState = "kept";
  });

  await chooseView(page, "Source");
  await expect(page.getByLabel("Note source")).toContainText("Interactive sales prototype");
  await expect(frameElement).toBeHidden();
  await chooseView(page, "Preview");
  await expect(frameElement).toBeVisible();
  await expect(prototype.locator("body")).toHaveAttribute("data-runtime-state", "kept");
});

test("groups typed properties and root metadata in one collapsed disclosure", async ({ page }) => {
  await page.goto("/notes?note=reports%2Ftyped-report.html");
  const metadata = page.locator("details.ontology-properties--html-root");
  const status = metadata.locator('[data-field="status"]');
  const aliases = metadata.locator('[data-field="aliases"]');
  await expect(status).toHaveCount(1);
  await expect(status).toBeHidden();
  await expect(aliases).toBeHidden();
  await expect(metadata.locator("details")).toHaveCount(0);
  await metadata.locator("summary").click();
  await expect(status).toBeVisible();
  await expect(status).toContainText("ready");
  await expect(aliases).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(status.getByRole("textbox")).toBeVisible();
  await expect(aliases.getByRole("textbox")).toBeVisible();
});

test("keeps the HTML document usable with expanded metadata in a short viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1000, height: 400 });
  await page.goto("/notes?note=reports%2Ftyped-report.html");
  const metadata = page.locator("details.ontology-properties--html-root");
  await metadata.locator("summary").click();
  await expect(metadata.locator('[data-field="aliases"]')).toBeVisible();

  const frame = page.getByTitle("Interactive HTML note: reports/typed-report.html");
  await expect
    .poll(async () => (await frame.boundingBox())?.height ?? 0)
    .toBeGreaterThanOrEqual(160);
  await frame.scrollIntoViewIfNeeded();
  await expect(frame).toBeInViewport();
});

test("requires a parent action and preserves generated CSV bytes", async ({ page }) => {
  await page.goto(prototypeRoute);

  const prototype = page.frameLocator(
    'iframe[title="Interactive HTML note: reports/prototype.html"]',
  );

  await prototype.getByRole("button", { name: "Try invalid CSV export" }).click();
  await expect(prototype.locator("body")).toHaveAttribute("data-invalid-export-result", "rejected");
  await prototype.getByRole("button", { name: "Prepare CSV export" }).click();
  await expect(page.getByText("sales.csv · 30 B")).toBeVisible();
  await page.waitForTimeout(5_500);

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("sales.csv");
  await expect(prototype.locator("body")).toHaveAttribute("data-export-result", "accepted");
  const stream = await download.createReadStream();
  const chunks: Buffer[] = [];

  for await (const chunk of stream) chunks.push(Buffer.from(chunk));
  expect(Buffer.concat(chunks).toString("utf8")).toBe("region,amount\nNorth,42\nWest,58");
});

test("intercepts ordinary blob download links for parent approval", async ({ page }) => {
  await page.goto(prototypeRoute);

  const prototype = page.frameLocator(
    'iframe[title="Interactive HTML note: reports/prototype.html"]',
  );

  await prototype.getByRole("link", { name: "Prepare CSV link export" }).click();
  await expect(page.getByText("sales-from-link.csv · 30 B")).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("sales-from-link.csv");
  const stream = await download.createReadStream();
  const chunks: Buffer[] = [];

  for await (const chunk of stream) chunks.push(Buffer.from(chunk));
  expect(Buffer.concat(chunks).toString("utf8")).toBe("region,amount\nNorth,42\nWest,58");
});

test("routes HTML links through retained note tabs", async ({ page }) => {
  await page.goto(prototypeRoute);

  const prototype = page.frameLocator(
    'iframe[title="Interactive HTML note: reports/prototype.html"]',
  );

  await prototype.getByRole("link", { name: "Read the static report" }).click();
  await expect(page).toHaveURL(/note=reports%2Ftyped-report\.html/);
  await expect(page.getByRole("tab", { name: /Quarterly report/ })).toBeVisible();
  await expect(page.getByTitle("Interactive HTML note: reports/typed-report.html")).toBeVisible();
});

test("previews, discards, and saves root metadata without changing authored HTML", async ({
  page,
}) => {
  await page.goto("/notes?note=reports%2Funtyped-report.htm");
  await page.getByText("HTML metadata", { exact: true }).click();
  await page.getByRole("button", { name: "Edit", exact: true }).click();

  const tags = page.getByLabel("tags");
  await tags.fill("draft\nprototype");
  await tags.press("Control+Enter");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Discard", exact: true }).click();

  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const title = page.getByLabel("title");
  await title.fill("Saved HTML report");
  await title.press("Enter");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Saved HTML report" })).toBeVisible();

  await chooseView(page, "Source");
  const source = page.getByLabel("Note source");
  await expect(source).toContainText('<script id="rhizome-metadata" type="application/json">');
  await expect(source).toContainText('"title": "Saved HTML report"');
  await expect(source).toContainText("Untyped HTML remains useful");
  await expect(source).not.toContainText('"tags"');
});
