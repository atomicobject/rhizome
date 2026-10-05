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

/** Opens the rail's note list, which starts collapsed while a collection view lists the records. */
export async function expandNoteList(page: Page, name: string) {
  const toggle = page.getByRole("button", { name, exact: true });
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await toggle.click();
}
