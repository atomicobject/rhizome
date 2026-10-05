import { ApiError, fetchJSON } from "../api/client";
import { isJsonObject } from "../api/parse";
import {
  isPreferenceResponse,
  preferenceScopeKey,
  type PreferenceMigration,
  type PreferencePatch,
  type ViewPreferenceResponse,
  type ViewPreferenceScope,
} from "./types";

type PreferenceRequestBody = PreferencePatch & {
  scope: ViewPreferenceScope;
  expectedRevision?: number;
  migrationId?: string;
  includeSlots?: boolean;
  values?: PreferenceMigration["values"];
};

const ROUTE = "/api/v1/view-preferences";

async function request(
  scope: ViewPreferenceScope,
  suffix = "",
  body?: PreferenceRequestBody,
  method = "POST",
) {
  const value: unknown = await fetchJSON(
    `${ROUTE}${suffix}${body ? "" : `?scope=${encodeURIComponent(JSON.stringify(scope))}`}`,
    body
      ? { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }
      : undefined,
  );

  if (!isPreferenceResponse(value) || preferenceScopeKey(value.scope) !== preferenceScopeKey(scope))
    throw new Error("Invalid view preferences response");

  return value;
}

export const readViewPreferences = (scope: ViewPreferenceScope) => request(scope);

export const patchViewPreferences = (
  scope: ViewPreferenceScope,
  expectedRevision: number,
  patch: PreferencePatch,
) => request(scope, "", { scope, expectedRevision, ...patch }, "PATCH");

export const resetViewPreferences = (
  scope: ViewPreferenceScope,
  expectedRevision: number,
  includeSlots = false,
) => request(scope, "/reset", { scope, expectedRevision, includeSlots });

export const importViewPreferences = (scope: ViewPreferenceScope, migration: PreferenceMigration) =>
  request(scope, "/import", {
    scope,
    migrationId: migration.migrationId,
    values: migration.values,
  });

export function preferenceConflict(
  cause: unknown,
  scope: ViewPreferenceScope,
): ViewPreferenceResponse | null {
  if (!(cause instanceof ApiError) || cause.status !== 409 || !isJsonObject(cause.details))
    return null;
  const current = cause.details.current;

  return isPreferenceResponse(current) &&
    preferenceScopeKey(current.scope) === preferenceScopeKey(scope)
    ? current
    : null;
}
