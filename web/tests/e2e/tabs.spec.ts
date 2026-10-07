import { expect, test } from "./fixtures";
import { chooseView, expectView } from "./workspaceView";

declare global {
  interface Window {
    tabEventSources: EventSource[];
  }
}

test("retains one tab per file and restores all open files across reload", async ({ page }) => {
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).not.toHaveClass(/preview/);

  await chooseView(page, "Structure");
  // Plain headings read inline instead of as collapsed section disclosures.
  await expect(page.locator(".body-disclosure")).toHaveCount(0);
  await expect(page.getByText(/_Fallback|fallback-section|fallback-note/i)).toHaveCount(0);
  await page.getByRole("link", { name: "communities/engineering", exact: true }).click();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab", { name: /Engineering/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );

  await page.goBack();
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).not.toHaveClass(/preview/);

  await page.getByRole("link", { name: "communities/engineering", exact: true }).click();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab", { name: /Engineering/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.reload();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab", { name: /Engineering/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await tabs.getByRole("tab", { name: /Task Flow/ }).click();
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText("Task Flow");
  await expect(tabs.getByRole("tab")).toHaveCount(3);
});

test("previews a linked typed note after hover intent", async ({ page }) => {
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  await chooseView(page, "Structure");
  await page.getByRole("link", { name: "Read linked story" }).hover();

  const preview = page.getByRole("region", { name: /Preview of Stable typed retrieval/ });
  await expect(preview).toBeVisible();
  await expect(preview).toContainText("User story");
  await expect(preview).toContainText("STORY-001");
});

test("modifier opens beside the current tab and history activates existing tabs", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const NativeEventSource = window.EventSource;
    window.tabEventSources = [];
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);

        if (String(url).includes("/api/v1/nodes/events")) window.tabEventSources.push(this);
      }
    };
  });
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toBeVisible();
  await chooseView(page, "Structure");
  const originalURL = page.url();
  await page.getByRole("link", { name: "communities/engineering", exact: true }).click({
    modifiers: ["ControlOrMeta"],
  });
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(page).toHaveURL(originalURL);
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("tab", { name: /engineering/i })).not.toHaveClass(/preview/);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          window.tabEventSources.filter((source) => source.readyState !== EventSource.CLOSED)
            .length,
      ),
    )
    .toBe(1);
  await tabs.getByRole("tab", { name: /engineering/i }).click();
  await expect(page).toHaveURL(/note=communities%2Fengineering\.md/);
  await tabs.getByRole("tab", { name: "Home", exact: true }).click();
  await expect(page).toHaveURL(/\/notes$/);
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await page.goBack();
  await expect(tabs.getByRole("tab", { name: /Engineering/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.goBack();
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("tab")).toHaveCount(3);
});

