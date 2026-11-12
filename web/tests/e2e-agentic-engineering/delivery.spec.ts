import { expect, test } from "@playwright/test";
import {
  capture,
  content,
  groups,
  home,
  noOverflow,
  openGroup,
  record,
  specPath,
  specTitle,
  stage,
  workspace,
} from "./helpers";

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme, viewport: { width: 1440, height: 1000 } });
    test("Delivery mounts its home with every factual group", async ({ page }, info) => {
      await page.goto("/notes");
      await openGroup(page, "Delivery");

      for (const name of groups)
        await expect(
          content(page)
            .getByRole("navigation", { name: "Delivery workflow" })
            .getByRole("link", { name: new RegExp(name) }),
        ).toBeVisible();
      await expect(content(page).getByRole("link", { name: /Current contracts/ })).toBeVisible();
      await expect(content(page).getByRole("link", { name: /Record review/ })).toBeVisible();
      await expect(content(page).getByText(/overlap/i)).toBeVisible();
      await expect(
        content(page)
          .getByText(/^(?:at least )?3 stories in (?:at least )?2 specs$/)
          .first(),
      ).toBeVisible();
      await expect(
        content(page).getByText("Some sources didn't load", { exact: true }),
      ).toHaveCount(0);
      const panel = page.frameLocator("iframe.panel-frame[data-shown]");
      await expect(
        panel.getByRole("link", { name: /^3 stories in 2 specs Ready stories$/ }),
      ).toBeVisible();
      await capture(page, info, "home");
    });

    for (const [value, title] of [
      ["shaping", "Shaping specs"],
      ["ready", "Ready stories"],
      ["planned", "Planned efforts"],
      ["active", "Active efforts"],
      ["history", "Delivery history"],
      ["contracts", "Current contracts"],
      ["review", "Record review"],
    ]) {
      test(`${title} opens its installed stage`, async ({ page }, info) => {
        await page.goto(stage(value));
        await expect(
          content(page).getByRole("heading", { name: new RegExp(title), level: 1 }),
        ).toBeVisible();

        if (value === "ready")
          await expect(
            content(page)
              .getByText(/^(?:at least )?3 stories in (?:at least )?2 specs$/)
              .first(),
          ).toBeVisible();
        await capture(page, info, `stage-${value}`);
      });
    }

    test("ready selection composes exact scope and clipboard matches preview", async ({
      page,
      context,
    }, info) => {
      await context.grantPermissions(["clipboard-read", "clipboard-write"]);
      await page.goto(stage("ready"));
      const f = content(page);
      const copy = f.getByRole("button", { name: /copy/i });
      await expect(copy).toBeDisabled();
      await f.getByRole("checkbox", { name: /US1.*Reserve a seat/i }).check();
      const prompt = f.getByRole("textbox", { name: /prompt/i });
      const preview = await prompt.inputValue();
      expect(preview).toContain(`${specPath}#^SPEC-9001-US1`);
      expect(preview).not.toContain("#^SPEC-9001-US3");
      expect(preview).toMatch(/effort/);
      expect(preview).toMatch(/plan/);
      await copy.click();
      expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(preview);
      await capture(page, info, "ready-selected");
      await page.goto(stage("active"));
      await expect(content(page).getByRole("button", { name: /copy/i })).toBeDisabled();
    });

    test("effort stage selection loads exact scope and notes unrecorded scope", async ({
      page,
    }, info) => {
      const f = content(page);

      for (const [value, title, route] of [
        ["planned", "Plan harbor boarding", "classify"],
        ["active", "Active all checked", "implement"],
        ["history", "Complete with unselected ready story", "finish"],
      ]) {
        await page.goto(stage(value));
        await f.getByRole("checkbox", { name: `Select ${title}`, exact: true }).check();
        await expect(
          f.getByRole("button", { name: "Copy workflow prompt", exact: true }),
        ).toBeEnabled();
        await expect(f.getByRole("textbox", { name: /prompt/i })).toHaveValue(new RegExp(route));
      }

      await page.goto(stage("active"));
      await f
        .getByRole("checkbox", { name: "Select Harbor legacy workspace", exact: true })
        .check();
      await expect(
        f.getByRole("button", { name: "Copy workflow prompt", exact: true }),
      ).toBeEnabled();
      await expect(f.getByRole("textbox", { name: /prompt/i })).toHaveValue(
        /\(active; frozen scope not recorded, inspect the effort\)/,
      );
      await expect(f.getByText(/Some selected scope is incomplete/)).toHaveCount(0);
      await capture(page, info, "selection-incomplete");
    });

    test("clipboard failure preserves the selected prompt for manual copy", async ({
      page,
    }, info) => {
      await page.addInitScript(() =>
        Object.defineProperty(navigator, "clipboard", {
          value: {
            writeText: async () => {
              throw new Error("synthetic clipboard denial");
            },
          },
        }),
      );
      await page.goto(stage("ready"));
      const f = content(page);
      await f.getByRole("checkbox", { name: /US1.*Reserve a seat/ }).check();
      await f.getByRole("button", { name: "Copy workflow prompt", exact: true }).click();
      await expect(f.getByText(/Could not copy/)).toBeVisible();
      await expect(f.getByRole("textbox", { name: /prompt/i })).toHaveValue(/SPEC-9001-US1/);
      await capture(page, info, "clipboard-error");
    });

    test("spec opens inline criteria and preserves direct link, Back and tabs", async ({
      page,
      context,
    }, info) => {
      await page.goto(record(specPath, "SPEC-9001-US1"));
      await expect(
        content(page).getByRole("heading", { name: specTitle, exact: true }),
      ).toBeVisible();
      await expect(content(page).getByText(/harbor confirmation ends here/)).toBeVisible();
      await expect(content(page).getByText(/nested example is detail/)).toBeVisible();
      await content(page)
        .getByText(/harbor confirmation ends here/)
        .scrollIntoViewIfNeeded();
      await capture(page, info, "spec-expanded");
      await content(page).getByText("Related efforts", { exact: true }).click();
      await expect(
        content(page)
          .getByText(/Governing-specs metadata:/)
          .first(),
      ).toBeVisible();
      await expect(
        content(page)
          .getByText(/Stories In Scope \(Frozen\):/)
          .first(),
      ).toBeVisible();

      const frozenSpec = content(page)
        .getByText(/Spec Set \(Frozen\):/)
        .first();

      await expect(frozenSpec).toBeVisible();
      await frozenSpec.scrollIntoViewIfNeeded();
      await capture(page, info, "frozen-spec-relationships");
      const direct = page.url();
      await page.reload();
      await expect(content(page).getByText(/harbor confirmation ends here/)).toBeVisible();
      const tab = await context.newPage();
      await tab.goto(direct);
      await expect(content(tab).getByText(/harbor confirmation ends here/)).toBeVisible();
      await tab.close();
      await content(page)
        .getByRole("link", { name: /Ready stories/ })
        .first()
        .click();
      await expect(page).toHaveURL(/stage=ready/);
      await expect(
        content(page).getByRole("heading", { name: "Ready stories", exact: true, level: 1 }),
      ).toBeVisible();
      await page.goBack();
      await expect(
        content(page).getByRole("heading", { name: specTitle, exact: true }),
      ).toBeVisible();
      await page.goForward();
      await expect(page).toHaveURL(/stage=ready/);
    });

    test("explicit workspace preserves frozen, entry and log provenance", async ({
      page,
    }, info) => {
      await page.goto(record(workspace("explicit", "00")));
      const f = content(page);
      await expect(
        f.getByRole("heading", { name: "Harbor explicit workspace", exact: true }),
      ).toBeVisible();
      await expect(f.getByText(/Story A first occurrence/)).toBeVisible();
      await expect(f.getByText(/Story A repeated occurrence/)).toBeVisible();
      await expect(f.getByText(/Unresolved authored scope/)).toBeVisible();
      await expect(f.getByText(/snapshot/i).first()).toBeVisible();
      await expect(f.getByText("No approver recorded.", { exact: true })).toBeVisible();
      await capture(page, info, "workspace-provenance");
    });

    test("inline active effort stays active with all checklist items checked", async ({
      page,
    }, info) => {
      await page.goto(record("docs/efforts/2026-11-09-10-02-harbor.md"));
      await expect(
        content(page).getByRole("heading", { name: "Active all checked", exact: true }),
      ).toBeVisible();
      await expect(
        content(page)
          .getByText(/active/i)
          .first(),
      ).toBeVisible();
      await expect(content(page).getByText(/acceptance.ready/i)).toHaveCount(0);
      await capture(page, info, "inline-effort");
    });

    test("legacy workspace keeps unknown authority and opens its safe HTML plan", async ({
      page,
    }, info) => {
      await page.goto(record(workspace("legacy", "02")));
      const f = content(page);
      await expect(
        f.getByRole("heading", { name: "Harbor legacy workspace", exact: true }),
      ).toBeVisible();
      await expect(f.getByText(/scope unknown/)).toBeVisible();
      await capture(page, info, "legacy-workspace");
      await f.getByRole("button", { name: /Open implementation plan:/ }).click();

      const plan = page.frameLocator(
        'iframe[title="Interactive HTML note: docs/efforts/harbor-legacy/plan.html"]',
      );

      await expect(plan.getByText(/Inspect selected sources before implementation/)).toBeVisible();
      await capture(page, info, "html-plan");
    });

    test("panel Find navigates by identifier and records actual visits", async ({ page }, info) => {
      await page.goto(home);
      const panel = page.frameLocator("iframe.panel-frame[data-shown]");
      const find = panel.getByRole("searchbox", { name: /find/i });
      await find.fill("SPEC-9001");
      await find.press("Enter");
      await expect(
        content(page).getByRole("heading", { name: specTitle, exact: true }),
      ).toBeVisible();
      await expect(panel.getByRole("list", { name: /Recent/i })).toContainText(specTitle);
      await page
        .getByRole("button", { name: "Fill the sidebar with Delivery", exact: true })
        .click();
      await panel.getByRole("list", { name: /Recent/i }).scrollIntoViewIfNeeded();
      await capture(page, info, "panel-find-recent");
      await page.reload();
      await expect(panel.getByRole("list", { name: /Recent/i })).toContainText(specTitle);
    });

    for (const width of [768, 390]) {
      test(`long criteria reflow at ${width}px with keyboard controls`, async ({ page }, info) => {
        await page.setViewportSize({ width, height: 900 });
        await page.goto(record(specPath));

        if (width < 480)
          await page.getByRole("button", { name: "Hide sidebar", exact: true }).click();
        const disclosure = content(page).getByRole("button", { name: /US1.*Reserve a seat/i });
        await disclosure.focus();
        await expect(disclosure).toBeFocused();
        await page.keyboard.press("Enter");
        await expect(content(page).getByText(/harbor confirmation ends here/)).toBeVisible();
        await noOverflow(page);
        await capture(page, info, `narrow-${width}`);
      });
    }
  });
}

