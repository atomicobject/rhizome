import { expect, test } from "./fixtures";

test("⌘P finds a note by a typo'd query and opens it as a tab, then a code file in Explorer", async ({
  page,
}) => {
  await page.goto("/notes");
  const tabs = page.getByRole("tablist", { name: "Open notes" });

  await expect(tabs).toBeVisible();
  await page.keyboard.press("ControlOrMeta+p");
  const input = page.getByRole("combobox", { name: "Find a note or file" });

  await expect(input).toBeFocused();
  // One swapped pair: "Stratgey" still finds "Sync Strategy".
  await input.fill("sync stratgey");
  const option = page.getByRole("option", { name: /Sync Strategy/ });

  await expect(option).toBeVisible();
  await expect(option).toHaveAttribute("aria-selected", "true");
  await input.press("Enter");
  await expect(input).toBeHidden();
  await expect(tabs.getByRole("tab", { name: /Sync Strategy/ })).toHaveAttribute(
    "aria-selected",
    "true",
  );

  await page.keyboard.press("ControlOrMeta+p");
  await input.fill("main.py");
  await expect(page.getByRole("option", { name: /main\.py/ }).first()).toBeVisible();
  await input.press("Enter");
  await expect(page).toHaveURL(/\/explorer\?file=.*main\.py/);

  // With no query the finder lists recent opens; other specs share the vault,
  // so only assert that the files opened here are among them.
  await page.keyboard.press("ControlOrMeta+p");
  await expect(page.getByRole("option", { name: /main\.py/ }).first()).toBeVisible();
  await expect(page.getByRole("option", { name: /Sync Strategy/ })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(input).toBeHidden();
});