test("collection navigation keeps note tabs and keyboard navigation reaches pinned Home", async ({
  page,
}) => {
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toBeVisible();
  await page.getByRole("button", { name: "Expand Other" }).click();
  await page.getByRole("button", { name: /^Spec \(1\)/ }).click();
  await expect(page).toHaveURL(/\/notes\/spec$/);
  await expect(tabs.getByRole("tab", { name: "Home", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await tabs.getByRole("tab", { name: "Home", exact: true }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.getByRole("tab", { name: /Task Flow/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await page.keyboard.press("Delete");
  await expect(tabs.getByRole("tab")).toHaveCount(1);
  await expect(tabs.getByRole("tab", { name: "Home", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(tabs.getByRole("button", { name: /Close Home/ })).toHaveCount(0);
});

test("gives a typed note a usable full-height reading surface", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(
    page
      .getByRole("tabpanel", { name: /Search Rewrite/ })
      .getByRole("heading", { name: "Search Rewrite", exact: true }),
  ).toBeVisible();
  const body = page.locator(".ontology-pane__main:visible");
  await expect.poll(() => body.evaluate((element) => element.clientHeight)).toBeGreaterThan(300);
  await expect
    .poll(() => body.evaluate((element) => element.clientWidth))
    .toBeGreaterThanOrEqual(600);
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth))
    .toBe(0);
});

test("keeps node drill history in its note tab and returns to a cached parent", async ({
  page,
}) => {
  let nodeReads = 0;
  page.on("request", (request) => {
    if (
      request.url().endsWith("/api/v1/graphql") &&
      request.postData()?.includes("PublicNodeDetail")
    )
      nodeReads += 1;
  });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(
    page
      .getByRole("tabpanel", { name: /Search Rewrite/ })
      .getByRole("heading", { name: "Search Rewrite", exact: true }),
  ).toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await page
    .getByRole("tabpanel")
    .getByRole("button", { name: /Requirements/ })
    .click();
  await expect(
    page
      .getByRole("tabpanel", { name: /Search Rewrite/ })
      .getByRole("heading", { name: "Requirements", exact: true }),
  ).toBeVisible();
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await page.getByRole("button", { name: "Task Flow notes/task-flow.md", exact: true }).click();
  await expect(
    page
      .getByRole("tabpanel", { name: /Task Flow/ })
      .getByRole("heading", { name: "Task Flow", exact: true }),
  ).toBeVisible();
  const readsBeforeReturn = nodeReads;
  await tabs.getByRole("tab", { name: /Search Rewrite/ }).click();
  await expect(
    page
      .getByRole("tabpanel", { name: /Search Rewrite/ })
      .getByRole("heading", { name: "Requirements", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Node path" })
    .getByRole("button", { name: "Search Rewrite", exact: true })
    .click();
  await expect(
    page
      .getByRole("tabpanel", { name: /Search Rewrite/ })
      .getByRole("heading", { name: "Search Rewrite", exact: true }),
  ).toBeVisible();
  expect(nodeReads).toBe(readsBeforeReturn);
  await expect(tabs.getByRole("tab")).toHaveCount(3);
});

test("keeps note chrome compact and remembers Markdown for the session", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  const note = page.getByRole("tabpanel", { name: /Search Rewrite/ });
  await expect(note.getByRole("heading", { name: "Search Rewrite", exact: true })).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() => {
        const strip = document.querySelector(".notes-tabs");
        const panel = document.querySelector(".notes-shell__panel.is-active");
        const body = panel?.querySelector(".ontology-pane__main");

        if (!strip || !panel || !body) return Infinity;

        return (
          strip.getBoundingClientRect().height +
          body.getBoundingClientRect().top -
          panel.getBoundingClientRect().top
        );
      }),
    )
    .toBeLessThan(90);
  await chooseView(note, "Markdown");
  await page.reload();
  await expectView(note, "Markdown");
  await chooseView(note, "Structure");
  await expectView(note, "Structure");
});

test("keeps the active tab visible beside editing controls in a crowded strip", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText("Task Flow");
  await expect
    .poll(() =>
      page.evaluate(() =>
        Object.keys(sessionStorage).find((key) => key.startsWith("rhizome:notes:tabs:v3:")),
      ),
    )
    .toBeTruthy();
  await page.evaluate(() => {
    const key = Object.keys(sessionStorage).find((entry) =>
      entry.startsWith("rhizome:notes:tabs:v3:"),
    );

    if (!key) throw new Error("Expected hydrated vault tabs");
    sessionStorage.setItem(
      key,
      JSON.stringify({
        v: 3,
        tabs: [
          ...Array.from({ length: 12 }, (_, index) => ({
            kind: "note",
            path: `notes/missing-${index}.md`,
            preview: false,
            title: `Background note ${index}`,
          })),
          { kind: "note", path: "notes/task-flow.md", preview: false, title: "Task Flow" },
        ],
      }),
    );
  });
  await page.reload();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab")).toHaveCount(14);

  const visibleActiveTab = () =>
    tabs.evaluate((strip) => {
      const active = strip.querySelector('[aria-selected="true"]');
      const controls = document.querySelector(".notes-shell__session");

      if (!active || !controls) return false;
      const selected = active.getBoundingClientRect();
      const viewport = strip.getBoundingClientRect();

      return (
        selected.left >= viewport.left - 1 &&
        selected.right <= Math.min(viewport.right, controls.getBoundingClientRect().left) + 1
      );
    });

  await expect.poll(visibleActiveTab).toBe(true);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(page.getByRole("button", { name: "Done", exact: true })).toBeVisible();
  await expect.poll(visibleActiveTab).toBe(true);
  await tabs.getByRole("tab", { name: "Task Flow", exact: true }).focus();
  await page.keyboard.press("Home");
  await expect(tabs.getByRole("tab", { name: "Home", exact: true })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect.poll(visibleActiveTab).toBe(true);
});

test("returns to the configured view tab after opening, reloading, and closing a note", async ({
  page,
}) => {
  await page.goto("/notes?view=e2e.action-items");
  const heading = page.getByRole("heading", { name: "Action Items Editing Fixture" });
  await expect(heading).toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  await expect(tabs.getByRole("tab").nth(1)).toHaveAttribute("aria-selected", "true");

  await page
    .locator(".configured-view__table")
    .getByRole("button", { name: /^Open / })
    .first()
    .dblclick();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(tabs.getByRole("tab").nth(2)).toHaveAttribute("aria-selected", "true");
  await expect(page).toHaveURL((url) => url.pathname === "/notes" && url.searchParams.has("note"));
  expect(new URL(page.url()).searchParams.get("view")).toBeNull();

  await page.reload();
  await expect(tabs.getByRole("tab").nth(2)).toHaveAttribute("aria-selected", "true");
  await tabs.getByRole("tab").nth(1).click();
  await expect(page).toHaveURL(/\/notes\?view=e2e.action-items$/);
  await expect(heading).toBeVisible();

  await tabs.getByRole("tab").nth(2).click();
  await tabs
    .getByRole("tab")
    .nth(2)
    .getByRole("button", { name: /^Close / })
    .click();
  await expect(page).toHaveURL(/\/notes\?view=e2e.action-items$/);
  await expect(heading).toBeVisible();
});
