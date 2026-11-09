import {
  useScopedViewPreference,
  getScopedViewPreference,
  type PreferenceDefinition,
} from "../src/viewPreferences/hooks";
import type { ViewPreferenceScope } from "../src/viewPreferences/types";
import { PANEL_TAB_ID } from "../src/lib/customViewMessages";
import { activePageBinding, usePageBinding, type PageBinding } from "./pageBinding";
import { getShellInvocation } from "./viewContext";

export type {
  PreferenceAccessor,
  PreferenceDefinition,
  PreferenceSnapshot,
} from "../src/viewPreferences/hooks";

export type ViewPreferenceOptions<T> = PreferenceDefinition<T> & { slot?: string };

// A panel's page keeps one store per vault and experience, whatever its
// subject (SPEC-0136 R6): its view ID is the experience's panel.
function binding(page: PageBinding | null, slot?: string) {
  const invocation = page?.invocation ?? getShellInvocation();

  if (!invocation.view.id) throw new Error("View preferences require a server view invocation");

  if (!invocation.vaultKey) throw new Error("View preferences require the server vault identity");

  const scope: ViewPreferenceScope = {
    viewId: invocation.view.id,
    context: page?.tabId === PANEL_TAB_ID ? { kind: "standalone" } : invocation.context,
  };

  if (invocation.preferenceSlot) scope.slot = invocation.preferenceSlot;

  if (slot) scope.widgetSlot = slot;

  return { scope, vaultKey: invocation.vaultKey };
}

/**
 * Personal preferences for this view and mount subject, or for a panel, for its
 * experience. Defaults may come from authored configuration.
 */
export function useViewPreference<T>(key: string, options: ViewPreferenceOptions<T>) {
  const { scope, vaultKey } = binding(usePageBinding(), options.slot);

  return useScopedViewPreference(scope, vaultKey, key, options);
}

/** The same preference contract without React, including subscription for HTML controls. */
export function getViewPreference<T>(key: string, options: ViewPreferenceOptions<T>) {
  const { scope, vaultKey } = binding(activePageBinding(), options.slot);

  return getScopedViewPreference(scope, vaultKey, key, options);
}
