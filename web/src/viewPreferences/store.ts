import { isCallable, type JsonObject } from "../api/parse";
import { ApiError } from "../api/client";
import {
  importViewPreferences,
  patchViewPreferences,
  preferenceConflict,
  readViewPreferences,
  resetViewPreferences,
} from "./api";
import {
  canonicalPreferenceScope,
  preferenceScopeKey,
  preferenceFamilyKey,
  type PreferenceMigration,
  type PreferencePatch,
  type ViewPreferenceResponse,
  type ViewPreferenceScope,
} from "./types";

export type PreferenceStoreSnapshot = {
  values: JsonObject;
  loading: boolean;
  pending: boolean;
  error: Error | null;
  revision: number;
};

type Operation = {
  patch: (values: JsonObject) => PreferencePatch;
  reset?: boolean;
  includeSlots?: boolean;
  resolve: () => void;
  reject: (error: Error) => void;
};

const asError = (cause: unknown) => (cause instanceof Error ? cause : new Error(String(cause)));

function applyPatch(values: JsonObject, patch: PreferencePatch): JsonObject {
  const next = { ...values, ...patch.set };

  for (const key of patch.unset ?? []) delete next[key];

  return next;
}

/** One queue and optimistic snapshot per instance, shared by every mounted consumer. */
export class PreferenceStore {
  readonly scope: ViewPreferenceScope;
  readonly vaultKey: string;
  private base: ViewPreferenceResponse;
  private snapshot: PreferenceStoreSnapshot;
  private listeners = new Set<() => void>();
  private operations: Operation[] = [];
  private migrations = new Map<string, PreferenceMigration>();
  private acknowledgedMigrations = new Set<string>();
  private rejectedMigrations = new Set<string>();
  private migrationError: Error | null = null;
  private hydrated = false;
  private blocked = false;
  private processing: Promise<void> | null = null;
  private reading: Promise<void> | null = null;
  private readAgain = false;
  private error: Error | null = null;
  private writeEpoch = 0;
  private baseVersion = 0;

  constructor(scope: ViewPreferenceScope, vaultKey: string, migrations: PreferenceMigration[]) {
    this.scope = canonicalPreferenceScope(scope);
    this.vaultKey = vaultKey;
    this.base = { scope: this.scope, revision: 0, values: {}, migrationClosed: false };
    this.snapshot = { values: {}, revision: 0, loading: true, pending: false, error: null };
    this.addMigrations(migrations);
    void this.refresh().catch(() => {});
  }

  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => {
    const wasInactive = this.listeners.size === 0;
    this.listeners.add(listener);

    if (wasInactive && this.hydrated) void this.refresh(true).catch(() => {});

    return () => {
      this.listeners.delete(listener);
    };
  };
  isActive = () => this.listeners.size > 0;
  isResettingFamily = () => this.operations[0]?.includeSlots === true;

  addMigrations(migrations: PreferenceMigration[]) {
    let added = false;

    for (const migration of migrations)
      if (
        !this.migrations.has(migration.migrationId) &&
        !this.acknowledgedMigrations.has(migration.migrationId) &&
        !this.rejectedMigrations.has(migration.migrationId)
      ) {
        this.migrations.set(migration.migrationId, migration);
        added = true;
      }

    if (added && this.hydrated) void this.refresh(true).catch(() => {});
  }

  private emit() {
    let values = this.base.values;

    for (const operation of this.operations) {
      try {
        values = operation.reset ? {} : applyPatch(values, operation.patch(values));
      } catch (cause) {
        // Rebased updates can fail validation. Publish that error using the valid
        // snapshot; the write queue rejects the operation and retains it for reset/retry.
        this.error = asError(cause);
      }
    }

    this.snapshot = {
      values,
      revision: this.base.revision,
      loading: !this.hydrated && !this.error,
      pending: this.operations.length > 0,
      error: this.error,
    };

    for (const listener of this.listeners) listener();
  }

  private adopt(response: ViewPreferenceResponse, readVersion?: number) {
    if (readVersion !== undefined && readVersion !== this.baseVersion) {
      this.readAgain = true;

      return;
    }

    if (readVersion !== undefined || response.revision >= this.base.revision) this.base = response;
    this.baseVersion++;
  }

  refresh = (afterCurrent = false): Promise<void> => {
    if (this.reading) {
      if (afterCurrent) this.readAgain = true;

      return this.reading;
    }

    this.reading = (async () => {
      do {
        this.readAgain = false;
        await this.read();
      } while (this.readAgain);
    })().finally(() => {
      this.reading = null;
    });

    return this.reading;
  };

  private async read() {
    try {
      const readVersion = this.baseVersion;
      this.adopt(await readViewPreferences(this.scope), readVersion);

      for (const migration of this.migrations.values()) {
        try {
          const importVersion = this.baseVersion;
          this.adopt(await importViewPreferences(this.scope, migration), importVersion);
          migration.acknowledged?.();
          this.acknowledgedMigrations.add(migration.migrationId);
          this.migrations.delete(migration.migrationId);
        } catch (cause) {
          if (!(cause instanceof ApiError) || ![400, 413, 422].includes(cause.status)) throw cause;
          // Keep the browser source intact, but stop one invalid source from blocking
          // durable preferences. A new page may reconsider it after the source is repaired.
          this.rejectedMigrations.add(migration.migrationId);
          this.migrations.delete(migration.migrationId);
          this.migrationError = new Error(
            `Could not import saved browser preferences: ${cause.message}`,
          );
        }
      }

      this.hydrated = true;

      if (!this.blocked) this.error = this.migrationError;
    } catch (cause) {
      this.error = asError(cause);
      throw this.error;
    } finally {
      this.emit();
    }
  }

