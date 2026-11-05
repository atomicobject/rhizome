import { expect, test } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.route("**/api/v1/search?*", async (route) => {
    const url = new URL(route.request().url());
    const query = url.searchParams.get("q") || "";
    const continuation = url.searchParams.get("continuationToken");
    const scope = url.searchParams.get("scope");
    const noteType = url.searchParams.get("noteType");
    const folder = url.searchParams.get("folder");

    if (query === "second search") {
      await route.fulfill({
        json: {
          count: 1,
          total: 1,
          matches: [
            {
              type: "note",
              path: "notes/product-brief.md",
              title: "Product Brief",
              snippetStatus: "available",
              snippet: "Second search evidence remains independent.",
            },
          ],
          lanes: [],
          warnings: [],
        },
      });

      return;
    }

    if (continuation) {
      await route.fulfill({
        json: {
          count: 1,
          total: 2,
          matches: [
            {
              type: "code",
              path: "src/todoapp/main.py",
              title: "main",
              startLine: 4,
              snippetStatus: "available",
              snippet: "def main():",
            },
          ],
          lanes: [],
          warnings: [],
        },
      });

      return;
    }

    await route.fulfill({
      json: {
        count: 1,
        total: 2,
        continuationToken: "page-two",
        targetStatus: "inferred_path",
        confidence: { level: "high", reason: "exact title match" },
        matches: [
          {
            type: "note",
            path: "notes/specs/search-rewrite.md",
            title: "Search Rewrite",
            noteType: noteType || "Spec",
            snippetStatus: "available",
            snippet: "Stable `typed retrieval` preserves ranked search evidence.",
            nodeRef: { notePath: "notes/specs/search-rewrite.md", kind: "note" },
          },
        ],
        lanes: [],
        warnings:
          scope === "notes" || noteType || folder
            ? [{ code: "scope_filter_applied", message: "Search scope was restricted." }]
            : [],
      },
    });
  });
});

test("uses the indexed backend for search, filtering, pagination, and source opening", async ({
  page,
}) => {
  await page.unroute("**/api/v1/search?*");

  const firstResponse = await page.request.get("/api/v1/search?q=task&limit=1");
  expect(firstResponse.ok()).toBeTruthy();
  const first = await firstResponse.json();
  expect(first.matches).toHaveLength(1);
  expect(first.continuationToken).toBeTruthy();
  expect(first).toHaveProperty("count");
  expect(first.offset).toBeUndefined();

  const secondResponse = await page.request.get(
    `/api/v1/search?q=task&limit=1&continuationToken=${encodeURIComponent(first.continuationToken)}`,
  );

  expect(secondResponse.ok()).toBeTruthy();
  const second = await secondResponse.json();
  expect(second.matches).toHaveLength(1);
  expect(second.matches[0].path).not.toBe(first.matches[0].path);

  await page.goto("/notes?search=stable+typed+retrieval");
  const result = page.getByRole("button", { name: "Stable typed retrieval", exact: true });
  await expect(result).toBeVisible();
  await expect(page.getByLabel("Search status")).toBeVisible();
  await expect(page.getByLabel("Search status")).toContainText("Showing");

  const filteredRequest = page.waitForRequest(
    (request) =>
      request.url().includes("/api/v1/search?") &&
      request.url().includes("scope=notes") &&
      request.url().includes("noteType=Spec"),
  );

  await page.getByLabel("Search note type").selectOption("Spec");
  await filteredRequest;
  await expect(result).toBeVisible();

  await result.click();
  await expect(page).toHaveURL(/note=notes%2Fspecs%2Fsearch-rewrite\.md/);
  await expect(page.getByRole("tab", { name: "Search Rewrite", exact: true })).toBeVisible();
  await expect(
    page.getByLabel("Info").getByRole("heading", { name: "Stable typed retrieval", exact: true }),
  ).toBeVisible();
});

