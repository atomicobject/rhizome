import { expect, test } from "./fixtures";
import { chooseView } from "./workspaceView";

for (const activation of ["click", "Enter", "middle"]) {
  test(`custom view diagram opens the linked note with ${activation}`, async ({ page }) => {
    const peleRequests: string[] = [];
    page.on("request", (request) => {
      if (new URL(request.url()).pathname.startsWith("/kit/v1/chunks/pele-")) {
        peleRequests.push(request.url());
      }
    });
    await page.goto("/notes");
    const collections = page.getByRole("region", { name: "Collections" });
    await expect(collections.getByRole("button", { name: "E2E Views", exact: true })).toBeVisible();
    const expand = collections.getByRole("button", { name: "Expand E2E Views", exact: true });

    if (await expand.count()) await expand.click();

    await collections.getByRole("button", { name: "View demo efforts (2)", exact: true }).click();
    const frame = page.frameLocator("iframe.custom-view-frame");
    await expect(frame.getByRole("heading", { name: "Demo dashboard", exact: true })).toBeVisible();

    if (activation === "click") expect(peleRequests).toHaveLength(0);

    await chooseView(page, "Diagram demo");
    const diagram = frame.getByRole("group", { name: "Effort diagram", exact: true });
    const link = diagram.getByRole("link", { name: "View demo Alpha", exact: true });
    await expect(link).toBeVisible();

    if (activation === "click") {
      expect(peleRequests).toHaveLength(1);

      for (const width of [1280, 620]) {
        await page.setViewportSize({ width, height: 800 });

        const dimensions = await diagram.locator("svg").evaluate((svg) => ({
          width: svg.getBoundingClientRect().width,
          naturalWidth: Number(svg.getAttribute("width")),
          containerWidth: svg.parentElement?.getBoundingClientRect().width ?? 0,
        }));

        expect(dimensions.width).toBeGreaterThan(0);
        expect(dimensions.width).toBeLessThanOrEqual(dimensions.naturalWidth + 1);
        expect(dimensions.width).toBeLessThanOrEqual(dimensions.containerWidth + 1);
      }

      await diagram.evaluate((element) => {
        element.style.width = "80px";
      });

      const shrunken = await diagram.locator("svg").evaluate((svg) => ({
        width: svg.getBoundingClientRect().width,
        natural: Number(svg.getAttribute("width")),
      }));

      expect(shrunken.width).toBeLessThanOrEqual(81);
      expect(shrunken.width).toBeLessThan(shrunken.natural);
      await diagram.evaluate((element) => {
        element.style.width = "";
      });
      await link.click();
    } else if (activation === "middle") {
      const tabs = page.getByRole("tablist", { name: "Open notes" });
      const before = await tabs.getByRole("tab").count();
      const popups: string[] = [];
      page.on("popup", (popup) => popups.push(popup.url()));
      await link.click({ button: "middle" });
      await expect(tabs.getByRole("tab")).toHaveCount(before + 1);
      await expect(diagram).toBeVisible();
      expect(popups).toHaveLength(0);
      const openedTab = tabs.getByRole("tab").nth(before);

      await expect(openedTab).toHaveAttribute("aria-selected", "false");
      await openedTab.click();
    } else {
      await link.focus();
      await link.press("Enter");
    }

    await expect(
      frame.getByRole("heading", { name: "View demo Alpha", exact: true }),
    ).toBeVisible();
    expect(new URL(page.url()).searchParams.get("note")).toBe("notes/view-demo/effort-a.md");
  });
}
