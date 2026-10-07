import { expect, test } from "./fixtures";
import { chooseView, expandNoteList } from "./workspaceView";

test("loads the graph and navigates notes/code from the tree", async ({ page }) => {
  await page.goto("/explorer");
  const rail = page.locator(".explorer-rail");

  await expect(page.getByText("Graph unavailable")).toHaveCount(0);
  await expect(page.locator(".explorer-bar__title h1")).toHaveText("Explorer");

  await rail.getByRole("button", { name: /notes/i }).click();
  await rail.getByRole("button", { name: /task-flow\.md/i }).click();
  await expect(page).toHaveURL(/\/notes\?note=notes%2Ftask-flow\.md$/);
  await expect(page.locator(".ontology-identity__title")).toHaveText("Task Flow");

  await page.goto("/explorer");
  await expect(page.locator(".explorer-bar__title h1")).toHaveText("Explorer");

  await rail.getByRole("button", { name: /\bsrc$/i }).click();
  await rail.getByRole("button", { name: /todoapp/i }).click();
  await rail.getByRole("button", { name: /main\.py/i }).click();
  await expect(page.getByRole("heading", { name: "main.py" })).toBeVisible();
  await expect(page.locator(".explorer-pane__code")).toBeVisible();
});

test("focuses folders and supports search suggestions", async ({ page }) => {
  await page.goto("/explorer");
  const rail = page.locator(".explorer-rail");

  await rail.getByRole("button", { name: /\bsrc$/i }).click();
  await expect(page.getByRole("heading", { name: "src" })).toBeVisible();
  await expect(page.getByText("Contents")).toBeVisible();

  const search = page.getByPlaceholder("Search notes + code");
  await search.fill("task-flow");
  await search.focus();
  await expect(page.locator(".explorer-suggestions")).toBeVisible();
  await page.locator(".explorer-suggestions button").first().click();

  await expect(page).toHaveURL(/\/notes\?note=notes%2Ftask-flow\.md$/);
  await expect(page.locator(".ontology-identity__title")).toHaveText("Task Flow");
});

test("keeps the Specs type bound to its delayed response identity", async ({ page }) => {
  let releaseResponse = () => {};

  const responseGate = new Promise<void>((resolve) => {
    releaseResponse = resolve;
  });

  await page.route("**/api/v1/ontology/types/Spec", async (route) => {
    await responseGate;
    await route.continue();
  });

  await page.goto("/notes");
  await page.getByRole("button", { name: "Expand Other" }).click();
  await page.getByRole("button", { name: /^Spec \(1\)/ }).click();
  // The Specs table lists its records, so the rail's list starts collapsed.
  await expandNoteList(page, "Spec notes");

  const noteList = page.getByRole("region", { name: "Note list" });
  await expect(noteList.getByText("Loading notes…")).toBeVisible();
  releaseResponse();
  await expect(noteList.getByText("Search Rewrite")).toBeVisible();
  await expect(page).toHaveURL(/\/notes\/spec$/);
});

test("shows a retryable Specs error and recovers without stale content", async ({ page }) => {
  let fail = true;
  await page.route("**/api/v1/ontology/types/Spec", async (route) => {
    if (fail) {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: "temporary failure" }),
      });

      return;
    }

    await route.continue();
  });

  await page.goto("/notes");
  await page.getByRole("button", { name: "Expand Other" }).click();
  await page.getByRole("button", { name: /^Spec \(1\)/ }).click();
  // The Specs table lists its records, so the rail's list starts collapsed.
  await expandNoteList(page, "Spec notes");

  const error = page.locator(".ontology-list__empty[role=alert]");
  await expect(error).toContainText("temporary failure");
  fail = false;
  await error.getByRole("button", { name: "Retry" }).click();
  await expect(
    page.getByRole("region", { name: "Note list" }).getByText("Search Rewrite"),
  ).toBeVisible();
});

test("renders a successful empty Specs list distinctly from loading", async ({ page }) => {
  await page.route("**/api/v1/ontology/types/Spec", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    await route.fulfill({
      response,
      json: { ...body, count: 0, notes: [] },
    });
  });

  await page.goto("/notes");
  await page.getByRole("button", { name: "Expand Other" }).click();
  await page.getByRole("button", { name: /^Spec \(1\)/ }).click();
  // The Specs table lists its records, so the rail's list starts collapsed.
  await expandNoteList(page, "Spec notes");

  await expect(page.getByText("Loading type…")).toHaveCount(0);
  await expect(page.getByText("No notes match the current filter.")).toBeVisible();
});

