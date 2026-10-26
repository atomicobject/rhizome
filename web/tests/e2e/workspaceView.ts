import { expect, type Locator, type Page } from "@playwright/test";

/** The workspace view switcher within `scope`. */
export const workspaceView = (scope: Page | Locator) =>
  scope.getByRole("toolbar", { name: "Workspace view" });

/** Chooses a view by its exact name, from a segment or the custom views menu. */
export async function chooseView(scope: Page | Locator, name: string) {
  const segment = workspaceView(scope)
    .getByRole("button", { name, exact: true })
    .and(workspaceView(scope).locator("[aria-pressed]:not([aria-haspopup])"));

  const trigger = workspaceView(scope).locator("[aria-haspopup=menu]");
  await expect(segment.or(trigger).first()).toBeVisible();

  if (await segment.count()) return segment.click();
  await trigger.click();
  await scope.getByRole("menuitemradio", { name, exact: true }).click();
}

/** Expects the switcher to show `name` as the selected view. */
export async function expectView(scope: Page | Locator, name: string) {
  await expect(workspaceView(scope).locator("[aria-pressed=true]")).toHaveText(name);
}

/** Waits until the visible configured view has finished loading. */
export async function expectViewSettled(page: Page) {
  await expect(page.locator(".configured-view:visible")).toHaveAttribute("aria-busy", "false");
}

/** Opens the view header's View popover (grouping, sort, density, columns, Save view, refresh). */
export async function openViewOptions(scope: Page | Locator) {
  const button = scope.getByRole("button", { name: /^View options/ });

  if ((await button.getAttribute("aria-expanded")) !== "true") await button.click();
}

/** Opens the view header's Filter popover (presets, quick filters, Add filter). */
export async function openFilterMenu(scope: Page | Locator) {
  const button = scope.getByRole("button", { name: /^Filter(,|$)/ });

  if ((await button.getAttribute("aria-expanded")) !== "true") await button.click();
}

/** A choice in the View popover's "1 line | 2 lines" Rows control. */
export const rowsChoice = (scope: Page | Locator, name: "1 line" | "2 lines") =>
  scope
    .getByRole("group", { name: "Rows", exact: true })
    .getByRole("button", { name, exact: true });
