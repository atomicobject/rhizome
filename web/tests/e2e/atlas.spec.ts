import { expect, test, type Page } from "./fixtures";

async function expectAtlasGeometry(page: Page) {
  const measurements = await page
    .locator(".react-flow__node-ontologyType")
    .evaluateAll((elements) =>
      elements.map((element) => {
        if (!(element instanceof HTMLElement)) throw new Error("Expected a graph node");
        const card = element.querySelector<HTMLElement>(".ontology-node");

        if (!card) throw new Error("Expected a rendered type card");
        const cardRect = card.getBoundingClientRect();
        const scale = cardRect.width / card.offsetWidth;

        return {
          id: element.dataset.id,
          width: card.offsetWidth,
          height: card.offsetHeight,
          declaredWidth: Number.parseFloat(element.style.width),
          declaredHeight: Number.parseFloat(element.style.height),
          // Relation handles leave from the card's middle; the compact card lists no field rows.
          handles: [...card.querySelectorAll<HTMLElement>('[data-handleid^="field-"]')].map(
            (handle) => {
              const handleRect = handle.getBoundingClientRect();

              return {
                name: handle.dataset.handleid ?? "",
                midpointError: Math.abs(
                  (handleRect.top + handleRect.height / 2 - cardRect.top - cardRect.height / 2) /
                    scale,
                ),
                overflow: (handleRect.top + handleRect.height / 2 - cardRect.bottom) / scale,
              };
            },
          ),
        };
      }),
    );

  expect(measurements.length).toBeGreaterThan(5);
  expect(measurements.flatMap((node) => node.handles).length).toBeGreaterThan(4);

  for (const node of measurements) {
    expect(node.width, `${node.id} card width`).toBe(node.declaredWidth);
    expect(node.height, `${node.id} card height`).toBe(node.declaredHeight);

    for (const handle of node.handles) {
      expect(handle.midpointError, `${node.id}.${handle.name} middle alignment`).toBeLessThan(0.6);
      expect(handle.overflow, `${node.id}.${handle.name} stays inside card`).toBeLessThanOrEqual(0);
    }
  }
}

test("Atlas finder focuses types, searches without removing graph nodes, and opens type cards", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/ontology");
  const index = page.getByRole("search", { name: "Atlas types" });
  const search = index.getByRole("searchbox", { name: "Find a type" });
  const person = page.locator('.react-flow__node[data-id="Person"]');
  const graphNodes = page.locator(".react-flow__node-ontologyType");

  const nodeIds = () =>
    graphNodes.evaluateAll((nodes) => nodes.map((node) => node.getAttribute("data-id")).sort());

  await expect(person).toBeVisible();
  await search.fill("person");
  await expect(index.getByRole("button", { name: "Focus Person", exact: true })).toBeVisible();
  const originalIds = await nodeIds();
  expect(originalIds).toContain("Spec");
  await index.getByRole("button", { name: "Focus Person", exact: true }).click();
  await expect(index.getByText("Tracing Person")).toBeVisible();
  await expect(person).toHaveClass(/atlas-node--focused/);
  await expect(page.locator('.react-flow__node[data-id="Spec"]')).toHaveClass(/atlas-node--dimmed/);
  await expect
    .poll(() =>
      person.evaluate((element) => {
        const canvas = document.querySelector(".atlas-canvas")?.getBoundingClientRect();

        if (!canvas) throw new Error("Expected atlas canvas");
        const node = element.getBoundingClientRect();

        return Math.hypot(
          node.x + node.width / 2 - canvas.x - canvas.width / 2,
          node.y + node.height / 2 - canvas.y - canvas.height / 2,
        );
      }),
    )
    .toBeLessThan(2);

  await search.fill("person");
  await expect(index.getByRole("button", { name: /^Focus / })).toHaveCount(1);
  expect(await nodeIds()).toEqual(originalIds);
  await search.fill("no-such-type");
  await expect(index.getByText("No matching types.")).toBeVisible();
  expect(await nodeIds()).toEqual(originalIds);
  await search.clear();
  await index.getByRole("button", { name: "Show all types" }).click();
  await expect(page.locator(".atlas-node--dimmed, .atlas-node--focused")).toHaveCount(0);
  expect(await nodeIds()).toEqual(originalIds);
  await search.fill("Person");
  await search.press("Enter");
  await person.locator(".ontology-node__card").click();
  await expect(page).toHaveURL(/\/ontology\/type\/Person$/);
  await expect(page.getByRole("heading", { name: "Person", exact: true })).toBeVisible();
});

test("Atlas rendered cards match layout dimensions and relation handles leave from the card middle", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/ontology");
  await page.getByRole("searchbox", { name: "Find a type" }).fill("project");
  const focus = page.getByRole("button", { name: "Focus Project", exact: true });
  await expect(focus).toBeVisible();
  await expect(page.locator('.react-flow__node[data-id="Project"]')).toBeVisible();
  await expectAtlasGeometry(page);
  await expect.poll(() => page.locator(".react-flow__edge").count()).toBeGreaterThan(0);
  await expect(page.locator(".react-flow__edge-textwrapper, .react-flow__edge-text")).toHaveCount(
    0,
  );
  await focus.click();
  await expect(page.locator('.react-flow__node[data-id="Project"]')).toHaveClass(
    /atlas-node--focused/,
  );
  await expectAtlasGeometry(page);
});

test("Atlas pans on scroll and supports pinch zoom, drag pan, and zoom controls", async ({
  page,
}) => {
  await page.goto("/ontology");
  await expect(page.locator(".ontology-node").first()).toBeVisible();
  const viewport = page.locator(".react-flow__viewport");
  const pane = page.locator(".react-flow__pane");
  const transform = () => viewport.evaluate((element) => getComputedStyle(element).transform);
  await expect.poll(transform).not.toBe("none");
  const before = await transform();

  const consumed = await pane.evaluate((element) => {
    const event = new WheelEvent("wheel", { deltaY: 100, bubbles: true, cancelable: true });
    element.dispatchEvent(event);

    return event.defaultPrevented;
  });

  expect(consumed).toBe(true);
  await expect.poll(transform).not.toBe(before);
  const scrolled = await transform();
  await pane.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    element.dispatchEvent(
      new WheelEvent("wheel", {
        deltaY: -40,
        ctrlKey: true,
        clientX: rect.x + rect.width / 2,
        clientY: rect.y + rect.height / 2,
        bubbles: true,
        cancelable: true,
      }),
    );
  });
  await expect.poll(transform).not.toBe(scrolled);
  const pinched = await transform();
  const rect = await pane.boundingBox();

  if (!rect) throw new Error("Atlas pane has no bounds");
  // The top right of the pane is clear of the finder, zoom controls, and legend.
  await page.mouse.move(rect.x + rect.width - 80, rect.y + 20);
  await page.mouse.down();
  await page.mouse.move(rect.x + rect.width - 130, rect.y + 70, { steps: 5 });
  await page.mouse.up();
  await expect.poll(transform).not.toBe(pinched);
  await expect(page.getByRole("button", { name: "Zoom In", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Fit View", exact: true }).click();
});
