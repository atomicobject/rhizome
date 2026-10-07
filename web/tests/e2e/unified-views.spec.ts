import { expect, test, type Page } from "./fixtures";
import { chooseView, expectView, workspaceView } from "./workspaceView";

const effortPath = "notes/view-demo/effort-a.md";

const betaPath = "notes/view-demo/effort-b.md";

const customFrame = (page: Page) => page.frameLocator("iframe.custom-view-frame");

async function openEffortCollection(page: Page) {
  const collections = page.getByRole("region", { name: "Collections" });
  await expect(collections.getByRole("button", { name: "E2E Views", exact: true })).toBeVisible();
  const expand = collections.getByRole("button", { name: "Expand E2E Views", exact: true });

  if (await expand.count()) await expand.click();

  await page
    .getByRole("region", { name: "Collections" })
    .getByRole("button", { name: "View demo efforts (2)", exact: true })
    .click();
  await expect(customFrame(page).getByRole("heading", { name: "Demo dashboard" })).toBeVisible();
}

test("configured collection default shares native rows and retains every built-in layout", async ({
  page,
}) => {
  await page.goto("/notes");
  await openEffortCollection(page);
  const frame = customFrame(page);
  await expect(frame.getByLabel("View context")).toHaveText(
    JSON.stringify({ kind: "type", type: "ViewDemoEffort" }),
  );
  await expect(frame.getByText("collection configuration")).toBeVisible();
  await expect(
    frame.getByRole("list", { name: "Native effort rows" }).getByRole("listitem"),
  ).toHaveCount(2);
  await expect(frame.getByLabel("Native capabilities")).toContainText('"key":"status"');
  await expect(
    frame.getByRole("button", { name: "Open View demo Alpha", exact: true }),
  ).toBeVisible();

  // The type Briefing takes Overview's place for a type (SPEC-0112).
  await expect(
    workspaceView(page).getByRole("button", { name: "Overview", exact: true }),
  ).toHaveCount(0);
  await chooseView(page, "Briefing");
  await expect(frame.getByRole("region", { name: "Needs attention" })).toBeVisible();
  await expect(frame.getByLabel("View context")).toHaveCount(0);
  await chooseView(page, "Table");
  await expect(page.locator(".configured-view__table tbody tr[data-row-key]")).toHaveCount(2);
  await chooseView(page, "Demo native · Table");
  await expect(page.locator(".configured-view__table tbody tr[data-row-key]")).toHaveCount(2);
  await chooseView(page, "Demo native · Cards");
  await expect(page.locator(".configured-card")).toHaveCount(2);
  await chooseView(page, "Demo native · Board");
  await expect(page.locator(".configured-view__board")).toBeVisible();

  await page.reload();
  await expect(page.locator(".configured-view__board")).toBeVisible();
  await chooseView(page, "Demo dashboard");
  await expect(customFrame(page).getByRole("heading", { name: "Demo dashboard" })).toBeVisible();
  await page.reload();
  await expect(customFrame(page).getByRole("heading", { name: "Demo dashboard" })).toBeVisible();
});

