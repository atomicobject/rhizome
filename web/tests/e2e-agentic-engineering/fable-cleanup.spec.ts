import { isJsonObject, type JsonValue } from "../../src/api/parse";
import { expect, test } from "@playwright/test";
import { capture, content, home, noOverflow, specPath, stage } from "./helpers";

test.afterEach(async ({ page }) => {
  await page.unrouteAll({ behavior: "wait" });
});

test("stage sort reorders the visible rows and the composed prompt together", async ({
  page,
}, info) => {
  await page.goto(stage("ready"));
  const f = content(page);
  const sort = f.getByRole("radiogroup", { name: "Sort records", exact: true });
  await expect(sort.getByRole("radio", { name: "Source", exact: true })).toBeChecked();
  await f.getByRole("checkbox", { name: /Select US1.*Reserve a seat/ }).check();
  await f.getByRole("checkbox", { name: /Select US3.*Recover a saved itinerary/ }).check();
  const prompt = f.getByRole("textbox", { name: /prompt/i });

  const firstStory = async (read: () => Promise<string>) => {
    const text = await read();
    const first = text.indexOf("US1");
    const third = text.indexOf("US3");

    return first < 0 || third < 0 ? "missing" : first < third ? "US1" : "US3";
  };

  const rows = async () =>
    (
      await f
        .getByRole("checkbox", { name: /^Select US\d/, checked: true })
        .evaluateAll((boxes) => boxes.map((box) => box.getAttribute("aria-label")))
    ).join("\n");

  const preview = () => prompt.inputValue();

  await expect.poll(() => firstStory(rows)).toBe("US1");
  await expect.poll(() => firstStory(preview)).toBe("US1");
  await sort.getByText("Z–A", { exact: true }).click();
  await expect(sort.getByRole("radio", { name: "Z–A", exact: true })).toBeChecked();
  await expect.poll(() => firstStory(rows)).toBe("US3");
  await expect.poll(() => firstStory(preview)).toBe("US3");
  await expect(f.getByRole("button", { name: "Copy workflow prompt", exact: true })).toBeEnabled();
  await sort.getByText("A–Z", { exact: true }).click();
  await expect.poll(() => firstStory(rows)).toBe("US1");
  await expect.poll(() => firstStory(preview)).toBe("US1");
  await capture(page, info, "sorted-ready-selection");
});

test("record review names the exact pending selected record and its locator", async ({
  page,
}, info) => {
  let changed = 0;
  await page.route("**/api/v1/graphql", async (route) => {
    const response = await route.fetch();
    const json: unknown = await response.json();

    if (!isJsonObject(json) || !isJsonObject(json.data))
      throw new Error("Expected fixture GraphQL data");

    function isSelectedStory(ref: JsonValue | undefined) {
      return isJsonObject(ref) && ref.fragment === "^SPEC-9001-US4";
    }

    function update(value: JsonValue) {
      if (Array.isArray(value)) {
        value.forEach(update);

        return;
      }

      if (!isJsonObject(value)) return;

      const linkTarget =
        isJsonObject(value.locator) && isJsonObject(value.locator.linkTarget)
          ? value.locator.linkTarget
          : null;

      if (isSelectedStory(value.ref) || isSelectedStory(linkTarget?.ref)) {
        const workspace = isJsonObject(value.workspace) ? value.workspace : null;

        if (Array.isArray(workspace?.fields))
          for (const field of workspace.fields) {
            if (isJsonObject(field) && field.name === "status") {
              field.values = ["ready"];
              changed++;
            }
          }
      }

      Object.values(value).forEach(update);
    }

    update(json.data);
    await route.fulfill({ response, json });
  });
  await page.goto(stage("review"));
  const f = content(page);

  const pending = f.getByRole("list", {
    name: "Pending selected records for Complete with unselected ready story",
    exact: true,
  });

  await f.getByText("1 pending", { exact: true }).click();
  await expect(pending).toBeVisible();
  expect(changed).toBeGreaterThan(0);
  await expect(pending.getByRole("listitem")).toHaveCount(1);
  await expect(
    pending.getByRole("button", { name: /US4 - Read the published timetable/ }),
  ).toBeVisible();
  await expect(pending.getByText(`${specPath}#^SPEC-9001-US4`, { exact: true })).toBeVisible();
  await capture(page, info, "pending-review-scope");
});

test("the panel marks the stage on screen and no stage on the home page", async ({
  page,
}, info) => {
  await page.goto(stage("ready"));
  const panel = page.frameLocator("iframe.panel-frame[data-shown]");
  const ready = panel.getByRole("link", { name: /Ready stories/ });
  const active = panel.getByRole("link", { name: /Active efforts/ });
  await expect(ready).toHaveAttribute("aria-current", "page");
  await expect(active).not.toHaveAttribute("aria-current", "page");
  const find = panel.getByRole("searchbox", { name: "Find a spec or effort" });
  await find.fill("Harbor");
  await active.click();
  await expect(page).toHaveURL(/stage=active/);
  await expect(active).toHaveAttribute("aria-current", "page");
  await expect(ready).not.toHaveAttribute("aria-current", "page");
  await expect(find).toHaveValue("Harbor");
  await capture(page, info, "panel-active-stage");
  await page.goto(home);
  await expect(ready).toBeVisible();
  await expect(
    panel.getByRole("navigation", { name: "Delivery workflow" }).locator('[aria-current="page"]'),
  ).toHaveCount(0);
});

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(colorScheme, () => {
    test.use({ colorScheme });
    test("a partial read keeps refresh visible and its raw error behind a disclosure", async ({
      page,
    }, info) => {
      await page.route("**/api/v1/graphql", (route) => route.abort("failed"));
      await page.goto(home);
      const f = content(page);

      const failure = f.getByRole("status").filter({ hasText: "Some sources didn't load" });

      await expect(failure).toBeVisible();
      await expect(f.getByRole("button", { name: "Refresh records", exact: true })).toBeVisible();
      const detail = f.locator(".ae-read-state details").filter({ hasText: "Error detail" });
      await expect(detail.locator("code")).toBeHidden();
      await capture(page, info, "partial-read-error");
      await detail.getByText("Error detail", { exact: true }).click();
      await expect(detail.locator("code")).toBeVisible();
      await expect(detail.locator("code")).not.toBeEmpty();
    });

    for (const width of [768, 390]) {
      for (const [name, url, heading] of [
        ["home", home, "Delivery"],
        ["ready", stage("ready"), "Ready stories"],
      ] as const) {
        test(`${name} reflows at ${width}px`, async ({ page }, info) => {
          await page.setViewportSize({ width, height: 900 });
          await page.goto(url);

          if (width < 480)
            await page.getByRole("button", { name: "Hide sidebar", exact: true }).click();
          await expect(
            content(page).getByRole("heading", { name: heading, exact: true }),
          ).toBeVisible();
          await noOverflow(page);
          await capture(page, info, `${name}-narrow-${width}`);
        });
      }
    }
  });
}
