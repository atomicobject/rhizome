import { mkdir } from "node:fs/promises";
import { expect, type Page } from "./fixtures";

// Shared by the group view specs.
export const customFrame = (page: Page) =>
  page.frameLocator("iframe.custom-view-frame:not([data-incoming])");

export async function openGroup(page: Page, group: string) {
  await page
    .getByRole("region", { name: "Collections" })
    .getByRole("button", { name: group, exact: true })
    .click();
  await expect(page.getByRole("heading", { name: group, exact: true })).toBeVisible();
}

export async function feedbackScreenshot(page: Page, name: string) {
  const feedbackDir = process.env.RHIZOME_FEEDBACK_DIR;

  if (!feedbackDir) return;
  await mkdir(feedbackDir, { recursive: true });
  await page.screenshot({
    path: `${feedbackDir}/${process.env.RHIZOME_FEEDBACK_PHASE ?? "after"}-${name}.png`,
    fullPage: true,
  });
}
