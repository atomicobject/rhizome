import { expect, test } from "./fixtures";
import { chooseView, expectView } from "./workspaceView";

declare global {
  interface Window {
    paneEventSources: EventSource[];
  }
}

test("follows a relative extensionless wikilink to an embedded node without a reload", async ({
  page,
}) => {
  await page.goto("/notes?note=notes%2Ftask-flow.md");
  const link = page.getByRole("link", { name: "Read linked story", exact: true });
  await expect(link).toHaveAttribute(
    "href",
    "/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#%5Estory-001",
  );
  await link.click();
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await expect(page.getByRole("alert")).toHaveCount(0);
  await page.reload();
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
});

test("recovers an initially missing node after an index event without reloading", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const NativeEventSource = window.EventSource;
    window.paneEventSources = [];
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);

        if (String(url).includes("/api/v1/events")) window.paneEventSources.push(this);
      }
    };
  });
  let failNode = true;
  await page.route("**/api/v1/graphql", async (route) => {
    const body = route.request().postDataJSON();

    if (body.operationName === "PublicNodeDetail" && failNode) {
      await route.fulfill({ json: { data: { node: null } } });

      return;
    }

    await route.continue();
  });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#^story-001");
  await expect(page.getByRole("alert")).toContainText("did not resolve");
  failNode = false;
  await page.evaluate(() => {
    for (const source of window.paneEventSources) {
      source.dispatchEvent(new MessageEvent("index.changed", { data: "{}" }));
    }
  });
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await expect(page.getByRole("alert")).toHaveCount(0);
});

test("opens an embedded user story pane with its title and body", async ({ page }) => {
  // A hand-written link may leave the block-id fragment unencoded.
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#^story-001");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await expect(page.getByText("Choose a note")).toHaveCount(0);
  await expect(
    page.getByText(/retrieve typed story sections with bounded parent context/),
  ).toBeVisible();
});

test("opens an embedded user story pane from a percent-encoded fragment", async ({ page }) => {
  // The app writes fragments percent-encoded; the browser hands the hash back
  // encoded, so the workspace decodes it.
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#%5Estory-001");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await expect(page.getByText("Choose a note")).toHaveCount(0);
});

test("refreshes a node pane without reopening its live subscription", async ({ page }) => {
  await page.addInitScript(() => {
    const NativeEventSource = window.EventSource;
    window.paneEventSources = [];
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, options?: EventSourceInit) {
        super(url, options);

        if (String(url).includes("/api/v1/nodes/events")) {
          window.paneEventSources.push(this);
        }
      }
    };
  });

  await page.setViewportSize({ width: 1280, height: 600 });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText("Search Rewrite");
  await expect.poll(() => page.evaluate(() => window.paneEventSources.length)).toBe(1);

  await chooseView(page, "Markdown");
  await expect(page.getByRole("textbox", { name: "Note source" })).toBeVisible();
  const body = page.locator(".ontology-pane__main:visible");
  await body.evaluate((element) => {
    element.scrollTop = 120;
  });
  const originalScroll = await body.evaluate((element) => element.scrollTop);
  expect(originalScroll).toBeGreaterThan(0);

  let release = () => {};

  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });

  let refreshes = 0;
  await page.route("**/api/v1/graphql", async (route) => {
    if (route.request().postData()?.includes("PublicNodeDetail")) {
      refreshes += 1;
      await pending;
    }

    await route.continue();
  });

  // Deliver an invalidation to the real browser subscription. The subsequent
  // workspace read still goes through the fixture's HTTP/GraphQL server.
  await page.evaluate(() => {
    const source = window.paneEventSources[0];
    const query = new URL(source.url, window.location.href).searchParams;
    source.dispatchEvent(
      new MessageEvent("message", {
        data: JSON.stringify({
          id: "pane-lifecycle-refresh",
          kind: "node.stale",
          ref: {
            notePath: query.get("ref"),
            kind: query.get("kind"),
            nodeId: query.get("nodeId"),
            structuralFingerprint: query.get("structural"),
          },
        }),
      }),
    );
  });
  await expect.poll(() => refreshes).toBe(1);
  await expect(page.getByText("Refreshing note…")).toBeVisible();
  expect(await page.evaluate(() => window.paneEventSources.length)).toBe(1);

  const refreshed = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/graphql") &&
      Boolean(response.request().postData()?.includes("PublicNodeDetail")),
  );

  release();
  await refreshed;
  await expect(page.getByText("Refreshing note…")).toHaveCount(0);
  await expect(page.getByRole("tab", { name: /Search Rewrite/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(await page.evaluate(() => window.paneEventSources.length)).toBe(1);
  await expectView(page, "Markdown");
  expect(
    Math.abs((await body.evaluate((element) => element.scrollTop)) - originalScroll),
  ).toBeLessThanOrEqual(1);
});
