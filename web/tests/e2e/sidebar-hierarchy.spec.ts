import { expect, test } from "./fixtures";

test("indents each schema hierarchy level and lets the active branch collapse", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/ontology/summary", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        schemaPresent: true,
        totalNotes: 1,
        typedNotes: 1,
        untypedNotes: 0,
        ambiguousNotes: 0,
        issueNotes: 0,
        types: [
          {
            name: "EffortWorkspace",
            pluralLabel: "Effort workspaces",
            displayParent: "Effort",
            count: 1,
            role: "note",
          },
          {
            name: "Material",
            pluralLabel: "Materials",
            displayParent: "EffortWorkspace",
            count: 1,
            role: "note",
          },
        ],
        interfaces: [
          {
            name: "Effort",
            pluralLabel: "Efforts",
            displayGroup: "Delivery",
            count: 1,
            issueCount: 0,
            implementors: ["EffortWorkspace"],
          },
        ],
      }),
    });
  });
  await page.route("**/api/v1/ontology/types/Material", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        count: 1,
        issueCount: 0,
        type: { name: "Material", fields: [] },
        notes: [],
      }),
    });
  });

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/notes/material");
  const collections = page.getByRole("region", { name: "Collections" });
  const efforts = collections.getByRole("button", { name: "Efforts (1)" });
  const workspaces = collections.getByRole("button", { name: "Effort workspaces (1)" });
  const materials = collections.getByRole("button", { name: "Materials (1)" });
  await expect(materials).toBeVisible();

  const [effortBox, workspaceBox, materialBox] = await Promise.all([
    efforts.boundingBox(),
    workspaces.boundingBox(),
    materials.boundingBox(),
  ]);

  if (!effortBox || !workspaceBox || !materialBox) {
    throw new Error("Expected visible hierarchy rows to have browser layout boxes");
  }

  expect(workspaceBox.x).toBeGreaterThan(effortBox.x);
  expect(materialBox.x).toBeGreaterThan(workspaceBox.x);
  await page.screenshot({ path: testInfo.outputPath("sidebar-hierarchy-expanded.png") });

  await collections.getByRole("button", { name: "Collapse Delivery" }).click();
  await expect(materials).toHaveCount(0);
  await expect(collections.getByRole("button", { name: "Expand Delivery" })).toHaveAttribute(
    "aria-expanded",
    "false",
  );
});
