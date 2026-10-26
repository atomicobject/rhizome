import { isPreferenceResponse, isPreferenceScope } from "../../src/viewPreferences/types";
import { expect, test } from "./fixtures";
import { openViewOptions, rowsChoice } from "./workspaceView";

function guideScope(url: string) {
  const parsed = new URL(url);

  if (parsed.pathname !== "/api/v1/view-preferences") return null;
  const serialized = parsed.searchParams.get("scope");

  if (!serialized) return null;
  const scope: unknown = JSON.parse(serialized);

  // The native table's own scope, not the view selection or a built-in
  // presentation such as builtin:overview.
  return isPreferenceScope(scope) &&
    scope.viewId !== "$selection" &&
    !scope.viewId.startsWith("builtin:") &&
    scope.context.kind === "type" &&
    scope.context.type === "TvGuide"
    ? scope
    : null;
}

test("a filter for a removed field is cleared before it can hide the collection after reload", async ({
  page,
  request,
}) => {
  const nativeRead = page.waitForRequest(
    (incoming) => incoming.method() === "GET" && guideScope(incoming.url()) !== null,
  );

  await page.goto("/notes/tv-guide");
  const scope = guideScope((await nativeRead).url());

  if (!scope) throw new Error("The guide did not request its native preference scope");
  await expect(page.locator("tr.configured-view__row")).toHaveCount(5);

  const read = await request.get("/api/v1/view-preferences", {
    params: { scope: JSON.stringify(scope) },
  });

  expect(read.ok()).toBe(true);
  const snapshot: unknown = await read.json();

  if (!isPreferenceResponse(snapshot)) throw new Error("Invalid preferences snapshot");
  const obsolete = [{ field: "removedField", op: "eq", value: "x" }];

  const seeded = await request.patch("/api/v1/view-preferences", {
    data: { scope, expectedRevision: snapshot.revision, set: { "native.filters": obsolete } },
  });

  expect(seeded.ok(), await seeded.text()).toBe(true);
  const persisted: unknown = await seeded.json();

  if (!isPreferenceResponse(persisted)) throw new Error("Invalid seeded preferences snapshot");
  expect(persisted.values["native.filters"]).toEqual(obsolete);

  await page.reload();
  await expect(page.locator("tr.configured-view__row")).toHaveCount(5);
  await expect
    .poll(async () => {
      const current = await request.get("/api/v1/view-preferences", {
        params: { scope: JSON.stringify(scope) },
      });

      expect(current.ok()).toBe(true);
      const value: unknown = await current.json();

      if (!isPreferenceResponse(value)) throw new Error("Invalid reloaded preferences snapshot");

      return Object.hasOwn(value.values, "native.filters");
    })
    .toBe(false);
});

test("invalid saved layout falls back visibly and recovers after a valid choice", async ({
  page,
  request,
}) => {
  const nativeRead = page.waitForRequest(
    (incoming) => incoming.method() === "GET" && guideScope(incoming.url()) !== null,
  );

  await page.goto("/notes/tv-guide");
  const scope = guideScope((await nativeRead).url());

  if (!scope) throw new Error("The guide did not request its native preference scope");
  await openViewOptions(page);
  await expect(rowsChoice(page, "2 lines")).toHaveAttribute("aria-pressed", "true");

  const read = await request.get("/api/v1/view-preferences", {
    params: { scope: JSON.stringify(scope) },
  });

  expect(read.ok()).toBe(true);
  const snapshot: unknown = await read.json();

  if (!isPreferenceResponse(snapshot)) throw new Error("Invalid preferences snapshot");

  const seeded = await request.patch("/api/v1/view-preferences", {
    data: { scope, expectedRevision: snapshot.revision, set: { "native.density": "unsupported" } },
  });

  expect(seeded.ok(), await seeded.text()).toBe(true);
  await page.reload();
  await openViewOptions(page);
  await expect(rowsChoice(page, "2 lines")).toHaveAttribute("aria-pressed", "true");

  const warning = page
    .getByRole("alert")
    .filter({ hasText: "Some view preferences could not load" });

  await expect(warning).toBeVisible();
  await warning.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(warning).toBeVisible();

  const saved = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/view-preferences" &&
      response.request().method() === "PATCH",
  );

  // Retry sits outside View options, so pressing it closed them.
  await openViewOptions(page);
  await rowsChoice(page, "1 line").click();
  expect((await saved).ok()).toBe(true);
  await expect(warning).toBeHidden();
  await page.reload();
  await openViewOptions(page);
  await expect(rowsChoice(page, "1 line")).toHaveAttribute("aria-pressed", "true");
  await expect(warning).toBeHidden();
});
