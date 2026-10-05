import { jsonReply, type FakeFetch, type FakeFetchRequest } from "../test/fakeFetch";
import type { JsonObject } from "../api/parse";
import {
  canonicalPreferenceScope,
  preferenceScopeKey,
  preferenceFamilyKey,
  type ViewPreferenceResponse,
  type ViewPreferenceScope,
} from "./types";

/** Synthetic protocol fixture with revisions, sparse patches, and globally claimed migration sources. */
export function preferenceServer(http: FakeFetch) {
  const rows = new Map<string, ViewPreferenceResponse>();
  const migrations = new Set<string>();
  const resetFamilies = new Map<string, number>();
  const writes: { scope: ViewPreferenceScope; set?: JsonObject; unset?: string[] }[] = [];
  let failure: string | null = null;
  let onReset = (_scope: ViewPreferenceScope, _includeSlots: boolean) => {};

  const read = (scope: ViewPreferenceScope) => {
    const canonical = canonicalPreferenceScope(scope);
    const key = preferenceScopeKey(canonical);
    let row = rows.get(key);

    if (!row) {
      row = {
        scope: canonical,
        revision: resetFamilies.get(preferenceFamilyKey(canonical)) ?? 0,
        values: {},
        migrationClosed: resetFamilies.has(preferenceFamilyKey(canonical)),
      };
      rows.set(key, row);
    }

    return row;
  };

  // SAFETY: This fixture receives bodies produced by the typed preference API wrappers.
  const body = (request: FakeFetchRequest) =>
    JSON.parse(request.body ?? "{}") as {
      scope: ViewPreferenceScope;
      expectedRevision: number;
      set?: JsonObject;
      unset?: string[];
      migrationId?: string;
      includeSlots?: boolean;
      values?: JsonObject;
    };

  // SAFETY: The typed client serializes a ViewPreferenceScope into this query parameter.
  http.on("GET", "/api/v1/view-preferences", (request) =>
    jsonReply(read(JSON.parse(request.query.get("scope") ?? "{}") as ViewPreferenceScope)),
  );
  http.on("PATCH", "/api/v1/view-preferences", (request) => {
    const input = body(request);
    const current = read(input.scope);

    if (failure) return jsonReply({ error: failure }, 500);

    if (input.expectedRevision !== current.revision)
      return jsonReply(
        { error: "view preferences changed", code: "CONFLICT", details: { current } },
        409,
      );
    const values = { ...current.values, ...input.set };

    for (const key of input.unset ?? []) delete values[key];
    const next = { ...current, revision: current.revision + 1, values, migrationClosed: true };
    rows.set(preferenceScopeKey(input.scope), next);
    writes.push(input);

    return jsonReply(next);
  });
  http.on("POST", "/api/v1/view-preferences/reset", (request) => {
    const input = body(request);
    const current = read(input.scope);

    if (failure) return jsonReply({ error: failure }, 500);

    if (input.expectedRevision !== current.revision)
      return jsonReply(
        { error: "view preferences changed", code: "CONFLICT", details: { current } },
        409,
      );
    const next = { ...current, revision: current.revision + 1, values: {}, migrationClosed: true };

    if (input.includeSlots) {
      const familyKey = preferenceFamilyKey(input.scope);
      resetFamilies.set(familyKey, (resetFamilies.get(familyKey) ?? 0) + 1);

      for (const [key, row] of rows)
        if (preferenceFamilyKey(row.scope) === familyKey)
          rows.set(key, { ...row, revision: row.revision + 1, values: {}, migrationClosed: true });
    }

    rows.set(preferenceScopeKey(input.scope), next);
    onReset(input.scope, input.includeSlots === true);

    return jsonReply(next);
  });
  http.on("POST", "/api/v1/view-preferences/import", (request) => {
    const input = body(request);
    const current = read(input.scope);
    const imported = !migrations.has(input.migrationId!) && !current.migrationClosed;
    migrations.add(input.migrationId!);

    const next = imported
      ? {
          ...current,
          revision: current.revision + 1,
          values: { ...input.values, ...current.values },
        }
      : current;

    rows.set(preferenceScopeKey(input.scope), next);

    return jsonReply({ ...next, imported });
  });

  return {
    read,
    writes,
    failWrites: (message: string | null) => {
      failure = message;
    },
    onReset: (listener: (scope: ViewPreferenceScope, includeSlots: boolean) => void) => {
      onReset = listener;
    },
    change: (scope: ViewPreferenceScope, values: JsonObject) => {
      const current = read(scope);
      rows.set(preferenceScopeKey(scope), { ...current, values, revision: current.revision + 1 });
    },
  };
}
