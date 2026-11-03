// A view's declared params (SPEC-0110). They start from the page's own URL
// query. Framed, the host owns the workspace URL: `setViewParams` asks it to
// change, and it answers with the new values unless the change remounts the
// frame. Standalone, the page writes its own URL and history.
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
import { embedded } from "./editSession";
import { getViewInvocation } from "./viewContext";

export type {
  ViewParamDeclaration,
  ViewParamKind,
  ViewParams,
  ViewParamsPatch,
  ViewParamValue,
} from "../src/views/params";

const declarations = () => getViewInvocation().params ?? [];

let state: { params: ViewParams; identity: string } | undefined;

const listeners = new Set<() => void>();

function read(search: string | URLSearchParams) {
  const params = readViewParams(declarations(), search);

  return { params, identity: viewParamsIdentity(declarations(), params) };
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

/** The declared params with defaults applied, for HTML and self-mounted code. */
export function getViewParams(): ViewParams {
  return current().params;
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
  // SAFETY: TParams is the view's description of its own declared params.
  return useSyncExternalStore(subscribeViewParams, getViewParams) as TParams;
}

/**
 * Change declared params; `null` returns one to its default. A UI param adds a
 * history entry without remounting; an identity param remounts the view.
 * Throws when a name is not declared or a value does not match its kind.
 */
export function setViewParams(patch: ViewParamsPatch) {
  const error = viewParamsPatchError(declarations(), patch);

  if (error) throw new Error(`setViewParams: ${error}`);

  if (embedded) {
    window.parent.postMessage(
      { type: PARAMS_CHANGED_MESSAGE, params: patch },
      window.location.origin,
    );

    return;
  }

  const url = new URL(window.location.href);
  applyViewParamsPatch(declarations(), url.searchParams, patch);
  const next = read(url.searchParams);

  if (next.identity !== current().identity) {
    window.location.assign(url.href);

    return;
  }

  window.history.pushState(window.history.state, "", url.href);
  publish(next);
}
