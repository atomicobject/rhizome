import { expect, test } from "./fixtures";

// SPEC-0136 R5: the kit is served under a build stamp, ships a precompiled
// stylesheet, and loads Tailwind's runtime compiler unless a page opts out.

const arbitrary = "bg-[rgb(1,2,3)]";

test("a custom view outside experiences loads the stamped kit and compiles its own utilities", async ({
  page,
}) => {
  await page.goto("/views/e2e.views.params");
  await expect(page.getByRole("heading", { name: "Params demo" })).toBeVisible();

  const boot = await page.locator("script[src$='/boot.js']").getAttribute("src");
  expect(boot).toMatch(/^\/kit\/v1\/[0-9a-f]{16}\/boot\.js$/);
  const base = boot!.replace(/boot\.js$/, "");
  await expect(page.locator(`link[rel=stylesheet][href$='${base}kit.css']`)).toHaveCount(1);
  await expect(page.locator(`script[src$='${base}tailwind.js']`)).toHaveCount(1);

  const kit = await page.request.get(`${base}kit.js`);
  expect(kit.headers()["cache-control"]).toBe("public, max-age=31536000, immutable");

  // A utility the precompiled sheet lacks still applies, as before the split.
  await page.evaluate((className) => {
    const probe = document.createElement("div");
    probe.id = "tailwind-probe";
    probe.className = className;
    document.body.append(probe);
  }, arbitrary);
  await expect(page.locator("#tailwind-probe")).toHaveCSS("background-color", "rgb(1, 2, 3)");
});

test("a page that opts out of the compiler styles from the precompiled sheet alone", async ({
  page,
}) => {
  await page.goto("/views/e2e.views.params");
  const boot = await page.locator("script[src$='/boot.js']").getAttribute("src");

  await page.route("**/e2e-kit-probe", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: `<!doctype html><html><head><script src="${boot}" data-tailwind="false"></script></head>
<body><p id="kit" class="text-sm text-muted-foreground">kit</p><p id="extra" class="${arbitrary}">extra</p></body></html>`,
    }),
  );
  await page.goto("/e2e-kit-probe");

  await expect(page.locator("#kit")).toHaveCSS("font-size", "13px");
  await expect(page.locator("script[src$='tailwind.js']")).toHaveCount(0);
  await expect(page.locator("#extra")).not.toHaveCSS("background-color", "rgb(1, 2, 3)");
});

test("custom views can import the optional Markdown entry from the stamped kit", async ({
  page,
}) => {
  await page.goto("/views/e2e.views.params");
  await expect(page.getByRole("heading", { name: "Params demo" })).toBeVisible();

  const result = await page.evaluate(async () => {
    const moduleName = "@rhizome/kit/markdown";

    const { markdownChecklist, convertWikilinks } = await import(moduleName);

    return {
      tasks: markdownChecklist("- [x] Done\n- [ ] Pending\n\n    - [x] Nested task"),
      link: convertWikilinks("[[plan.md#Scope|Plan]]"),
    };
  });

  expect(result.tasks).toEqual({ checked: 2, total: 3 });
  expect(result.link).toBe("[Plan](rhizome://note/plan.md%23Scope)");
});
