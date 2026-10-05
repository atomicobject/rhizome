import { test as base, type APIRequestContext } from "@playwright/test";
import { isJsonObject, isString } from "../../src/api/parse";
import { isViewContext, type ViewContext } from "../../src/views/context";

export { expect } from "@playwright/test";

export type { APIRequestContext, FrameLocator, Locator, Page } from "@playwright/test";

type TestPreferenceScope = {
  viewId: string;
  context: ViewContext;
  slot?: string;
  widgetSlot?: string;
};

function parseScope(serialized: string): TestPreferenceScope {
  const scope: unknown = JSON.parse(serialized);

  if (!isJsonObject(scope) || !isString(scope.viewId) || !isViewContext(scope.context)) {
    throw new Error(`Preference fixture received an invalid scope: ${serialized}`);
  }

  if (scope.slot !== undefined && !isString(scope.slot)) {
    throw new Error("Preference fixture received an invalid slot");
  }

  if (scope.widgetSlot !== undefined && !isString(scope.widgetSlot)) {
    throw new Error("Preference fixture received an invalid widget slot");
  }

  const parsed: TestPreferenceScope = { viewId: scope.viewId, context: scope.context };

  if (scope.slot !== undefined) parsed.slot = scope.slot;

  if (scope.widgetSlot !== undefined) parsed.widgetSlot = scope.widgetSlot;

  return parsed;
}

async function resetScope(request: APIRequestContext, scope: TestPreferenceScope) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    const read = await request.get("/api/v1/view-preferences", {
      params: { scope: JSON.stringify(scope) },
    });

    if (!read.ok()) throw new Error(`Preference fixture read failed: ${read.status()}`);

    const snapshot = await read.json();

    const reset = await request.post("/api/v1/view-preferences/reset", {
      data: { scope, expectedRevision: snapshot.revision },
    });

    if (reset.ok()) return;

    if (reset.status() !== 409) {
      throw new Error(`Preference fixture reset failed: ${reset.status()}`);
    }
  }

  throw new Error("Preference fixture could not reset a concurrently changing scope");
}

export const test = base.extend<{ isolatedPreferences: void }>({
  isolatedPreferences: [
    async ({ context, request }, use) => {
      const scopes = new Map<string, Promise<void>>();

      await context.route("**/api/v1/view-preferences?*", async (route) => {
        if (route.request().method() !== "GET") return route.continue();
        const serialized = new URL(route.request().url()).searchParams.get("scope");

        if (!serialized) throw new Error("Preference read did not identify its scope");
        let ready = scopes.get(serialized);

        if (!ready) {
          ready = resetScope(request, parseScope(serialized));
          scopes.set(serialized, ready);
        }

        await ready;
        await route.continue();
      });

      try {
        await use();
      } finally {
        await context.unrouteAll({ behavior: "wait" });
      }
    },
    { auto: true },
  ],
});
