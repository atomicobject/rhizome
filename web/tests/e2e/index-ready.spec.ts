import { expect, test } from "./fixtures";

declare global {
  interface Window {
    indexEventSources: EventSource[];
  }
}

for (const desktop of [false, true]) {
  test(`shows index republication after startup (${desktop ? "desktop" : "browser"})`, async ({
    page,
  }) => {
    await page.addInitScript((enabled) => {
      window.__RHIZOME_DESKTOP__ = enabled;
      const NativeEventSource = window.EventSource;
      window.indexEventSources = [];
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options);

          if (String(url).includes("/api/v1/events")) window.indexEventSources.push(this);
        }
      };
    }, desktop);

    let rebuilding = true;

    await page.route("**/api/v1/status", async (route) => {
      const response = await route.fetch();

      await route.fulfill({ json: { ...(await response.json()), indexState: "ready" } });
    });
    await page.route("**/api/v1/ontology/summary", async (route) => {
      const response = await route.fetch();

      await route.fulfill({ json: { ...(await response.json()), rebuilding } });
    });

    await page.goto("/notes");
    const banner = page.getByTestId("index-ready-banner");

    await expect(banner).toBeVisible();
    await expect(banner).toHaveText("Updating index…");

    rebuilding = false;
    await expect
      .poll(() => page.evaluate(() => window.indexEventSources.length))
      .toBeGreaterThan(0);
    await page.evaluate(() => {
      for (const source of window.indexEventSources) {
        source.dispatchEvent(new MessageEvent("index.changed", { data: "{}" }));
      }
    });
    await expect(banner).toBeHidden();
  });
}
