import { useMemo, useSyncExternalStore } from "react";
import { isJsonValue } from "../api/parse";
import { connectPreferenceEvents } from "./events";
import { getPreferenceStore, type PreferenceStore, type PreferenceStoreSnapshot } from "./store";
import { isPreferenceValue, type PreferenceMigration, type ViewPreferenceScope } from "./types";

export type PreferenceDefinition<T> = {
  defaultValue: T;
  validate(value: unknown): value is T;
};

export type PreferenceSnapshot<T> = Omit<PreferenceStoreSnapshot, "values" | "revision"> & {
  value: T;
  overridden: boolean;
};

export type PreferenceAccessor<T> = {
  getSnapshot(): PreferenceSnapshot<T>;
  subscribe(listener: () => void): () => void;
  set(value: T): Promise<void>;
  update(fn: (current: T) => T): Promise<void>;
  reset(): Promise<void>;
  retry(): Promise<void>;
};

export function preferenceValue<T>(
  snapshot: PreferenceStoreSnapshot,
  key: string,
  definition: PreferenceDefinition<T>,
) {
  const stored = snapshot.values[key];
  const overridden = Object.hasOwn(snapshot.values, key);
  const valid = !overridden || definition.validate(stored);

  return {
    value: overridden && definition.validate(stored) ? stored : definition.defaultValue,
    overridden: overridden && valid,
    error: snapshot.error ?? (valid ? null : new Error(`Invalid saved preference: ${key}`)),
  };
}

export function createPreferenceAccessor<T>(
  store: PreferenceStore,
  key: string,
  definition: PreferenceDefinition<T>,
): PreferenceAccessor<T> {
  if (!key.trim()) throw new Error("A preference key is required");
  let previous: PreferenceStoreSnapshot | undefined;
  let selected: PreferenceSnapshot<T>;

  const patch = (value: T) => {
    if (!definition.validate(value) || !isJsonValue(value) || !isPreferenceValue(value))
      throw new Error(`Invalid preference value: ${key}`);

    return JSON.stringify(value) === JSON.stringify(definition.defaultValue)
      ? { unset: [key] }
      : { set: { [key]: value } };
  };

  return {
    getSnapshot: () => {
      const snapshot = store.getSnapshot();

      if (snapshot !== previous) {
        selected = {
          loading: snapshot.loading,
          pending: snapshot.pending,
          ...preferenceValue(snapshot, key, definition),
        };
        previous = snapshot;
      }

      return selected;
    },
    subscribe: store.subscribe,
    set: async (value) => {
      await store.patch(patch(value));
    },
    update: async (fn) => {
      await store.patch((values) =>
        patch(fn(preferenceValue({ ...store.getSnapshot(), values }, key, definition).value)),
      );
    },
    reset: () => store.patch({ unset: [key] }),
    retry: store.retry,
  };
}

export function getScopedViewPreference<T>(
  scope: ViewPreferenceScope,
  vaultKey: string,
  key: string,
  definition: PreferenceDefinition<T>,
) {
  connectPreferenceEvents();

  return createPreferenceAccessor(getPreferenceStore(scope, vaultKey), key, definition);
}

export function useViewPreferences(
  scope: ViewPreferenceScope | null,
  vaultKey: string | null,
  migrations: PreferenceMigration[] = [],
) {
  connectPreferenceEvents();
  const store = scope && vaultKey !== null ? getPreferenceStore(scope, vaultKey, migrations) : null;

  const snapshot = useSyncExternalStore(
    store?.subscribe ?? noSubscribe,
    store?.getSnapshot ?? emptySnapshot,
  );

  return {
    ...snapshot,
    store,
    resetAll: store?.resetAll ?? resolved,
    resetFamily: store?.resetFamily ?? resolved,
    retry: store?.retry ?? resolved,
  };
}

export function useScopedViewPreference<T>(
  scope: ViewPreferenceScope,
  vaultKey: string | null,
  key: string,
  definition: PreferenceDefinition<T>,
) {
  const { store, ...status } = useViewPreferences(scope, vaultKey);

  const accessor = useMemo(
    () => (store ? createPreferenceAccessor(store, key, definition) : null),
    [store, key, definition.defaultValue, definition.validate],
  );

  const absent = useMemo<PreferenceSnapshot<T>>(
    () => ({
      value: definition.defaultValue,
      overridden: false,
      loading: false,
      pending: false,
      error: null,
    }),
    [definition.defaultValue],
  );

  const snapshot = useSyncExternalStore(
    accessor?.subscribe ?? noSubscribe,
    accessor?.getSnapshot ?? (() => absent),
  );

  return {
    ...snapshot,
    set: accessor?.set ?? unavailable,
    update: accessor?.update ?? unavailable,
    reset: accessor?.reset ?? resolved,
    retry: status.retry,
  };
}

const EMPTY: PreferenceStoreSnapshot = {
  values: {},
  loading: false,
  pending: false,
  error: null,
  revision: 0,
};

const emptySnapshot = () => EMPTY;

const noSubscribe = () => () => {};

const resolved = () => Promise.resolve();

const unavailable = () =>
  Promise.reject(new Error("View preferences are unavailable until the vault is known"));