  patch = (patch: PreferencePatch | ((values: JsonObject) => PreferencePatch)): Promise<void> =>
    this.enqueue(isCallable(patch) ? patch : () => patch);

  prepareReset = async (): Promise<void> => {
    await this.processing?.catch(() => {});
    this.discardPending();
  };

  discardPending = (): void => {
    this.writeEpoch++;
    const discarded = new Error("Unsaved preferences were reset");

    for (const operation of this.operations) operation.reject(discarded);
    this.operations = [];
    this.blocked = false;
    this.error = null;
    this.migrationError = null;
    this.emit();
  };

  resetAll = async (): Promise<void> => {
    await this.prepareReset();
    await this.enqueue(() => ({}), true);
  };

  resetFamily = async (): Promise<void> => {
    const family = [...stores.values()].filter(
      (store) =>
        store.vaultKey === this.vaultKey &&
        preferenceFamilyKey(store.scope) === preferenceFamilyKey(this.scope),
    );

    await Promise.all(family.map((store) => store.prepareReset()));
    const baseScope = canonicalPreferenceScope(this.scope);
    delete baseScope.widgetSlot;
    const base = getPreferenceStore(baseScope, this.vaultKey);
    await base.enqueue(() => ({}), true, true);

    for (const store of family) if (store !== base) store.discardPending();
    await Promise.all(family.flatMap((store) => (store === base ? [] : [store.refresh(true)])));
  };

  private enqueue(patch: Operation["patch"], reset = false, includeSlots = false): Promise<void> {
    patch(this.snapshot.values);

    const result = new Promise<void>((resolve, reject) => {
      this.operations.push({ patch, reset, includeSlots, resolve, reject });

      if (this.blocked)
        reject(this.error ?? new Error("Preferences have unsaved changes; retry is required"));
    });

    this.emit();

    if (!this.blocked) void this.drain().catch(() => {});

    return result;
  }

  private drain(): Promise<void> {
    if (this.processing) return this.processing;
    this.processing = this.write().finally(() => {
      this.processing = null;

      if (this.operations.length && !this.blocked) void this.drain().catch(() => {});
    });

    return this.processing;
  }

  private async write() {
    const epoch = this.writeEpoch;

    try {
      if (!this.hydrated) await this.refresh();

      while (this.operations.length && !this.blocked && epoch === this.writeEpoch) {
        const operation = this.operations[0];
        let committed = false;

        for (let attempt = 0; attempt < 3; attempt++) {
          try {
            const response = operation.reset
              ? await resetViewPreferences(this.scope, this.base.revision, operation.includeSlots)
              : await patchViewPreferences(
                  this.scope,
                  this.base.revision,
                  operation.patch(this.base.values),
                );

            if (epoch !== this.writeEpoch) return;
            this.adopt(response);
            committed = true;
            break;
          } catch (cause) {
            if (epoch !== this.writeEpoch) return;
            const current = preferenceConflict(cause, this.scope);

            if (!current || attempt === 2) throw cause;
            // A conflict carries the server's authoritative revision, including after database replacement.
            this.base = current;
            this.baseVersion++;
            this.emit();
          }
        }

        if (committed) {
          this.operations.shift();
          this.error = null;
          this.emit();
          operation.resolve();
        }
      }
    } catch (cause) {
      if (epoch !== this.writeEpoch) return;
      this.blocked = true;
      this.error = asError(cause);

      for (const operation of this.operations) operation.reject(this.error);
      this.emit();
      throw this.error;
    }
  }

  retry = async (): Promise<void> => {
    await this.processing?.catch(() => {});
    this.blocked = false;
    this.error = null;
    this.migrationError = null;
    this.emit();

    try {
      await this.refresh();
      await this.drain();
    } catch (cause) {
      this.blocked = true;
      this.error = asError(cause);
      this.emit();
      throw this.error;
    }
  };
}

const stores = new Map<string, PreferenceStore>();

export function getPreferenceStore(
  scope: ViewPreferenceScope,
  vaultKey: string,
  migrations: PreferenceMigration[] = [],
) {
  const key = JSON.stringify([vaultKey, preferenceScopeKey(scope)]);
  let store = stores.get(key);

  if (!store) {
    store = new PreferenceStore(scope, vaultKey, migrations);
    stores.set(key, store);
  } else store.addMigrations(migrations);

  return store;
}

export function refreshPreferenceStores(
  scope?: ViewPreferenceScope,
  vaultKey?: string,
  includeSlots = false,
) {
  for (const store of stores.values())
    if (
      store.isActive() &&
      (vaultKey === undefined || store.vaultKey === vaultKey) &&
      (!scope ||
        (includeSlots
          ? preferenceFamilyKey(store.scope) === preferenceFamilyKey(scope)
          : preferenceScopeKey(store.scope) === preferenceScopeKey(scope)))
    )
      void store.refresh(true).catch(() => {});
}

export function invalidatePreferenceFamily(scope: ViewPreferenceScope, vaultKey?: string) {
  for (const store of stores.values())
    if (
      (vaultKey === undefined || store.vaultKey === vaultKey) &&
      preferenceFamilyKey(store.scope) === preferenceFamilyKey(scope)
    ) {
      if (!store.isResettingFamily()) store.discardPending();
      void store.refresh(true).catch(() => {});
    }
}

/** Test harness isolation; application code keeps stores for its window's lifetime. */
export function clearPreferenceStores() {
  stores.clear();
}
