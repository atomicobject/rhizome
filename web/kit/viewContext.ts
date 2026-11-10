import { isJsonObject, isString, type JsonObject } from "../src/api/parse";
import { isViewContext, type ViewContext, type ViewInvocation } from "../src/views/context";
import { parseViewParamDeclarations, type ViewParamDeclaration } from "../src/views/params";
import { activePageBinding, PageActiveContext, usePageBinding } from "./pageBinding";
import type { ViewOrigin } from "./types";
import { useContext } from "react";

export { customViewHref, nodeHref, workspaceViewHref } from "../src/views/context";

export type {
  OpenNodeOptions,
  ViewContext,
  ViewContextInput,
  ViewNodeRef,
  ViewInvocation,
  ViewModuleProps,
} from "../src/views/context";

export type KitViewInvocation = ViewInvocation & {
  /** `experienceKey` is set when the view renders as a page of an experience frame or is its panel. */
  view: ViewInvocation["view"] & { origin?: ViewOrigin; experienceKey?: string };
  /** The definition's declared params; read their values with `useViewParams`. */
  params?: ViewParamDeclaration[];
};

/**
 * Validated context and authored configuration supplied by the server, for
 * HTML and TSX. In an experience frame it is the invocation of the page on
 * screen; hooks read their own page's.
 */
export function getViewInvocation(): KitViewInvocation {
  return activePageBinding()?.invocation ?? getShellInvocation();
}

/** The invocation the server wrote into this document. */
export function getShellInvocation(): KitViewInvocation {
  const data = document.getElementById("rhizome-view-invocation")?.textContent ?? "";

  // The invocation is immutable for the document's lifetime, so parse it once.
  if (cached?.data !== data)
    cached = {
      data,
      invocation: data
        ? parseViewInvocation(JSON.parse(data))
        : { view: { id: "", name: "" }, context: { kind: "standalone" } },
    };

  return cached.invocation;
}

let cached: { data: string; invocation: KitViewInvocation } | undefined;

const ORIGINS: readonly ViewOrigin[] = ["bundled", "repository", "generated"];

function isViewOrigin(value: unknown): value is ViewOrigin {
  return ORIGINS.some((origin) => origin === value);
}

/** Validate an invocation from the server, as written into the shell or posted with a location. */
// oxlint-disable-next-line anti-slop/no-unknown-parameters -- the parser at the document and message boundary.
export function parseViewInvocation(value: unknown): KitViewInvocation {
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

  if (value.view.experienceKey !== undefined && !isString(value.view.experienceKey)) {
    throw new Error("Invalid experience key");
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

  if (value.view.experienceKey !== undefined)
    invocation.view.experienceKey = value.view.experienceKey;

  if (value.vaultKey !== undefined) invocation.vaultKey = value.vaultKey;

  if (value.params !== undefined) invocation.params = parseViewParamDeclarations(value.params);

  if (value.preferenceSlot !== undefined) invocation.preferenceSlot = value.preferenceSlot;

  return invocation;
}

export function getViewContext(): ViewContext {
  return getViewInvocation().context;
}

export function getViewConfiguration(): JsonObject | undefined {
  return getViewInvocation().configuration;
}

/** The subject of the page this component renders in. */
export function useViewContext(): ViewContext {
  return usePageBinding()?.invocation.context ?? getShellInvocation().context;
}

/** The authored configuration of the page this component renders in. */
export function useViewConfiguration(): JsonObject | undefined {
  const binding = usePageBinding();

  return binding ? binding.invocation.configuration : getShellInvocation().configuration;
}

/**
 * False while this page is hidden or its frame is suspended (SPEC-0136 R3).
 * Kit queries and action polling pause on their own; stop your own timers,
 * intervals, and animations while it is false.
 */
export function useViewActive(): boolean {
  return useContext(PageActiveContext);
}