test("searches from shared chrome in a full-width page", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/explorer");

  const search = page.getByRole("searchbox", { name: "Search this project" });
  await search.fill("typed retrieval");
  await search.press("Enter");

  await expect(page).toHaveURL(/\/notes\?search=typed\+retrieval/);
  await expect(page.getByRole("heading", { name: "Search results" })).toBeVisible();
  await expect(
    page.getByLabel("Search results for typed retrieval").getByText("for “typed retrieval”", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("Search status")).toContainText("High confidence");
  // The sidebar stays where the user left it (SPEC-0128).
  await expect(page.locator(".notes-left-rail")).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Note context" })).toBeHidden();
  await expect(page.getByPlaceholder("Search notes...")).toHaveCount(0);
  await expect(page.getByPlaceholder("Filter notes...")).toHaveCount(0);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
  await page.screenshot({ path: testInfo.outputPath("search-workspace-1280.png") });

  const filteredRequestPromise = page.waitForRequest(
    (request) =>
      request.url().includes("/api/v1/search?") && request.url().includes("noteType=Spec"),
  );

  await page.getByLabel("Search note type").selectOption("Spec");
  await expect(page).toHaveURL(/scope=notes/);
  await expect(page).toHaveURL(/noteType=Spec/);
  const searchTabID = new URL(page.url()).searchParams.get("searchTab");
  expect(searchTabID).toMatch(/^search-tab:\d+$/);
  const filteredRequest = await filteredRequestPromise;
  expect(new URL(filteredRequest.url()).searchParams.get("scope")).toBe("notes");

  await page.goBack();
  await expect(page.getByLabel("Search note type")).toHaveValue("");
  // A search is a page under the page header: no tab strip without a note tab.
  await expect(page.getByRole("tablist", { name: "Open notes" })).toBeHidden();
  expect(new URL(page.url()).searchParams.get("searchTab")).toBe(searchTabID);

  await page.getByRole("button", { name: "Load more results" }).click();
  await expect(page.getByRole("button", { name: "main", exact: true })).toBeVisible();

  const codeResult = page
    .locator(".search-workspace__result")
    .filter({ has: page.getByRole("button", { name: "main", exact: true }) });

  const geometry = await codeResult.evaluate((result) => {
    const title = result.querySelector(".search-workspace__result-link")!;
    const meta = result.querySelector(".search-workspace__result-source")!;
    const code = result.querySelector("pre")!;

    return {
      titleBottom: title.getBoundingClientRect().bottom,
      metaTop: meta.getBoundingClientRect().top,
      metaBottom: meta.getBoundingClientRect().bottom,
      codeTop: code.getBoundingClientRect().top,
      codeRight: code.getBoundingClientRect().right,
      codeWidth: code.getBoundingClientRect().width,
      resultWidth: result.getBoundingClientRect().width,
      titleFont: getComputedStyle(title).fontFamily,
      titleSize: getComputedStyle(title).fontSize,
    };
  });

  expect(geometry.titleSize).toBe("13px");
  expect(geometry.titleFont).toContain("JetBrains Mono");
  expect(geometry.metaTop - geometry.titleBottom).toBeCloseTo(4, 0);
  expect(geometry.codeTop - geometry.metaBottom).toBeCloseTo(8, 0);
  expect(geometry.codeWidth).toBeCloseTo(Math.min(720, geometry.resultWidth), 0);
  const inlineCode = page.locator(".search-workspace__result p code").first();
  await expect(inlineCode).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
  await expect(inlineCode).toHaveCSS("padding", "0px");
  await expect(inlineCode.locator("mark").first()).toHaveCSS("font-weight", "650");
  await page.getByRole("button", { name: "main", exact: true }).click();
  await expect(page).toHaveURL(/\/explorer\?file=src%2Ftodoapp%2Fmain.py&line=4/);
  await expect(page.locator('[data-line="4"]')).toHaveAttribute("aria-current", "location");

  await page.goBack();
  await expect(page.getByRole("button", { name: "main", exact: true })).toBeVisible();
  expect(new URL(page.url()).searchParams.get("searchTab")).toBe(searchTabID);

  // A sidebar collection replaces the search page; Back brings the search again.
  await page.getByRole("button", { name: "Expand Other" }).click();
  const specRow = page.getByRole("button", { name: /^Spec \(1\)/ });
  await specRow.click();
  await expect(specRow).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { name: "Search results" })).toBeHidden();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Search results" })).toBeVisible();
  await expect(page.getByRole("button", { name: "main", exact: true })).toBeVisible();

  await page.setViewportSize({ width: 1440, height: 900 });
  // The sidebar stays where the user left it (SPEC-0128).
  await expect(page.locator(".notes-left-rail")).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Note context" })).toBeHidden();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
  await page.screenshot({ path: testInfo.outputPath("search-workspace-1440.png") });
});

