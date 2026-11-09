// A view's declared params (SPEC-0110). They start from the page's own URL
// query. Framed, the host owns the workspace URL: `setViewParams` asks it to
// change, and it answers with the new values unless the change remounts the
// frame. Standalone, the page writes its own URL and history. In an experience
// frame each page's params arrive with its location (SPEC-0136 R2).
import { useSyncExternalStore } from "react";

import { isPlainObject } from "../src/api/parse";
import { PARAMS_CHANGED_MESSAGE, VIEW_PARAMS_MESSAGE } from "../src/lib/customViewMessages";
import {
  applyViewParamsPatch,
  readViewParams,
  viewParamsIdentity,
  viewParamsPatchError,
  type ViewParams,
  type ViewParamsPatch,
} from "../src/views/params";
import type { JsonObject } from "../src/api/parse";
import type { ViewContext } from "../src/views/context";
import { embedded } from "./editSession";
import { activePageBinding, pageStamp, usePageBinding, type PageBinding } from "./pageBinding";
import { getShellInvocation } from "./viewContext";

export type {
  ViewParamDeclaration,
  ViewParamKind,
  ViewParams,
  ViewParamsPatch,
  ViewParamValue,
} from "../src/views/params";

// The declarations of `page`, or of the document outside experience frames.
const declarationsOf = (page: PageBinding | null) =>
  (page?.invocation ?? getShellInvocation()).params ?? [];

// The document's own params, from its URL; experience pages use their binding.
const shellDeclarations = () => getShellInvocation().params ?? [];

let state: { params: ViewParams; identity: string } | undefined;

const listeners = new Set<() => void>();

function read(search: string | URLSearchParams) {
  const params = readViewParams(shellDeclarations(), search);

  return { params, identity: viewParamsIdentity(shellDeclarations(), params) };
}

function current() {
  if (!state) {
    state = read(window.location.search);

    if (!embedded) window.addEventListener("popstate", onPopState);
  }

  return state;
}

function publish(next: ReturnType<typeof read>) {
  if (JSON.stringify(next.params) === JSON.stringify(state?.params)) return;
  state = next;

  for (const listener of listeners) listener();
}

// Back and Forward on a standalone page: a changed identity param is a new
// invocation, so the page reloads; anything else updates in place.
function onPopState() {
  const next = read(window.location.search);

  if (next.identity !== current().identity) window.location.reload();
  else publish(next);
}

if (embedded) {
  window.addEventListener("message", (event) => {
    if (event.origin !== window.location.origin || event.source !== window.parent) return;

    const data: unknown = event.data;

    if (!isPlainObject(data) || data.type !== VIEW_PARAMS_MESSAGE) return;
    const query = new URLSearchParams();

    if (isPlainObject(data.params)) {
      for (const [name, value] of Object.entries(data.params)) query.set(name, String(value));
    }

    // Read through the declarations, so a value of the wrong kind falls back.
    publish({ ...read(query), identity: current().identity });
  });
}

const documentParams = () => current().params;

/** The declared params with defaults applied, for HTML and self-mounted code; the page on screen's in an experience frame. */
export function getViewParams(): ViewParams {
  return activePageBinding()?.params ?? documentParams();
}

/** Call `listener` whenever the params change in place; returns an unsubscribe. */
export function subscribeViewParams(listener: () => void) {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

/**
 * The view's declared params with defaults applied. Undeclared query keys are
 * ignored. It updates when a UI param changes; an identity param change
 * remounts the view instead.
 */
export function useViewParams<TParams extends ViewParams = ViewParams>(): TParams {
  const binding = usePageBinding();

  const params = useSyncExternalStore(
    binding ? noSubscription : subscribeViewParams,
    binding ? () => binding.params : documentParams,
  );

  // SAFETY: TParams is the view's description of its own declared params.
  return params as TParams;
}

const noSubscription = () => () => undefined;

export type ViewLocation = {
  viewId: string;
  /** The workbench tab showing this page, or `panel`; undefined outside experience frames. */
  tabId?: string;
  context: ViewContext;
  params: ViewParams;
  configuration?: JsonObject;
};

/** Where the page this component renders in is: its view, tab, subject, params, and configuration. */
export function useViewLocation(): ViewLocation {
  const binding = usePageBinding();
  const params = useViewParams();
  const invocation = binding?.invocation ?? getShellInvocation();

  const location: ViewLocation = {
    viewId: invocation.view.id,
    context: invocation.context,
    params,
  };

  if (binding) location.tabId = binding.tabId;

  if (invocation.configuration) location.configuration = invocation.configuration;

  return location;
}

/**
 * Change declared params; `null` returns one to its default. A UI param adds a
 * history entry without remounting; an identity param remounts the view.
 * Throws when a name is not declared or a value does not match its kind. In an
 * experience frame it changes the page on screen; components use
 * `usePageNavigation().setViewParams` to change their own page's.
 */
export function setViewParams(patch: ViewParamsPatch) {
  setPageViewParams(activePageBinding(), patch);
}

/** `setViewParams` for `page`: checked against its declarations and stamped with it. */
export function setPageViewParams(page: PageBinding | null, patch: ViewParamsPatch) {
  const declarations = declarationsOf(page);
  const error = viewParamsPatchError(declarations, patch);

  if (error) throw new Error(`setViewParams: ${error}`);

  if (embedded) {
    window.parent.postMessage(
      { type: PARAMS_CHANGED_MESSAGE, params: patch, ...pageStamp(page) },
      window.location.origin,
    );

    return;
  }

  const url = new URL(window.location.href);
  applyViewParamsPatch(declarations, url.searchParams, patch);
  const next = read(url.searchParams);

  if (next.identity !== current().identity) {
    window.location.assign(url.href);

    return;
  }

  window.history.pushState(window.history.state, "", url.href);
  publish(next);
}
