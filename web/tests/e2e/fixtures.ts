import fs from "node:fs";
import os from "node:os";
import path from "node:path";
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

// The suite shares one server, so preferences a page saved outlive its test.
// Every test first resets each scope an earlier page wrote, before its own page
// loads. Resetting while a test ran raced its first clicks and Playwright's own
// handling of held requests. The list lives in a file per server so a restarted
// worker or a reused local server still resets it.
function scopesFile(baseURL: string) {
  return path.join(os.tmpdir(), `rhizome-e2e-preference-scopes-${new URL(baseURL).port}.json`);
}

function readScopes(file: string): string[] {
  if (!fs.existsSync(file)) return [];
  const scopes: unknown = JSON.parse(fs.readFileSync(file, "utf8"));

  return Array.isArray(scopes) ? scopes.filter(isString) : [];
}

export const test = base.extend<{ isolatedPreferences: void }>({
  isolatedPreferences: [
    async ({ baseURL, context, request }, use) => {
      if (!baseURL) throw new Error("Preference isolation needs the suite baseURL");
      const file = scopesFile(baseURL);

      for (const serialized of readScopes(file)) await resetScope(request, parseScope(serialized));
      const written = new Set<string>();

      context.on("request", (incoming) => {
        if (incoming.method() === "GET") return;

        if (!new URL(incoming.url()).pathname.startsWith("/api/v1/view-preferences")) return;
        const body: unknown = incoming.postDataJSON();

        if (isJsonObject(body) && isJsonObject(body.scope)) written.add(JSON.stringify(body.scope));
      });

      try {
        await use();
      } finally {
        fs.writeFileSync(file, JSON.stringify([...written]));
      }
    },
    { auto: true },
  ],
});
