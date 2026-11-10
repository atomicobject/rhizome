import { isJsonObject, isString, type JsonValue } from "../../src/api/parse";
import { expect, test } from "@playwright/test";
import { capture, content, home, noOverflow, record, specPath, specTitle } from "./helpers";

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme });
    test("loading is visible while a real query is held", async ({ page }, info) => {
      let release = () => {};

      const held = new Promise<void>((resolve) => {
        release = resolve;
      });

      await page.route("**/api/v1/graphql", async (route) => {
        await held;
        await route.continue();
      });

      try {
        await page.goto(home);
        await expect(
          content(page)
            .getByText(/loading/i)
            .first(),
        ).toBeVisible();
        await expect(content(page).getByText(/no specs|no efforts/i)).toHaveCount(0);
        await capture(page, info, "loading");
      } finally {
        release();
      }

      await expect(
        content(page)
          .getByRole("navigation", { name: "Delivery workflow" })
          .getByRole("link", { name: /Ready stories/ }),
      ).toBeVisible();
    });
  });
}

test("a host workbench tab retains a focused story after visiting another record", async ({
  page,
}) => {
  await page.goto(record(specPath, "SPEC-9001-US1"));
  await expect(content(page).getByText(/harbor confirmation ends here/)).toBeVisible();
  await page.goto(record("docs/specs/proposed-2.md"));
  await expect(
    content(page).getByRole("heading", { name: "Harbor proposed contract 2", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("tablist", { name: "Open notes" })
    .getByRole("tab", { name: new RegExp(specTitle) })
    .click();
  await expect(content(page).getByText(/harbor confirmation ends here/)).toBeVisible();
});

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme, viewport: { width: 720, height: 450 } });
    test("home remains usable at the viewport of a 1440px display at 200 percent zoom", async ({
      page,
    }) => {
      await page.goto(home);
      await expect(
        content(page)
          .getByRole("navigation", { name: "Delivery workflow" })
          .getByRole("link", { name: /Ready stories/ }),
      ).toBeVisible();
      await noOverflow(page);
    });
  });
}

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme });
    test("retained record remains readable after refresh failure and recovers", async ({
      page,
    }, info) => {
      await page.goto(record("docs/efforts/2026-11-09-10-02-harbor.md"));
      const f = content(page);
      await expect(
        f.getByRole("heading", { name: "Active all checked", exact: true }),
      ).toBeVisible();
      await f.getByRole("checkbox", { name: /Include this effort/ }).check();
      await expect(
        f.getByRole("button", { name: "Copy workflow prompt", exact: true }),
      ).toBeEnabled();
      await page.route("**/api/v1/graphql", (route) => route.abort("failed"));
      await f.getByRole("button", { name: "Refresh records", exact: true }).click();
      await expect(f.getByText(/Showing previous saved records/)).toBeVisible();
      await expect(
        f.getByRole("heading", { name: "Active all checked", exact: true }),
      ).toBeVisible();
      await expect(
        f.getByRole("button", { name: "Copy workflow prompt", exact: true }),
      ).toBeDisabled();
      await capture(page, info, "retained-error");
      await page.unroute("**/api/v1/graphql");
      await f.getByRole("button", { name: "Retry read", exact: true }).click();
      await expect(f.getByText(/Showing previous saved records/)).toHaveCount(0);
      await expect(
        f.getByRole("button", { name: "Copy workflow prompt", exact: true }),
      ).toBeEnabled();
    });
  });
}

test("refresh reports removed selection before allowing a reduced prompt", async ({
  page,
}, info) => {
  await page.goto("/notes?view=agentic-engineering.stage&stage=ready");
  const f = content(page);
  await f.getByRole("checkbox", { name: /Select US1.*Reserve a seat/ }).check();
  await f.getByRole("checkbox", { name: /Select US3.*Recover a saved itinerary/ }).check();
  await expect(f.getByRole("textbox", { name: /prompt/i })).toHaveValue(/SPEC-9001-US3/);
  let changed = 0;
  await page.route("**/api/v1/graphql", async (route) => {
    const response = await route.fetch();
    const json: unknown = await response.json();

    if (!isJsonObject(json) || !isJsonObject(json.data))
      throw new Error("Expected fixture GraphQL data");

    function isSelectedStory(ref: JsonValue | undefined) {
      return isJsonObject(ref) && ref.fragment === "^SPEC-9001-US3";
    }

    function update(value: JsonValue) {
      if (Array.isArray(value)) {
        value.forEach(update);

        return;
      }

      if (!isJsonObject(value)) return;

      const linkTarget =
        isJsonObject(value.locator) && isJsonObject(value.locator.linkTarget)
          ? value.locator.linkTarget
          : null;

      if (isSelectedStory(value.ref) || isSelectedStory(linkTarget?.ref)) {
        const workspace = isJsonObject(value.workspace) ? value.workspace : null;

        if (Array.isArray(workspace?.fields))
          for (const field of workspace.fields) {
            if (isJsonObject(field) && field.name === "status") {
              field.values = ["satisfied"];
              changed++;
            }
          }
      }

      Object.values(value).forEach(update);
    }

    update(json.data);
    await route.fulfill({ response, json });
  });
  await f.getByRole("button", { name: "Refresh records", exact: true }).click();
  await expect(f.getByText(/selected records? (?:is|are) no longer eligible/i)).toBeVisible();
  expect(changed).toBeGreaterThan(0);
  await expect(f.getByRole("button", { name: "Copy workflow prompt", exact: true })).toBeDisabled();
  await capture(page, info, "changed-selection");
  await f.getByRole("button", { name: /Use remaining eligible selection/ }).click();
  await expect(f.getByRole("button", { name: "Copy workflow prompt", exact: true })).toBeEnabled();
  const prompt = await f.getByRole("textbox", { name: /prompt/i }).inputValue();
  expect(prompt).toContain("SPEC-9001-US1");
  expect(prompt).not.toContain("SPEC-9001-US3");
});

