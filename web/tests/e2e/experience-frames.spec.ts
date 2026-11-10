import { expect, test, type Page } from "./fixtures";
import { feedbackScreenshot, openGroup } from "./groupViewHelpers";

// The synthetic experience in fixtures/experience: a group home, a record page
// for its ExpClaim notes, and a panel view, in one views folder with
// experience.yml (SPEC-0136 R2, R3, R10; SPEC-0135 R6a).

const frames = (page: Page) => page.locator("iframe.experience-frame");

const shown = (page: Page) => page.frameLocator("iframe.experience-frame[data-shown]");

// A frame keeps recently shown pages mounted but hidden; read the one on screen.
const label = (page: Page, name: string) => shown(page).getByLabel(name).filter({ visible: true });

const frameStart = (page: Page) => label(page, "Frame").textContent();

const tab = (page: Page, name: RegExp) =>
  page.getByRole("tablist", { name: "Open notes" }).getByRole("tab", { name });

test("records of one experience open in new tabs without a new frame, and tabs return", async ({
  page,
}) => {
  await page.goto("/notes/group/E2E%20Experience");
  await expect(shown(page).getByRole("heading", { name: "Experience home" })).toBeVisible();
  await expect(frames(page)).toHaveCount(1);
  // The shell loads the experience frame without the Tailwind compiler.
  expect(await frames(page).getAttribute("src")).toMatch(/[?&]frame=experience(&|$)/);
  const start = await frameStart(page);

  await shown(page).getByRole("button", { name: "Sort by recent" }).click();
  await expect(page).toHaveURL(/[?&]sort=recent(&|$)/);
  await expect(label(page, "Sort")).toHaveText("sort=recent");

  for (const name of ["alpha", "beta"]) {
    // With no note tab open, the strip and its Home tab are hidden.
    if (name !== "alpha") await tab(page, /^Home$/).click();
    await expect(shown(page).getByRole("heading", { name: "Experience home" })).toBeVisible();
    await shown(page)
      .getByRole("button", { name: `Open ${name}` })
      .click();
    await expect(shown(page).getByRole("heading", { name: `Claim page ${name}.md` })).toBeVisible();
    await expect(tab(page, new RegExp(`Claim ${name}`, "i"))).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // A record of the same experience in a new tab reuses the frame.
    await expect(frames(page)).toHaveCount(1);
    expect(await frameStart(page)).toBe(start);
  }

  await feedbackScreenshot(page, "experience-record");

  await shown(page).getByRole("button", { name: "Show evidence" }).click();
  await expect(page).toHaveURL(/[?&]section=evidence(&|$)/);

  // Returning to a tab shows its page with the params it had (SPEC-0135 R6a).
  await tab(page, /Claim alpha/i).click();
  await expect(shown(page).getByRole("heading", { name: "Claim page alpha.md" })).toBeVisible();
  await expect(label(page, "Section")).toHaveText("section=summary");
  await tab(page, /Claim beta/i).click();
  await expect(label(page, "Section")).toHaveText("section=evidence");
  await tab(page, /^Home$/).click();
  await expect(label(page, "Sort")).toHaveText("sort=recent");
  await expect(frames(page)).toHaveCount(1);
  expect(await frameStart(page)).toBe(start);

  // Back and Forward land on the page the URL names.
  await page.goBack();
  await expect(shown(page).getByRole("heading", { name: "Claim page beta.md" })).toBeVisible();
  await page.goForward();
  await expect(shown(page).getByRole("heading", { name: "Experience home" })).toBeVisible();
  expect(await frameStart(page)).toBe(start);

  // A reload starts one new frame at the tab on screen; the other tabs keep their params.
  await tab(page, /Claim beta/i).click();
  await page.reload();
  await expect(shown(page).getByRole("heading", { name: "Claim page beta.md" })).toBeVisible();
  await expect(label(page, "Section")).toHaveText("section=evidence");
  await expect(frames(page)).toHaveCount(1);
  expect(await frameStart(page)).not.toBe(start);
  await tab(page, /^Home$/).click();
  await expect(label(page, "Sort")).toHaveText("sort=recent");
});

test("a frame no tab shows is suspended and hidden, and comes back without reloading", async ({
  page,
}) => {
  await page.goto("/notes/group/E2E%20Experience");
  await expect(shown(page).getByRole("heading", { name: "Experience home" })).toBeVisible();
  const start = await frameStart(page);

  await page
    .getByRole("region", { name: "Collections" })
    .getByRole("button", { name: "All notes" })
    .click();
  await expect(frames(page)).toHaveCount(1);
  await expect(frames(page)).toHaveAttribute("inert", "");
  await expect(frames(page)).not.toHaveAttribute("data-shown", "");
  await expect(frames(page)).toBeHidden();

  await openGroup(page, "E2E Experience");
  await expect(shown(page).getByRole("heading", { name: "Experience home" })).toBeVisible();
  expect(await frameStart(page)).toBe(start);
});

test("a record page closes its header with one hairline, and facts in its body keep theirs", async ({
  page,
}) => {
  await page.goto("/notes/group/E2E%20Experience");
  await shown(page).getByRole("button", { name: "Open alpha" }).click();
  const header = shown(page).locator("article > header");
  await expect(header).toHaveCSS("border-bottom-width", "1px");

  const facts = header.locator(".rz-facts");
  await expect(facts).toHaveCSS("border-bottom-width", "0px");
  await expect(facts).toHaveCSS("margin-bottom", "0px");
  await expect(facts).toHaveCSS("padding-bottom", "0px");
  await expect(shown(page).locator(".e2e-body-facts")).toHaveCSS("border-bottom-width", "1px");
});
