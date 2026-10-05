import { useCallback, useMemo, useState } from "react";
import {
  decodeJson,
  isJsonObject,
  isOptionalString,
  isJsonValue,
  type JsonObject,
} from "../api/parse";
import type { ViewCatalogEntry, ViewExecuteRequest } from "../api/types";
import type { ViewContext } from "../views/context";
import { useScopedViewPreference, useViewPreferences } from "../viewPreferences/hooks";
import {
  DURABLE_QUERY_FIELDS,
  durableRequest,
  nativeMigrations,
  nativePreferenceScope,
  readBrowserItem,
  validNativePreference,
  nativePreferenceError,
  viewStorageKey,
  type LayoutField,
} from "../viewPreferences/native";
import { preferenceScopeKey } from "../viewPreferences/types";

export { viewStorageKey } from "../viewPreferences/native";

const EMPTY_REQUEST: ViewExecuteRequest = {};

function readTransient(key: string, legacyKey?: string): ViewExecuteRequest {
  const value = decodeJson(
    readBrowserItem(() => window.sessionStorage, key) ??
      (legacyKey ? readBrowserItem(() => window.sessionStorage, legacyKey) : null),
    isJsonObject,
  );

  if (!value) return EMPTY_REQUEST;

  const transient: ViewExecuteRequest = {};

  if (isOptionalString(value.search) && value.search !== undefined) transient.search = value.search;

  if (isJsonObject(value.page)) transient.page = value.page;

  if (isOptionalString(value.variant) && value.variant !== undefined)
    transient.variant = value.variant;

  try {
    window.sessionStorage.setItem(key, JSON.stringify(transient));
  } catch {}

  return transient;
}

/** Durable personal query overrides plus tab-local search/page, scoped to the actual mount. */
export function useViewState(
  view: ViewCatalogEntry | null,
  vaultKey: string | null,
  context?: ViewContext,
  slot?: string,
) {
  const scope = view ? nativePreferenceScope(view, context, slot) : null;

  const key = scope
    ? `rhizome:view:transient:v3:${JSON.stringify([vaultKey, preferenceScopeKey(scope)])}`
    : "";

  const stored = useMemo(
    () => (view ? readTransient(key, viewStorageKey("state", vaultKey, view.id)) : EMPTY_REQUEST),
    [key, vaultKey, view],
  );

  const [transientByKey, setTransient] = useState<Record<string, ViewExecuteRequest>>({});

  const preferences = useViewPreferences(
    scope,
    vaultKey,
    view ? nativeMigrations(view, vaultKey) : [],
  );

  const state: ViewExecuteRequest = {
    ...(transientByKey[key] ?? stored),
    ...durableRequest(preferences.values),
  };

  const setState = useCallback(
    (next: ViewExecuteRequest) => {
      if (!view || !scope) return;

      const transient = Object.fromEntries(
        Object.entries(next).filter(
          ([field]) => !DURABLE_QUERY_FIELDS.some((durable) => durable === field),
        ),
      );

      try {
        window.sessionStorage.setItem(key, JSON.stringify(transient));
      } catch {}

      setTransient((current) => ({ ...current, [key]: transient }));
      const set: JsonObject = {};
      const unset: string[] = [];

      for (const field of DURABLE_QUERY_FIELDS) {
        if (JSON.stringify(next[field]) === JSON.stringify(state[field])) continue;
        const value = next[field];

        const defaultValue =
          field === "columnField" || field === "laneField"
            ? view.variants.kanban?.[field]
            : view.defaults[field];

        if (value === undefined || JSON.stringify(value) === JSON.stringify(defaultValue))
          unset.push(`native.${field}`);
        else if (validNativePreference(field, value) && isJsonValue(value))
          set[`native.${field}`] = value;
      }

      if (preferences.store && (Object.keys(set).length || unset.length))
        void preferences.store.patch({ set, unset }).catch(() => {});
    },
    [key, preferences.store, scope, state, view],
  );

  return [
    state,
    setState,
    { ...preferences, error: preferences.error ?? nativePreferenceError(preferences.values) },
  ] as const;
}

/** A sparse personal layout override. Null keeps the authored configuration. */
export function useStoredViewChoice<T>(
  kind: LayoutField,
  vaultKey: string | null,
  view: ViewCatalogEntry,
  guard: (value: unknown) => value is T,
  context?: ViewContext,
  slot?: string,
): [T | null, (next: T) => void] {
  const scope = nativePreferenceScope(view, context, slot);
  const localKey = preferenceScopeKey(scope);
  const [localChoices, setLocalChoices] = useState<Record<string, T>>({});

  const preference = useScopedViewPreference<T | null>(scope, vaultKey, `native.${kind}`, {
    defaultValue: null,
    validate: (value): value is T | null => value === null || guard(value),
  });

  return [
    vaultKey === null ? (localChoices[localKey] ?? null) : preference.value,
    (next) => {
      if (vaultKey === null) {
        setLocalChoices((current) => ({ ...current, [localKey]: next }));

        return;
      }

      const defaultValue =
        kind === "columns"
          ? view.variants.table?.columns?.map((column) => column.field)
          : kind === "density"
            ? (view.variants.table?.density ?? "two-line")
            : kind === "reshown"
              ? []
              : {};

      const followsDefault =
        JSON.stringify(next) === JSON.stringify(defaultValue) ||
        (kind === "columns" && Array.isArray(next) && next.length === 0);

      void (followsDefault ? preference.reset() : preference.set(next)).catch(() => {});
    },
  ];
}