test("group specificity, wildcard context, fallback and native variant default work through navigation", async ({
  page,
}, testInfo) => {
  await page.goto("/notes");
  const collections = page.getByRole("region", { name: "Collections" });
  await collections.getByRole("button", { name: "E2E Views", exact: true }).click();
  await expect(
    customFrame(page).getByRole("heading", { name: "Exact group dashboard" }),
  ).toBeVisible();
  await expect(customFrame(page).getByLabel("View context")).toHaveText(
    JSON.stringify({ kind: "group", group: "E2E Views" }),
  );
  // A group workspace selects its rail row, hides the note list, and is not left loading.
  await expect(collections.getByRole("button", { name: "E2E Views", exact: true })).toHaveClass(
    /is-selected/,
  );
  await expect(page.getByRole("region", { name: "Note list", exact: true })).toHaveCount(0);
  await expect(page.getByText(/^Loading (notes|views)…$/)).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("unified-group.png"), animations: "disabled" });
  await chooseView(page, "Generic group dashboard");
  await expect(
    customFrame(page).getByRole("heading", { name: "Generic group dashboard" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    customFrame(page).getByRole("heading", { name: "Generic group dashboard" }),
  ).toBeVisible();
  await chooseView(page, "Exact group dashboard");
  await expect(
    customFrame(page).getByRole("heading", { name: "Exact group dashboard" }),
  ).toBeVisible();
  await chooseView(page, "Overview");
  await expect(customFrame(page).getByRole("region", { name: "Members" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "E2E Views", exact: true })).toBeVisible();

  // Without an exact default, a one-root group opens the bundled Sections view;
  // the wildcard dashboard is still offered there with that group's context.
  await collections.getByRole("button", { name: "E2E Other", exact: true }).click();
  await expectView(page, "Sections");
  await chooseView(page, "Generic group dashboard");
  await expect(
    customFrame(page).getByRole("heading", { name: "Generic group dashboard" }),
  ).toBeVisible();
  await expect(customFrame(page).getByLabel("View context")).toHaveText(
    JSON.stringify({ kind: "group", group: "E2E Other" }),
  );
  await expect(
    workspaceView(page).getByRole("button", { name: "Exact group dashboard", exact: true }),
  ).toHaveCount(0);
  await collections.getByRole("button", { name: "Expand E2E Other", exact: true }).click();
  await collections.getByRole("button", { name: "Other demos (1)", exact: true }).click();
  await expect(page.locator(".configured-card")).toHaveCount(1);
  await expect(page.locator(".configured-card")).toContainText("Other view demo");
});

test("two nodes using one definition keep separate preferences and canonical tab identities", async ({
  page,
}) => {
  await page.goto(`/notes?note=${encodeURIComponent(effortPath)}`);
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  await chooseView(page, "Markdown");
  await expectView(page, "Markdown");

  await openEffortCollection(page);
  await customFrame(page)
    .getByRole("button", { name: "Beside View demo Beta", exact: true })
    .click();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  await expect(customFrame(page).getByRole("heading", { name: "Demo dashboard" })).toBeVisible();
  await tabs.getByRole("tab", { name: /^effort-b/ }).click();
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Beta", exact: true }),
  ).toBeVisible();
  const betaContext = JSON.parse(await customFrame(page).getByLabel("View context").innerText());
  expect(betaContext).toMatchObject({
    kind: "node",
    type: "ViewDemoEffort",
    ref: { notePath: betaPath },
  });
  await expect(tabs.getByRole("tab", { name: /^View demo Alpha/ })).toHaveCount(1);
  await expect(tabs.getByRole("tab", { name: /^View demo Beta/ })).toHaveCount(1);
  await tabs.getByRole("tab", { name: /^View demo Alpha/ }).click();
  await expectView(page, "Markdown");

  // Explicit dashboard navigation overrides Alpha's remembered source choice.
  await openEffortCollection(page);
  await customFrame(page)
    .getByRole("button", { name: "Open View demo Alpha", exact: true })
    .click();
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  const alphaContext = JSON.parse(await customFrame(page).getByLabel("View context").innerText());
  expect(alphaContext.ref.notePath).toBe(effortPath);
  expect(alphaContext.ref.notePath).not.toBe(betaContext.ref.notePath);
});

