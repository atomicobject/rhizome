import type { PublicNodeDetailData } from "../../src/api/publicGraphQLTypes";
import { isJsonObject, isString } from "../../src/api/parse";
import { expect, test, type Page } from "./fixtures";

const notePath = (index: number) => `memory-fixture/note-${index}.md`;

function noteReply(index: number, lateImage: boolean): PublicNodeDetailData {
  const path = notePath(index);
  const title = `Memory note ${index}`;

  return {
    node: {
      ref: { ref: path, notePath: path, kind: "NOTE" },
      nodeId: `note:${path}`,
      nodeKind: "NOTE",
      path,
      title,
      content: `# ${title}\n\n${lateImage && index === 0 ? "![Layout fixture](/memory-fixture/late.svg)\n\n" : ""}${Array.from(
        { length: 40 },
        (_, section) =>
          `## Section ${section}\n\nA paragraph with **formatting** and enough text to give this note a reading position.\n\n- First item\n- Second item\n`,
      ).join("\n")}`,
      locator: { sourceLocator: path, status: "linkable", exists: true, requiresFix: false },
      workspace: {
        fields: [],
        collections: [],
        bodies: [],
        relationGroups: [],
        structure: [],
        capabilities: {
          canEdit: false,
          canEditFields: false,
          canEditCollections: false,
          canNavigateChildren: true,
          canSubscribe: true,
        },
        status: {
          dirty: false,
          validation: { issueCount: 1 },
          freshness: {},
          session: {},
          hasWarnings: false,
        },
        version: "memory-fixture",
        loaded: { rendered: true, assessment: true, structure: false, relations: false },
        assessment: {
          notePath: path,
          candidateTypes: [],
          issues: [{ code: "fixture", message: "A visible fixture issue" }],
          fields: [],
          relations: [],
        },
      },
    },
  };
}

async function installNotes(page: Page, lateImage = false) {
  const reads = new Map<string, number>();
  await page.route("**/api/v1/graphql", async (route) => {
    const body: unknown = JSON.parse(route.request().postData() || "{}");

    if (!isJsonObject(body) || !isJsonObject(body.variables)) return route.continue();
    const ref = body.variables.ref;

    if (!isString(ref) || !ref.startsWith("memory-fixture/")) return route.continue();
    const index = Number(ref.match(/note-(\d+)\.md/)?.[1]);

    if (!Number.isSafeInteger(index)) throw new Error(`Unexpected memory fixture ref: ${ref}`);
    reads.set(ref, (reads.get(ref) || 0) + 1);
    await route.fulfill({ json: { data: noteReply(index, lateImage) } });
  });

  return reads;
}

async function openNote(page: Page, index: number) {
  await page.evaluate((path) => {
    window.history.pushState({}, "", `/notes?note=${encodeURIComponent(path)}`);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, notePath(index));
  await expect(page.locator(".notes-shell__panel.is-active")).toContainText("Section 39");
}

test("releases older note bodies and restores reading state without another note query", async ({
  page,
}) => {
  let releaseImage = () => {};

  const imageGate = new Promise<void>((resolve) => {
    releaseImage = resolve;
  });

  let imageArrived = () => {};

  const secondImage = new Promise<void>((resolve) => {
    imageArrived = resolve;
  });

  await page.route("**/memory-fixture/late.svg*", async (route) => {
    const delayed = new URL(route.request().url()).searchParams.has("late");

    if (delayed) {
      imageArrived();
      await imageGate;
    }

    await route.fulfill({
      contentType: "image/svg+xml",
      headers: { "Cache-Control": "no-store" },
      body: `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="${delayed ? 360 : 100}"><rect width="120" height="100%" fill="gray"/></svg>`,
    });
  });
  const reads = await installNotes(page, true);
  await page.goto(`/notes?note=${encodeURIComponent(notePath(0))}`);
  await page.addStyleTag({ content: 'img[alt="Layout fixture"] { width: 120px; height: 100px; }' });
  const original = page.locator('[id="panel-note:memory-fixture/note-0.md"]');
  const body = original.locator(".ontology-pane__main");
  const issues = original.locator("details.ontology-issues");
  await expect(original).toContainText("Section 39");
  await expect
    .poll(() =>
      original
        .getByRole("img")
        .evaluate((image) => image instanceof HTMLImageElement && image.complete),
    )
    .toBe(true);
  await issues.locator("summary").click();
  await expect(issues).toHaveAttribute("open", "");

  const before = await body.evaluate((element) => {
    element.scrollTop = 1_700;

    return element.scrollTop;
  });

  expect(before).toBeGreaterThan(1_000);
  const readingAnchor = body.getByRole("heading", { name: "Section 10", exact: true });

  const anchorOffset = await readingAnchor.evaluate(
    (element) => element.getBoundingClientRect().top,
  );

  for (let index = 1; index < 8; index++) await openNote(page, index);

  await expect(page.getByRole("tablist", { name: "Open notes" }).getByRole("tab")).toHaveCount(9);
  await expect(original).not.toContainText("Section 39");
  await expect(page.locator('.ontology-pane__main[data-note-body="resident"]')).toHaveCount(5);
  const beforeReturn = reads.get(notePath(0));
  await page.getByRole("tab", { name: "Memory note 0", exact: true }).click();
  await expect(original).toContainText("Section 39");
  await expect(issues).toHaveAttribute("open", "");
  await original.getByRole("img").evaluate((image) => {
    if (!(image instanceof HTMLImageElement)) throw new Error("Expected the layout fixture image");
    image.addEventListener("load", () => (image.style.height = "360px"), { once: true });
    image.src = "/memory-fixture/late.svg?late";
  });
  await secondImage;
  releaseImage();
  await expect
    .poll(() =>
      original
        .getByRole("img")
        .evaluate((image) => image instanceof HTMLImageElement && image.complete),
    )
    .toBe(true);
  await expect
    .poll(() => readingAnchor.evaluate((element) => element.getBoundingClientRect().top))
    .toBeCloseTo(anchorOffset, 0);
  expect(reads.get(notePath(0))).toBe(beforeReturn);
  await expect(page.locator('.ontology-pane__main[data-note-body="resident"]')).toHaveCount(5);
});

test("fresh fragment navigation selects the requested node after its old body was released", async ({
  page,
}) => {
  await installNotes(page);
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText("Search Rewrite");

  for (let index = 0; index < 8; index++) await openNote(page, index);

  await page.evaluate(() => {
    window.history.pushState({}, "", "/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#%5Estory-001");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await expect(page.getByRole("navigation", { name: "Node path" })).toContainText("Search Rewrite");
});

test("keeps collection filters when an older note body is restored", async ({ page }) => {
  await installNotes(page);
  const reply = noteReply(0, false);
  const node = reply.node!;

  const children = ["Alpha", "Beta", "Gamma", "Delta"].map((title, index) => ({
    ref: {
      ref: `${notePath(0)}#item-${index}`,
      nodeId: `${notePath(0)}#item-${index}`,
      notePath: notePath(0),
      fragment: `item-${index}`,
      kind: "EMBEDDED",
    },
    title,
    locator: "EMBEDDED",
    fields: [],
    collections: [],
    blocks: [],
  }));

  node.workspace!.bodies = [
    {
      ref: node.ref,
      title: node.title,
      locator: "FILE",
      fields: [],
      collections: [],
      blocks: [
        {
          kind: "collection",
          fieldName: "items",
          range: { start: 0, end: 1 },
          childRefs: children.map((child) => child.ref),
        },
      ],
    },
    ...children,
  ];
  await page.route("**/api/v1/graphql", async (route) => {
    const body: unknown = JSON.parse(route.request().postData() || "{}");

    if (
      !isJsonObject(body) ||
      !isJsonObject(body.variables) ||
      body.variables.ref !== notePath(0)
    ) {
      return route.fallback();
    }

    await route.fulfill({ json: { data: reply } });
  });
  await page.goto(`/notes?note=${encodeURIComponent(notePath(0))}`);
  const filter = page.getByRole("searchbox", { name: "Filter items" });
  await filter.fill("Beta");
  await expect(page.getByRole("button", { name: "Beta", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Alpha", exact: true })).toHaveCount(0);

  for (let index = 1; index < 8; index++) await openNote(page, index);
  await expect(
    page.locator('[id="panel-note:memory-fixture/note-0.md"] .body-collection'),
  ).toHaveCount(0);
  await page.getByRole("tab", { name: "Memory note 0", exact: true }).click();
  await expect(filter).toHaveValue("Beta");
  await expect(page.getByRole("button", { name: "Beta", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Alpha", exact: true })).toHaveCount(0);
});

test("keeps an active editor and its undo history when more than five other notes are visited", async ({
  page,
}) => {
  await installNotes(page);
  await page.goto("/notes?note=notes%2Fspecs%2Fsearch-rewrite.md#%5Estory-001");
  await expect(page.locator(".ontology-identity__title:visible")).toHaveText(
    "Stable typed retrieval",
  );
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const editor = page.getByRole("textbox", { name: "Narrative markdown" });
  await expect(editor).toBeVisible();
  const originalText = (await editor.locator(".cm-line").allTextContents()).join("\n");
  await editor.click();
  await editor.press("ControlOrMeta+End");
  await page.keyboard.insertText(" Memory preservation check.");
  const editorNode = await editor.elementHandle();

  try {
    for (let index = 0; index < 8; index++) await openNote(page, index);

    expect(await editorNode?.evaluate((element) => element.isConnected)).toBe(true);
    await page.locator('[id="tab-note:notes/specs/search-rewrite.md"]').click();
    await expect(editor).toContainText("Memory preservation check.");
    await editor.press("ControlOrMeta+Z");
    await expect
      .poll(async () => (await editor.locator(".cm-line").allTextContents()).join("\n"))
      .toBe(originalText);
  } finally {
    page.on("dialog", (dialog) => dialog.accept());
    const discard = page.getByRole("button", { name: "Discard", exact: true });

    if (await discard.isVisible()) await discard.click();
  }
});