test("legacy and invalid frozen sources remain visibly incomplete", async ({ page }) => {
  for (const [variant, minute] of [
    ["legacy", "02"],
    ["broken", "03"],
    ["unsupported", "04"],
    ["duplicate", "05"],
    ["missing-sections", "07"],
  ]) {
    await page.goto(record(workspace(variant, minute)));
    await expect(
      content(page).getByRole("heading", { name: `Harbor ${variant} workspace`, exact: true }),
    ).toBeVisible();
    await expect(
      content(page)
        .getByText(/unavailable|incomplete|unresolved|unsupported|duplicate|missing/i)
        .first(),
    ).toBeVisible();
  }
});

test("criterion selection stays criterion only", async ({ page }) => {
  await page.goto(record(workspace("criterion-only", "01")));
  await expect(content(page).getByText(/Only criterion A confirmation/)).toBeVisible();
  await expect(
    content(page)
      .getByText(/criterion.only/i)
      .first(),
  ).toBeVisible();
});

test("unknown stage is an explicit invalid route", async ({ page }) => {
  await page.goto(stage("invented"));
  await expect(
    content(page)
      .getByText(/invalid|unknown stage/i)
      .first(),
  ).toBeVisible();
});

test("scratch install indexes both effort forms without counting materials as efforts", async ({
  request,
}) => {
  const response = await request.post("/api/v1/graphql", {
    data: {
      query:
        "{ effort(first: 100) { id name status ... on EffortNote { path } ... on EffortWorkspace { path } } }",
    },
  });

  expect(response.ok()).toBe(true);
  const result = await response.json();
  expect(result.errors).toBeUndefined();
  expect(result.data.effort).toHaveLength(12);
  expect(result.data.effort).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "Active all checked", status: "active" }),
      expect.objectContaining({
        name: "Harbor explicit workspace",
        path: workspace("explicit", "00"),
      }),
    ]),
  );
});

