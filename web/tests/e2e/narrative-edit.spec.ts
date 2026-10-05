import { expect, test } from "./fixtures";

test("replays queued narrative typing and discards back to disk", async ({ page }) => {
  let releaseFirst: () => void = () => {};

  const firstResponseGate = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });

  let firstArrived: () => void = () => {};

  const firstResponseArrived = new Promise<void>((resolve) => {
    firstArrived = resolve;
  });

  await page.route("**/api/v1/edit-sessions", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    firstArrived();
    await firstResponseGate;
    await route.fulfill({ response });
  });
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#%5Estory-001");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const editor = page.getByRole("textbox", { name: "Narrative markdown" });
  await expect(editor).toBeVisible();
  const editorText = async () => (await editor.locator(".cm-line").allTextContents()).join("\n");

  const replaceEditorText = async (text: string) => {
    await editor.click();
    await editor.press("ControlOrMeta+A");
    await page.keyboard.insertText(text);
  };

  const original = await editorText();
  const firstEdit = `${original}\n\nFirst queued edit. `;
  const secondEdit = `${original}\n\nSecond queued edit with a different length.`;
  await replaceEditorText(firstEdit);
  await firstResponseArrived;
  await replaceEditorText(secondEdit);

  const stagedResponse = page.waitForResponse(
    (response) => response.url().endsWith("/stage") && response.request().method() === "POST",
  );

  releaseFirst();
  const response = await stagedResponse;
  expect(response.ok(), await response.text()).toBe(true);
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  await expect.poll(editorText).toBe(secondEdit);
  await expect(page.locator(".ontology-edit-bar__error")).toHaveCount(0);
  page.on("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Discard", exact: true }).click();
  await expect(page.getByRole("button", { name: "Edit", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  await expect.poll(editorText).toBe(original);
});
