import { expect, test } from "./fixtures";

test("renders Mermaid diagrams in note read mode", async ({ page }) => {
  await page.goto("/notes?note=notes%2Fmermaid-note.md");

  await expect(page.getByRole("article").getByText("renders a Mermaid diagram")).toBeVisible();
  await expect(page.locator(".mermaid-block svg")).toBeVisible();
});