test("a failed data transport remains an error rather than an empty portfolio", async ({
  page,
}) => {
  await page.route("**/api/v1/graphql", (route) => route.abort("failed"));
  await page.goto(home);
  await expect(
    content(page)
      .getByText(/error|failed|unavailable/i)
      .first(),
  ).toBeVisible();
  await expect(content(page).getByText(/no specs|no efforts|everything.*complete/i)).toHaveCount(0);
});

test("completed unselected stories do not become confirmed record-review findings", async ({
  page,
}) => {
  await page.goto(stage("ready"));
  await expect(
    content(page)
      .getByText(/US1 - Reserve a seat/)
      .first(),
  ).toBeVisible();
  await expect(
    content(page)
      .getByText(/US3 - Recover a saved itinerary/)
      .first(),
  ).toBeVisible();
  await page.goto(stage("review"));
  await expect(
    content(page).getByText("Complete with unselected ready story", { exact: true }),
  ).toHaveCount(0);
});

test("same source preserves its roles without inventing a disagreement", async ({ page }) => {
  await page.goto(record(workspace("same-source", "06")));
  await expect(
    content(page).getByRole("heading", { name: "Harbor same-source workspace", exact: true }),
  ).toBeVisible();
  await expect(
    content(page)
      .getByText(/same source|both.*roles|frozen scope.*work log/i)
      .first(),
  ).toBeVisible();
});