test("dashboard opens an embedded node with its canonical ref and selected presentation", async ({
  page,
}) => {
  const response = await page.request.post("/api/v1/views/e2e.views.tasks/execute", { data: {} });
  expect(response.ok()).toBeTruthy();
  const { rows } = await response.json();
  const task = rows.find((row: { title: string }) => row.title === "Alpha embedded task");
  expect(task.ref).toMatchObject({ notePath: effortPath, typeName: "ViewDemoTask" });
  expect(task.ref.nodeId).toBeTruthy();
  expect(task.ref.fragment).toBeTruthy();
  await page.goto("/notes");
  await openEffortCollection(page);
  await customFrame(page)
    .getByRole("button", { name: "Open Alpha embedded task", exact: true })
    .click();
  await expect(
    customFrame(page).getByRole("heading", { name: "Alpha embedded task", exact: true }),
  ).toBeVisible();
  const context = JSON.parse(await customFrame(page).getByLabel("View context").innerText());
  expect(context).toMatchObject({
    kind: "node",
    type: "ViewDemoTask",
    ref: {
      notePath: task.ref.notePath,
      fragment: task.ref.fragment,
      nodeId: task.ref.nodeId,
      typeName: task.ref.typeName,
      kind: task.ref.kind,
      structuralFingerprint: task.ref.structuralFingerprint,
    },
  });
  const url = new URL(page.url());
  expect(url.searchParams.get("note")).toBe(effortPath);
  expect(url.searchParams.get("nodeId")).toBe(task.ref.nodeId);
  expect(url.searchParams.get("structural")).toBe(task.ref.structuralFingerprint);
  expect(url.searchParams.get("presentation")).toBe("e2e.views.task-node");
  expect(decodeURIComponent(url.hash.slice(1))).toBe(task.ref.fragment);
  await chooseView(page, "Structure");
  await expect(page.locator("iframe.custom-view-frame")).toHaveCount(0);
  await chooseView(page, "Demo task workspace");
  await expect(
    customFrame(page).getByRole("heading", { name: "Alpha embedded task", exact: true }),
  ).toBeVisible();
});

test("staged custom writes survive native switches and remain separate between nodes", async ({
  page,
}) => {
  await page.goto(`/notes?note=${encodeURIComponent(effortPath)}`);
  await expect(customFrame(page).getByLabel("Node status")).toContainText("ready");
  await customFrame(page).getByRole("button", { name: "Stage active" }).click();
  await expect(customFrame(page).getByLabel("Node status")).toContainText("active");
  await expect(page.getByRole("button", { name: "Changes 1" })).toBeVisible();
  await chooseView(page, "Structure");
  await expect(page.getByRole("button", { name: "Changes 1" })).toBeVisible();
  await chooseView(page, "Markdown");
  await expect(page.getByRole("button", { name: "Changes 1" })).toBeVisible();
  await chooseView(page, "Demo node workspace");
  await expect(customFrame(page).getByLabel("Node status")).toContainText("active");

  await openEffortCollection(page);
  await customFrame(page)
    .getByRole("button", { name: "Beside View demo Beta", exact: true })
    .click();
  await page
    .getByRole("tablist", { name: "Open notes" })
    .getByRole("tab", { name: /^effort-b/ })
    .click();
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Beta", exact: true }),
  ).toBeVisible();
  await expect(customFrame(page).getByLabel("Node status")).toContainText("ready");
  await page
    .getByRole("tablist", { name: "Open notes" })
    .getByRole("tab", { name: /^View demo Alpha/ })
    .click();
  await expect(customFrame(page).getByLabel("Node status")).toContainText("active");
  await expect(page.getByRole("button", { name: "Changes 1" })).toBeVisible();

  // Switching never commits. A committed read still sees the original value.
  const response = await page.request.post("/api/v1/views/e2e.views.efforts/execute", { data: {} });
  const { rows } = await response.json();
  expect(
    rows.find((row: { ref: { notePath: string } }) => row.ref.notePath === effortPath).fields
      .status,
  ).toContain("ready");
});

