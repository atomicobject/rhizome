import {
  useScopedViewPreference,
  getScopedViewPreference,
  type PreferenceDefinition,
} from "../src/viewPreferences/hooks";
import type { ViewPreferenceScope } from "../src/viewPreferences/types";
import { getViewInvocation } from "./viewContext";

export type {
  PreferenceAccessor,
  PreferenceDefinition,
  PreferenceSnapshot,
} from "../src/viewPreferences/hooks";

export type ViewPreferenceOptions<T> = PreferenceDefinition<T> & { slot?: string };

function binding(slot?: string) {
  const invocation = getViewInvocation();

  if (!invocation.view.id) throw new Error("View preferences require a server view invocation");

  if (!invocation.vaultKey) throw new Error("View preferences require the server vault identity");

  const scope: ViewPreferenceScope = { viewId: invocation.view.id, context: invocation.context };

  if (invocation.preferenceSlot) scope.slot = invocation.preferenceSlot;

  if (slot) scope.widgetSlot = slot;

  return { scope, vaultKey: invocation.vaultKey };
}

/** Personal preferences for this view and mount subject. Defaults may come from authored configuration. */
export function useViewPreference<T>(key: string, options: ViewPreferenceOptions<T>) {
  const { scope, vaultKey } = binding(options.slot);

  return useScopedViewPreference(scope, vaultKey, key, options);
}

/** The same preference contract without React, including subscription for HTML controls. */
export function getViewPreference<T>(key: string, options: ViewPreferenceOptions<T>) {
  const { scope, vaultKey } = binding(options.slot);

  return getScopedViewPreference(scope, vaultKey, key, options);
}