test("inline Markdown links resolve from their authored source", async ({ page }) => {
  await page.goto(record(specPath, "SPEC-9001-US1"));
  const f = content(page);
  await f.getByRole("link", { name: "Recorded departure contract", exact: true }).click();
  await expect(
    content(page).getByRole("heading", { name: "Harbor active contract 5", exact: true }),
  ).toBeVisible();
  await page.goBack();
  await f.getByRole("link", { name: "relative plan", exact: true }).click();

  const plan = page.frameLocator(
    'iframe[title="Interactive HTML note: docs/efforts/harbor-legacy/plan.html"]',
  );

  await expect(
    plan.getByText("Inspect selected sources before implementation.", { exact: true }),
  ).toBeVisible();
});

test("ambiguous spec sections disable ready selection while preserving authored criteria", async ({
  page,
}) => {
  let changed = 0;
  await page.route("**/api/v1/graphql", async (route) => {
    const response = await route.fetch();
    const json: unknown = await response.json();

    if (!isJsonObject(json) || !isJsonObject(json.data))
      throw new Error("Expected fixture GraphQL data");

    function update(value: JsonValue) {
      if (Array.isArray(value)) {
        value.forEach(update);

        return;
      }

      if (!isJsonObject(value)) return;

      const workspace =
        isJsonObject(value.ref) && value.ref.notePath === specPath && isJsonObject(value.workspace)
          ? value.workspace
          : null;

      if (Array.isArray(workspace?.fields))
        for (const field of workspace.fields) {
          if (
            isJsonObject(field) &&
            field.name === "userStories" &&
            Array.isArray(field.sectionNodes) &&
            field.sectionNodes.length
          ) {
            field.sectionNodes.push(field.sectionNodes[0]);
            field.issues = [
              { code: "cardinality", message: "Section must match a single heading" },
            ];
            changed++;
          }
        }

      Object.values(value).forEach(update);
    }

    update(json.data);
    await route.fulfill({ response, json });
  });
  await page.goto("/notes?view=agentic-engineering.stage&stage=ready");
  const f = content(page);
  await expect(f.getByRole("checkbox", { name: /Select US1.*Reserve a seat/ })).toBeDisabled();
  await expect(f.getByRole("button", { name: "Copy workflow prompt", exact: true })).toBeDisabled();
  expect(changed).toBeGreaterThan(0);
  await f.getByRole("checkbox", { name: /Select US1.*Inspect proposed context/ }).check();
  await expect(f.getByRole("button", { name: "Copy workflow prompt", exact: true })).toBeEnabled();
  await expect(f.getByRole("textbox", { name: /prompt/i })).toHaveValue(/proposed-2\.md/);
  await page.goto(record(specPath, "SPEC-9001-US1"));
  await expect(f.getByText(/harbor confirmation ends here/)).toBeVisible();
});

test("refresh rereads selected effort scope before allowing Copy and recovers", async ({
  page,
}, info) => {
  const path = "docs/efforts/2026-11-09-10-02-harbor.md";
  await page.route("**/api/v1/events", (route) => route.abort("blockedbyclient"));
  await page.goto("/notes?view=agentic-engineering.stage&stage=active");
  const f = content(page);
  const copy = f.getByRole("button", { name: "Copy workflow prompt", exact: true });
  await f.getByRole("checkbox", { name: "Select Active all checked", exact: true }).check();
  await expect(copy).toBeEnabled();
  let detailReads = 0;
  const generations: JsonValue[] = [];
  let release = () => {};

  const held = new Promise<void>((resolve) => {
    release = resolve;
  });

  await page.route("**/api/v1/graphql", async (route) => {
    const response = await route.fetch();
    const json: unknown = await response.json();

    if (!isJsonObject(json) || !isJsonObject(json.data))
      throw new Error("Expected fixture GraphQL data");
    let ownDetail = false;

    function update(value: JsonValue) {
      if (Array.isArray(value)) {
        value.forEach(update);

        return;
      }

      if (!isJsonObject(value)) return;

      if (isJsonObject(value.ref) && value.ref.notePath === path && isJsonObject(value.workspace)) {
        if (isString(value.content)) ownDetail = true;

        if (Array.isArray(value.workspace.fields))
          for (const field of value.workspace.fields) {
            if (isJsonObject(field) && field.name === "storiesInScopeFrozen") {
              field.sectionNodes = [];
              field.issues = [{ code: "unresolved", message: "Frozen section unavailable" }];
            }
          }
      }

      Object.values(value).forEach(update);
    }

    update(json.data);
    generations.push({
      generation: isJsonObject(json.extensions) ? (json.extensions.generation ?? null) : null,
      ownDetail,
    });

    if (ownDetail) {
      detailReads++;
      await held;
    }

    await route.fulfill({ response, json });
  });

  try {
    await f.getByRole("button", { name: "Refresh records", exact: true }).click();
    await expect.poll(() => detailReads).toBeGreaterThan(0);
    await expect(copy).toBeDisabled();
  } finally {
    release();
    await info.attach("refresh-generations", {
      body: JSON.stringify(generations),
      contentType: "application/json",
    });
  }

  await expect(f.getByText(/Some selected scope is incomplete/)).toBeVisible();
  await expect(copy).toBeDisabled();
  await page.unroute("**/api/v1/graphql");
  await f.getByRole("button", { name: "Refresh records", exact: true }).click();
  await expect(copy).toBeEnabled();
});