test("same-file root and embedded presentations keep distinct tabs through activation and reload", async ({
  page,
}, testInfo) => {
  await page.goto(`/notes?note=${encodeURIComponent(effortPath)}`);
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  const tabs = page.getByRole("tablist", { name: "Open notes" });
  const rootTab = tabs.getByRole("tab").nth(1);
  await expect(tabs.getByRole("tab")).toHaveCount(2);
  const rootId = await rootTab.getAttribute("data-tab-id");
  const rootContext = JSON.parse(await customFrame(page).getByLabel("View context").innerText());
  expect(rootContext.ref.kind).toBe("NOTE");
  await page.screenshot({
    path: testInfo.outputPath("unified-root-node.png"),
    animations: "disabled",
  });

  await customFrame(page).getByRole("button", { name: "Open embedded task beside" }).click();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(rootTab).toHaveAttribute("aria-selected", "true");
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  expect(JSON.parse(await customFrame(page).getByLabel("View context").innerText())).toEqual(
    rootContext,
  );

  const embeddedTab = tabs.getByRole("tab").nth(2);
  expect(await embeddedTab.getAttribute("data-tab-id")).not.toBe(rootId);
  await embeddedTab.click();
  await expect(
    customFrame(page).getByRole("heading", { name: "Alpha embedded task", exact: true }),
  ).toBeVisible();
  await expect(embeddedTab).toHaveAccessibleName(/^Alpha embedded task/);
  await expect(rootTab).toHaveAccessibleName(/^View demo Alpha/);

  const embeddedContext = JSON.parse(
    await customFrame(page).getByLabel("View context").innerText(),
  );

  expect(embeddedContext).toMatchObject({
    kind: "node",
    type: "ViewDemoTask",
    ref: { notePath: effortPath, kind: "EMBEDDED", fragment: "^view-task-alpha" },
  });
  await page.screenshot({
    path: testInfo.outputPath("unified-embedded-node.png"),
    animations: "disabled",
  });

  await rootTab.click();
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  await page.reload();
  await expect(tabs.getByRole("tab")).toHaveCount(3);
  await expect(rootTab).toHaveAttribute("aria-selected", "true");
  await expect(
    customFrame(page).getByRole("heading", { name: "View demo Alpha", exact: true }),
  ).toBeVisible();
  await embeddedTab.click();
  await expect(
    customFrame(page).getByRole("heading", { name: "Alpha embedded task", exact: true }),
  ).toBeVisible();
  await expect(embeddedTab).toHaveAccessibleName(/^Alpha embedded task/);
  expect(JSON.parse(await customFrame(page).getByLabel("View context").innerText())).toEqual(
    embeddedContext,
  );
});

test("standalone HTML and TSX launches receive the same validated context and configuration", async ({
  page,
}) => {
  const group = { kind: "group", group: "E2E Views" };
  await page.goto(
    `/views/e2e.views.group?${new URLSearchParams({ context: JSON.stringify(group) })}`,
  );
  await expect(page.getByRole("heading", { name: "Exact group dashboard" })).toBeVisible();
  await expect(page.getByLabel("View context")).toHaveText(JSON.stringify(group));
  await expect(page.getByLabel("View configuration")).toContainText("exact configuration");

  await page.goto("/views/e2e.views.standalone");
  await expect(page.getByRole("heading", { name: "Standalone demo" })).toBeVisible();
  await expect(page.getByLabel("View context")).toHaveText('{"kind":"standalone"}');
  await expect(page.getByLabel("View configuration")).toContainText("standalone configuration");

  const response = await page.request.post("/api/v1/views/e2e.views.efforts/execute", { data: {} });
  const { rows } = await response.json();
  const ref = rows.find((row: { ref: { notePath: string } }) => row.ref.notePath === betaPath).ref;
  const node = { kind: "node", type: "ViewDemoEffort", ref };
  await page.goto(
    `/views/e2e.views.node?${new URLSearchParams({ context: JSON.stringify(node) })}`,
  );
  await expect(page.getByRole("heading", { name: "View demo Beta", exact: true })).toBeVisible();
  expect(JSON.parse(await page.getByLabel("View context").innerText())).toMatchObject(node);
  await expect(page.getByText("node configuration")).toBeVisible();
});