test("does not revive a collapsed specs folder after a delayed read", async ({ page }) => {
  await page.route("**/api/v1/files/tree?path=notes%2Fspecs&**", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 250));
    await route.continue();
  });

  await page.goto("/explorer");
  const rail = page.locator(".explorer-rail");
  await rail.getByRole("button", { name: /notes/i }).click();
  const specs = rail.getByRole("button", { name: /specs/i });
  await specs.click();
  await specs.click();
  await page.waitForTimeout(350);

  await expect(rail.getByRole("button", { name: /search-rewrite\.md/i })).toHaveCount(0);

  await specs.click();
  await expect(rail.getByRole("button", { name: /search-rewrite\.md/i })).toBeVisible();
});

test("root path redirects to notes workspace", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/notes$/);
  await expect(
    page
      .getByRole("tablist", { name: "Open notes" })
      .getByRole("tab", { name: "Home", exact: true }),
  ).toBeVisible();
});

test("opens the ontology atlas at the ontology route", async ({ page }) => {
  await page.goto("/ontology");
  await expect(page).toHaveURL(/\/ontology$/);
  await expect(page.getByRole("heading", { name: "Ontology Atlas" })).toBeVisible();
});

test("edits configured view enum checkbox and relation cells", async ({ page }) => {
  await page.goto("/notes");

  await page.getByRole("button", { name: "Action Items Editing Fixture", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  const response = await page.request.post("/api/v1/views/e2e.action-items/execute", { data: {} });
  expect(response.ok()).toBeTruthy();
  const body = await response.json();
  const capabilities = body.capabilities ?? [];
  expect(capabilities.find((cap: { key: string }) => cap.key === "status")?.edit).toMatchObject({
    kind: "enum",
    options: ["open", "blocked", "done"],
  });
  expect(capabilities.find((cap: { key: string }) => cap.key === "done")?.edit).toMatchObject({
    kind: "boolean",
  });
  expect(capabilities.find((cap: { key: string }) => cap.key === "assignedTo")?.edit).toMatchObject(
    {
      kind: "node",
      targetType: "Person",
    },
  );

  const row = page.locator(".configured-view__table tbody tr").nth(1);
  await expect(row).toBeVisible();
  await expect(row).toContainText(/open/i);
  await expect(row).toContainText("Alice", { timeout: 15_000 });

  await row.getByRole("button", { name: "Edit Status" }).click();
  await row.getByRole("combobox", { name: "Edit Status" }).selectOption("blocked");
  await expect(row.getByRole("button", { name: "Edit Status" })).toContainText(/blocked/i);

  await row.getByLabel("Edit Done").check();
  await expect(row.getByLabel("Edit Done")).toBeChecked();

  // Relations read as links until the edit control opens the picker.
  await row.getByRole("button", { name: "Edit Assigned To" }).click();
  const assignee = row.getByRole("combobox", { name: "Edit Assigned To" });
  const bobOption = assignee.locator("option", { hasText: "Bob" });
  await expect(bobOption).toHaveCount(1);
  const bobValue = await bobOption.getAttribute("value");
  expect(bobValue).toBeTruthy();
  await assignee.selectOption({ value: bobValue! });
  await expect(row.getByRole("link", { name: "Bob" })).toBeVisible();

  await expect(page.locator(".ontology-edit-bar")).toContainText("Ready to save");
  await expect(page.locator(".ontology-edit-bar")).toContainText("1 note");
  await expect(page.locator(".ontology-edit-bar")).toContainText("3 changes");

  const commitResponsePromise = page.waitForResponse(
    (commitResponse) =>
      commitResponse.url().includes("/api/v1/edit-sessions/") &&
      commitResponse.url().endsWith("/commit"),
  );

  await page.getByRole("button", { name: "Save", exact: true }).click();
  const commitResponse = await commitResponsePromise;
  expect(commitResponse.ok()).toBeTruthy();
  await expect(page.locator(".ontology-edit-bar")).toHaveCount(0);

  const committedResponse = await page.request.post("/api/v1/views/e2e.action-items/execute", {
    data: {},
  });

  expect(committedResponse.ok()).toBeTruthy();
  const committed = await committedResponse.json();

  const updatedRow = committed.rows.find(
    (item: { fields?: { status?: unknown } }) =>
      Array.isArray(item.fields?.status) && item.fields.status.includes("blocked"),
  );

  expect(updatedRow?.fields.status).toEqual(["blocked"]);
  expect(updatedRow?.fields.done).toBe("true");
  expect(updatedRow?.fields.assignedTo).toEqual([bobValue]);

  await page.reload();
  await expect(page.getByRole("heading", { name: "Action Items Editing Fixture" })).toBeVisible();

  const persistedRow = page
    .locator(".configured-view__table tbody tr")
    .filter({ hasText: "Follow up on dashboard rollout" });

  await expect(persistedRow.getByRole("button", { name: "Edit Status" })).toContainText(/blocked/i);
  await expect(persistedRow.getByLabel("Edit Done")).toBeChecked();
  await expect(persistedRow.getByRole("link", { name: "Bob" })).toBeVisible();
});

test("edits and restores Atomic narrative markdown", async ({ context, page }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(
    page.getByRole("article").getByRole("heading", { name: "Search Rewrite" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Edit", exact: true }).click();

  // The Requirements section edits in place once expanded; no drill-in.
  const notePane = page.getByRole("article").first();
  const requirements = notePane.locator('details.body-disclosure[data-field="requirements"]');
  await requirements.locator("> summary").click();
  await expect(requirements).toHaveAttribute("open");

  const editor = requirements.locator(".body-narrative--editor").first().getByRole("textbox", {
    name: "Narrative markdown",
  });

  await expect(editor).toBeVisible();
  await editor.click();
  await editor.press("End");
  await page.evaluate(() => navigator.clipboard.writeText(" Ship ready."));
  await editor.press("ControlOrMeta+V");
  await expect(editor).toContainText("Ship ready.");
  await editor.press("ControlOrMeta+A");
  await editor.press("ControlOrMeta+C");
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("Ship ready.");
  await editor.press("End");

  await page.setViewportSize({ width: 390, height: 844 });
  await expect(editor).toBeVisible();
  const editorBox = await editor.boundingBox();
  expect(editorBox).not.toBeNull();
  expect((editorBox?.x || 0) + (editorBox?.width || 0)).toBeLessThanOrEqual(390);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);

  const save = page.getByRole("button", { name: "Save", exact: true });
  await expect(save).toBeEnabled();

  const commitResponsePromise = page.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/edit-sessions/") && response.url().endsWith("/commit"),
  );

  await save.click();
  expect((await commitResponsePromise).ok()).toBeTruthy();

  await expect
    .poll(async () => {
      const response = await page.request.get(
        "/api/v1/files/view?path=notes%2Fspecs%2Fsearch-rewrite.md",
      );

      const file = await response.json();

      return file.content;
    })
    .toContain("Ship ready.");
});

test("edits and saves the whole file in Atomic Markdown mode", async ({ page }) => {
  const notePath = "notes/specs/search-rewrite.md";
  const fileURL = `/api/v1/files/view?path=${encodeURIComponent(notePath)}`;
  const beforeResponse = await page.request.get(fileURL);
  expect(beforeResponse.ok()).toBeTruthy();
  const before = await beforeResponse.json();
  const marker = "Phase B whole-file source edit.";

  await page.goto(`/notes?note=${encodeURIComponent(notePath)}`);
  const notePane = page.getByRole("article").first();
  await expect(notePane.getByRole("heading", { name: "Search Rewrite" })).toBeVisible();
  await chooseView(notePane, "Markdown");
  await page.getByRole("button", { name: "Edit", exact: true }).click();

  const editor = notePane.getByRole("textbox", { name: "Markdown source" });
  await expect(editor).toBeVisible();
  await editor.click({ position: { x: 4, y: 4 } });
  await editor.press("ControlOrMeta+End");
  await editor.press("Enter");
  await editor.type(marker);

  const commitResponsePromise = page.waitForResponse(
    (response) =>
      response.url().includes("/api/v1/edit-sessions/") && response.url().endsWith("/commit"),
  );

  await page.getByRole("button", { name: "Save", exact: true }).click();
  expect((await commitResponsePromise).ok()).toBeTruthy();

  await expect
    .poll(async () => {
      const response = await page.request.get(fileURL);
      const file = await response.json();

      return file.content;
    })
    .toContain(marker);
  const afterResponse = await page.request.get(fileURL);
  const after = await afterResponse.json();
  expect(after.content).toBe(`${before.content}\n${marker}`);
});
