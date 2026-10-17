import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

test("renders Mermaid diagrams in note read mode", async ({ page }) => {
  await page.goto("/notes?note=notes%2Fmermaid-note.md");

  await expect(page.getByRole("article").getByText("renders a Mermaid diagram")).toBeVisible();
  await expect(page.locator(".mermaid-block svg")).toHaveCount(3);

  for (const diagram of await page.locator(".mermaid-block svg").all()) {
    await expect(diagram).toBeVisible();
  }
});

test("reveals diagram source in Markdown editing without changing the note", async ({ page }) => {
  const fileURL = "/api/v1/files/view?path=notes%2Fmermaid-note.md";
  const before = await (await page.request.get(fileURL)).json();
  await page.goto("/notes?note=notes%2Fmermaid-note.md");
  const pane = page.getByRole("article").first();
  await chooseView(pane, "Markdown");
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const diagrams = pane.locator(".mermaid-block--editor");
  await expect(diagrams).toHaveCount(3);
  await expect(diagrams.first().locator("svg")).toBeVisible();
  await diagrams.first().press("Enter");
  await expect(pane.getByRole("textbox", { name: "Markdown source" })).toContainText(
    "flowchart LR",
  );
  await expect(diagrams).toHaveCount(2);
  const after = await (await page.request.get(fileURL)).json();
  expect(after.content).toBe(before.content);
});
