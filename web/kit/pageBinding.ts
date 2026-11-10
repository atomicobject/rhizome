// Which page a hook or message belongs to (SPEC-0136 R2). An experience frame
// renders one page per tab; each page's tree reads its own binding through
// React context, so a hidden page that re-renders never sees another page's
// location. Outside an experience frame there is no binding and the kit reads
// the page's one server invocation, as before.
import { createContext, useContext } from "react";

import type { PageStamp } from "../src/lib/customViewMessages";
import type { ViewParams } from "../src/views/params";
import type { KitViewInvocation } from "./viewContext";

export type PageBinding = {
  tabId: string;
  /** Changes only when the page's view, context, or identity params change. */
  navigationId: string;
  invocation: KitViewInvocation;
  params: ViewParams;
};

export const PageBindingContext = createContext<PageBinding | null>(null);

/** False while the page is hidden or its frame suspended. */
export const PageActiveContext = createContext(true);

let active: PageBinding | null = null;

/** The binding of the page on screen, for imperative getters; null outside experience frames. */
export function activePageBinding() {
  return active;
}

export function setActivePageBinding(binding: PageBinding | null) {
  active = binding;
}

/** The page binding this hook renders in, or null outside experience frames. */
export function usePageBinding() {
  return useContext(PageBindingContext);
}

/** The stamp a page's messages carry, so the host can reject a stale one. */
export function pageStamp(binding: PageBinding | null = active): PageStamp {
  return binding ? { tabId: binding.tabId, navigationId: binding.navigationId } : {};
}