test("scrolls smoothly after returning to search and restores its position on reload", async ({
  page,
}) => {
  await page.route("**/api/v1/search?*", (route) =>
    route.fulfill({
      json: {
        count: 40,
        matches: Array.from({ length: 40 }, (_, index) => ({
          type: "note",
          path: `notes/result-${index}.md`,
          title: `Result ${index}`,
          snippetStatus: "available",
          snippet: "Search scrolling regression evidence.",
        })),
      },
    }),
  );
  await page.goto("/notes?search=scrolling");
  const results = page.getByRole("main", { name: "Search results for scrolling" });
  await expect(results.getByRole("button", { name: "Result 39", exact: true })).toBeAttached();
  const top = () => results.evaluate((element) => element.scrollTop);

  // Switching tabs blurs this unchanged filter without resetting the saved position.
  await page.getByLabel("Search folder").focus();
  await results.hover();
  await page.mouse.wheel(0, 400);
  await expect.poll(top).toBeGreaterThan(300);
  const saved = await top();

  // Opening a result keeps the search as the page behind the note tab.
  await results.getByRole("button", { name: "Result 10", exact: true }).click();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await tabs.getByRole("tab", { name: /scrolling/i }).click();
  await expect.poll(top).toBe(saved);
  await results.hover();
  await page.mouse.wheel(0, 400);
  await expect.poll(top).toBeGreaterThan(saved + 300);
  const advanced = await top();
  await page.reload();
  // Browser scroll anchoring may adjust slightly as the reloaded header settles.
  await expect.poll(async () => Math.abs((await top()) - advanced)).toBeLessThan(16);
});

test("replaces the search page with a new search and restores it after reload", async ({
  page,
}) => {
  await page.goto("/notes?search=typed+retrieval");
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(page.getByRole("button", { name: "Search Rewrite" })).toBeVisible();
  await expect(tabs).toBeHidden();

  const search = page.getByRole("searchbox", { name: "Search this project" });
  await search.fill("second search");
  await search.press("Enter");
  await expect(page).toHaveURL(/search=second\+search/);
  await expect(page.getByLabel("Search results for second search")).toBeVisible();
  await expect(tabs).toBeHidden();

  await page.goBack();
  await expect(page.getByRole("button", { name: "Search Rewrite" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "Search Rewrite" })).toBeVisible();
  await expect(tabs).toBeHidden();
});

test("keeps note context tabs fixed while removing redundant outline identity", async ({
  page,
}) => {
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(page.getByRole("tab", { name: "Search Rewrite", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  const contextTabs = page.getByRole("tablist", { name: "Note context" });
  const expand = page.getByRole("button", { name: "Show right pane" });

  if (!(await contextTabs.isVisible())) {
    await expect(expand).toBeEnabled();
    await expand.click();
  }

  await expect(contextTabs).toBeVisible();
  const top = async () => contextTabs.evaluate((element) => element.getBoundingClientRect().top);
  const initialTop = await top();

  await contextTabs.getByRole("tab", { name: "Outline", exact: true }).click();
  await expect.poll(top).toBe(initialTop);
  const context = page.getByRole("complementary", { name: "Note context" });
  await expect(context.getByText("OUTLINE", { exact: true })).toHaveCount(0);
  await expect(context.getByText("Search Rewrite", { exact: true })).toHaveCount(0);
  await expect(context.getByRole("button", { name: "Requirements", exact: true })).toBeVisible();

  await contextTabs.getByRole("tab", { name: "Graph", exact: true }).click();
  await expect.poll(top).toBe(initialTop);
  await expect(context.getByRole("heading", { name: "Search Rewrite", exact: true })).toHaveCount(
    0,
  );
});
