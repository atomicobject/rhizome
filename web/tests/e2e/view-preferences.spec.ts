import { readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, type APIRequestContext, type Page, test } from "./fixtures";
import { chooseView } from "./workspaceView";

const frame = (page: Page) => page.frameLocator("iframe.custom-view-frame");

const rows = (page: Page) => page.locator("tr.configured-view__row");

async function vaultFile(request: APIRequestContext, ...parts: string[]) {
  const response = await request.get("/api/v1/status");
  expect(response.ok()).toBe(true);
  const status = await response.json();

  return path.join(status.vaultPath, ...parts);
}

async function save(file: string, text: string) {
  await writeFile(`${file}.tmp`, text);
  await rename(`${file}.tmp`, file);
}

async function preferenceWrite(page: Page, action: () => Promise<void>) {
  const saved = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/view-preferences" &&
      response.request().method() === "PATCH",
  );

  await action();
  const response = await saved;
  expect(response.ok(), await response.text()).toBe(true);
}

async function openGroup(page: Page, name: string) {
  await page
    .getByRole("region", { name: "Collections" })
    .getByRole("button", { name, exact: true })
    .click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
}

test("table and cards keep explicit collapse through query changes, content saves, and reload", async ({
  page,
  request,
}) => {
  await page.goto("/notes/tv-guide");
  await chooseView(page, "Table");
  const howto = page.getByRole("button", { name: "Howto 2", exact: true });
  await expect(howto).toHaveAttribute("aria-expanded", "true");
  await preferenceWrite(page, () => howto.click());
  await expect(howto).toHaveAttribute("aria-expanded", "false");

  const search = page.getByRole("searchbox", { name: "Search view" });
  await search.fill("Mooring");
  await expect(page.getByRole("button", { name: "Howto 1", exact: true })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
  await search.fill("");
  await expect(howto).toHaveAttribute("aria-expanded", "false");

  const file = await vaultFile(request, "notes/type-views/guides/mooring-knots.md");
  const original = await readFile(file, "utf8");

  try {
    await save(file, original.replace("every deckhand uses", "every deckhand remembers"));
    await chooseView(page, "Cards");
    await expect(page.locator(".configured-card", { hasText: "Mooring knots" })).toBeVisible();
    const cardsGroup = page.getByRole("button", { name: "Howto 2", exact: true });
    await preferenceWrite(page, () => cardsGroup.click());
    await expect(cardsGroup).toHaveAttribute("aria-expanded", "false");
    await page.reload();
    await expect(cardsGroup).toHaveAttribute("aria-expanded", "false");
    await chooseView(page, "Table");
    await expect(howto).toHaveAttribute("aria-expanded", "false");
    await expect(rows(page)).toHaveCount(3);
    await howto.click();
    await expect(rows(page).filter({ hasText: "Mooring knots" })).toContainText(
      "every deckhand remembers",
    );
  } finally {
    await save(file, original);
  }
});

test("native layout choices persist for one collection and leave another independent", async ({
  page,
  browser,
}) => {
  await page.goto("/notes/tv-guide");
  await chooseView(page, "Table");
  const density = page.getByRole("combobox", { name: "Rows", exact: true });
  await expect(density).toHaveValue("two-line");
  await preferenceWrite(page, async () => {
    await density.selectOption("one-line");
  });

  await page.goto("/notes/tv-work-item");
  await chooseView(page, "Table");
  await expect(density).toHaveValue("two-line");
  await page.goto("/notes/tv-guide");
  await expect(density).toHaveValue("one-line");
  await page.reload();
  await expect(density).toHaveValue("one-line");

  const freshContext = await browser.newContext({ baseURL: new URL(page.url()).origin });

  try {
    const freshPage = await freshContext.newPage();
    await freshPage.goto("/notes/tv-guide");
    await expect(freshPage.getByRole("combobox", { name: "Rows", exact: true })).toHaveValue(
      "one-line",
    );
  } finally {
    await freshContext.close();
  }
});

test("Trace remembers row-tree collapse and selected row type for its concrete group", async ({
  page,
}) => {
  await page.goto("/notes");
  await openGroup(page, "E2E Planning");
  await chooseView(page, "Trace");
  const tree = frame(page).getByRole("button", { name: "Rows under Platform", exact: true });
  await expect(tree).toHaveAttribute("aria-expanded", "true");
  await preferenceWrite(page, () => tree.click());
  await page.reload();
  await expect(tree).toHaveAttribute("aria-expanded", "false");
  await expect(frame(page).getByRole("rowheader", { name: "Search , under Platform" })).toHaveCount(
    0,
  );

  const rowTypes = frame(page).getByRole("radiogroup", { name: "Rows" });
  const work = rowTypes.getByRole("radio", { name: "Work items 4", exact: true });
  await preferenceWrite(page, () => work.locator("xpath=..").click());
  await page.reload();
  await expect(work).toBeChecked();

  await openGroup(page, "E2E Type views");
  await chooseView(page, "Trace");
  const defaultRows = rowTypes.getByRole("radio", { checked: true });
  const defaultName = await defaultRows.getAttribute("value");
  expect(defaultName).not.toBeNull();
  const alternative = rowTypes.getByRole("radio", { checked: false }).first();
  await preferenceWrite(page, () => alternative.locator("xpath=..").click());
  await openGroup(page, "E2E Planning");
  await expect(work).toBeChecked();
  await preferenceWrite(page, () =>
    rowTypes.getByRole("radio", { name: "Areas 3 (default)" }).locator("xpath=..").click(),
  );
  await expect(tree).toHaveAttribute("aria-expanded", "false");
});

test("the public kit isolates two invocations of one authored node view", async ({ page }) => {
  const alpha = "notes/view-demo/effort-a.md";
  const beta = "notes/view-demo/effort-b.md";
  const details = frame(page).getByRole("button", { name: "Node details", exact: true });

  await page.goto(`/notes?note=${encodeURIComponent(alpha)}`);
  await expect(
    frame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await preferenceWrite(page, () => details.click());
  await expect(details).toHaveAttribute("aria-expanded", "false");

  await page.goto(`/notes?note=${encodeURIComponent(beta)}`);
  await expect(
    frame(page).getByRole("heading", { name: "View demo Beta", exact: true }),
  ).toBeVisible();
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await preferenceWrite(page, () => details.click());
  await expect(details).toHaveAttribute("aria-expanded", "false");
  await page.goto(`/notes?note=${encodeURIComponent(alpha)}`);
  await expect(details).toHaveAttribute("aria-expanded", "false");
  await page.reload();
  await expect(details).toHaveAttribute("aria-expanded", "false");
  await expect(frame(page).getByLabel("Preference status")).toHaveText("Saved");

  const familyReset = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/view-preferences/reset" &&
      response.request().method() === "POST",
  );

  await page.getByRole("button", { name: "Reset to shared", exact: true }).click();
  const resetResponse = await familyReset;
  expect(resetResponse.ok(), await resetResponse.text()).toBe(true);
  expect(resetResponse.request().postDataJSON().includeSlots).toBe(true);
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await page.reload();
  await expect(details).toHaveAttribute("aria-expanded", "true");

  await page.goto(`/notes?note=${encodeURIComponent(beta)}`);
  await expect(details).toHaveAttribute("aria-expanded", "false");
});

test("reset follows changed authored defaults and stays reset after reload", async ({
  page,
  request,
}) => {
  const file = await vaultFile(request, ".rhizome/views/e2e-unified/node.yaml");
  const original = await readFile(file, "utf8");
  const details = frame(page).getByRole("button", { name: "Node details", exact: true });
  await page.goto(`/notes?note=${encodeURIComponent("notes/view-demo/effort-a.md")}`);
  await expect(details).toHaveAttribute("aria-expanded", "true");
  await preferenceWrite(page, () => details.click());

  try {
    await save(
      file,
      original.replace(
        "greeting: node configuration",
        "greeting: node configuration\n  detailsOpen: false",
      ),
    );
    await page.reload();
    await expect(details).toHaveAttribute("aria-expanded", "false");
    await preferenceWrite(page, () => details.click());
    await expect(details).toHaveAttribute("aria-expanded", "true");
    await preferenceWrite(page, () =>
      frame(page).getByRole("button", { name: "Reset node details", exact: true }).click(),
    );
    await expect(frame(page).getByLabel("Preference status")).toHaveText("Saved");
    await expect(details).toHaveAttribute("aria-expanded", "false");
    await page.reload();
    await expect(details).toHaveAttribute("aria-expanded", "false");
  } finally {
    await save(file, original);
  }
});

test("shared saves review the YAML change, preserve collapse, and include search only by choice", async ({
  page,
  request,
}) => {
  const file = await vaultFile(request, ".rhizome/views/e2e-unified/efforts.yaml");
  const original = await readFile(file, "utf8");

  try {
    await page.goto("/notes/view-demo-effort");
    await chooseView(page, "Demo native · Table");
    await preferenceWrite(page, async () => {
      await page.getByRole("combobox", { name: "Group by" }).selectOption("status");
    });
    const group = page.locator(".configured-view__group-toggle").first();
    await expect(group).toHaveAttribute("aria-expanded", "true");
    await preferenceWrite(page, () => group.click());
    await page.getByRole("searchbox", { name: "Search view" }).fill("View demo");
    await expect(group).toHaveAttribute("aria-expanded", "false");
    expect(await readFile(file, "utf8")).toBe(original);

    await page.getByRole("button", { name: /^Save view/ }).click();
    const review = page.getByRole("dialog", { name: "Save shared view configuration" });
    await expect(review).toContainText(".rhizome/views");
    await expect(review).toContainText("Grouping");
    await expect(review).toContainText("Column widths and expanded groups stay personal.");
    await expect(review.getByRole("checkbox", { name: /^Include search/ })).not.toBeChecked();
    expect(await readFile(file, "utf8")).toBe(original);

    const sharedSave = page.waitForResponse(
      (response) =>
        response.url().endsWith("/views/e2e.views.efforts/save") &&
        response.request().method() === "POST",
    );

    await review.getByRole("button", { name: "Write view YAML", exact: true }).click();

    const response = await sharedSave;
    expect(response.ok(), await response.text()).toBe(true);
    expect(response.request().postDataJSON().state.search).toBeUndefined();

    const shared = await readFile(file, "utf8");
    expect(shared).not.toBe(original);
    expect(shared).not.toMatch(/search:/);
    await expect(group).toHaveAttribute("aria-expanded", "false");
    await page.reload();
    await expect(group).toHaveAttribute("aria-expanded", "false");
    await expect(page.getByRole("searchbox", { name: "Search view" })).toHaveValue("View demo");

    await page.getByRole("searchbox", { name: "Search view" }).fill("Alpha");
    await page.getByRole("button", { name: /^Save view/ }).click();
    await review.getByRole("checkbox", { name: /^Include search/ }).check();

    const withSearch = page.waitForResponse(
      (result) =>
        result.url().endsWith("/views/e2e.views.efforts/save") &&
        result.request().method() === "POST",
    );

    await review.getByRole("button", { name: "Write view YAML", exact: true }).click();

    const included = await withSearch;
    expect(included.ok(), await included.text()).toBe(true);
    expect(included.request().postDataJSON().state.search).toBe("Alpha");
    expect(await readFile(file, "utf8")).toMatch(/search:\s*["']?Alpha/);
    await page.reload();
    await expect(page.getByRole("searchbox", { name: "Search view" })).toHaveValue("Alpha");
  } finally {
    await save(file, original);
  }
});
