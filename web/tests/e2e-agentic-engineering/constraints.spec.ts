import { expect, test } from "@playwright/test";
import { content, home, noOverflow, record, specPath, specTitle } from "./helpers";

test("loading is visible while a real query is held", async ({ page }) => {
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
  } finally {
    release();
  }

  await expect(content(page).getByRole("link", { name: /Ready stories/ })).toBeVisible();
});

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
      await expect(content(page).getByRole("link", { name: /Ready stories/ })).toBeVisible();
      await noOverflow(page);
    });
  });
}
