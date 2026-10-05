import { isJsonObject, isString, type JsonObject } from "../src/api/parse";
import { isViewContext, type ViewContext, type ViewInvocation } from "../src/views/context";
import type { ViewOrigin } from "./types";

export { customViewHref, nodeHref, workspaceViewHref } from "../src/views/context";

export type {
  OpenNodeOptions,
  ViewContext,
  ViewInvocation,
  ViewModuleProps,
} from "../src/views/context";

export type KitViewInvocation = ViewInvocation & {
  view: ViewInvocation["view"] & { origin?: ViewOrigin };
};

/** Validated context and authored configuration supplied by the server, for HTML and TSX. */
export function getViewInvocation(): KitViewInvocation {
  const data = document.getElementById("rhizome-view-invocation")?.textContent ?? "";

  // The invocation is immutable for the page's lifetime, so parse it once.
  if (cached?.data !== data) cached = { data, invocation: parseInvocation(data) };

  return cached.invocation;
}

let cached: { data: string; invocation: KitViewInvocation } | undefined;

const ORIGINS: readonly ViewOrigin[] = ["bundled", "repository", "generated"];

function isViewOrigin(value: unknown): value is ViewOrigin {
  return ORIGINS.some((origin) => origin === value);
}

function parseInvocation(data: string): KitViewInvocation {
  if (!data) return { view: { id: "", name: "" }, context: { kind: "standalone" } };

  const value: unknown = JSON.parse(data);

  if (
    !isJsonObject(value) ||
    !isJsonObject(value.view) ||
    !isString(value.view.id) ||
    !isString(value.view.name) ||
    !isViewContext(value.context)
  ) {
    throw new Error("Invalid server view invocation");
  }

  if (value.configuration !== undefined && !isJsonObject(value.configuration)) {
    throw new Error("Invalid view configuration");
  }

  if (value.view.origin !== undefined && !isViewOrigin(value.view.origin)) {
    throw new Error("Invalid view origin");
  }

  if (
    (value.vaultKey !== undefined && !isString(value.vaultKey)) ||
    (value.preferenceSlot !== undefined && !isString(value.preferenceSlot))
  ) {
    throw new Error("Invalid view preference scope");
  }

  const invocation: KitViewInvocation = {
    view: { id: value.view.id, name: value.view.name, origin: value.view.origin },
    context: value.context,
    configuration: value.configuration,
  };

  if (value.vaultKey !== undefined) invocation.vaultKey = value.vaultKey;

  if (value.preferenceSlot !== undefined) invocation.preferenceSlot = value.preferenceSlot;

  return invocation;
}

export function getViewContext(): ViewContext {
  return getViewInvocation().context;
}

export function getViewConfiguration(): JsonObject | undefined {
  return getViewInvocation().configuration;
}

// A context change remounts the frame; it is immutable for the page's lifetime.
export function useViewContext(): ViewContext {
  return getViewContext();
}
