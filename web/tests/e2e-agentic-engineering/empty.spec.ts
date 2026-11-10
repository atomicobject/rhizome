import { expect, test } from "@playwright/test";
import { capture, content, groups, home, openGroup, stage } from "./helpers";

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme });
    test("first day retains the map and composes a complete specify prompt", async ({
      page,
    }, info) => {
      await page.goto("/notes");
      await openGroup(page, "Delivery");

      for (const name of groups)
        await expect(content(page).getByRole("link", { name: new RegExp(name) })).toBeVisible();
      const outcome = content(page).getByRole("textbox", { name: /outcome/i });
      await outcome.fill("Let a passenger reserve a seat on the harbor ferry.");
      await expect(content(page).getByRole("textbox", { name: /prompt/i })).toHaveValue(/specify/);
      await capture(page, info, "empty-home");
      await page.goto(stage("planned"));
      await expect(content(page).getByText(/select.*stor.*effort/i)).toBeVisible();
      await expect(content(page).getByRole("button", { name: /copy/i })).toBeDisabled();
      await capture(page, info, "empty-planned");
      await page.goto(home);
      await expect(outcome).toBeVisible();
    });
  });
}
