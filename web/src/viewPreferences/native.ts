import {
  decodeJson,
  isFiniteNumber,
  isBoolean,
  isJsonObject,
  isOptionalString,
  isString,
  isStringArray,
  type JsonObject,
} from "../api/parse";
import type { ViewCatalogEntry, ViewExecuteRequest } from "../api/types";
import type { ViewContext } from "../views/context";
import type { PreferenceMigration, ViewPreferenceScope } from "./types";

export const DURABLE_QUERY_FIELDS = [
  "filters",
  "sort",
  "group",
  "filterPreset",
  "columnField",
  "laneField",
] as const;

export const LAYOUT_FIELDS = ["columns", "reshown", "density", "widths"] as const;

export const NATIVE_EXPANSION_KEYS = [
  "native.tableGroups",
  "native.cardGroups",
  "native.boardColumns",
  "native.boardLanes",
] as const;

export type LayoutField = (typeof LAYOUT_FIELDS)[number];

export function viewStorageKey(
  kind: "state" | LayoutField,
  vaultKey: string | null,
  viewID: string,
) {
  return `rhizome:view:${kind}:v2:${encodeURIComponent(vaultKey ?? "")}:${viewID}`;
}

export function nativePreferenceScope(
  view: ViewCatalogEntry,
  context?: ViewContext,
  slot?: string,
): ViewPreferenceScope {
  const mount = view.mount;

  const inferred: ViewContext =
    mount.kind === "type" && mount.type
      ? { kind: "type", type: mount.type }
      : mount.kind === "interface" && mount.interface
        ? { kind: "interface", interface: mount.interface }
        : mount.kind === "group" && mount.group
          ? { kind: "group", group: mount.group }
          : { kind: "standalone" };

  const scope: ViewPreferenceScope = { viewId: view.id, context: context ?? inferred };

  if (slot) scope.slot = slot;

  return scope;
}

export function validNativePreference(field: string, value: unknown): value is JsonObject[string] {
  switch (field) {
    case "columns":
    case "reshown":
      return isStringArray(value);
    case "density":
      return value === "two-line" || value === "one-line";
    case "widths":
      return (
        isJsonObject(value) &&
        Object.values(value).every((item) => isFiniteNumber(item) && item > 0)
      );
    case "tableGroups":
    case "cardGroups":
    case "boardColumns":
    case "boardLanes":
      return (
        isJsonObject(value) &&
        Object.values(value).every(
          (choices) => isJsonObject(choices) && Object.values(choices).every(isBoolean),
        )
      );
    case "sort":
      return (
        Array.isArray(value) &&
        value.every(
          (item) =>
            isJsonObject(item) &&
            isString(item.field) &&
            !!item.field.trim() &&
            (item.direction === undefined ||
              item.direction === "" ||
              item.direction === "asc" ||
              item.direction === "desc"),
        )
      );
    case "filters":
      return (
        Array.isArray(value) &&
        value.every(
          (item) =>
            isJsonObject(item) &&
            isString(item.field) &&
            !!item.field.trim() &&
            isString(item.op) &&
            ["eq", "neq", "in", "contains", "gt", "gte", "lt", "lte", "exists", "missing"].includes(
              item.op,
            ) &&
            (item.values === undefined || Array.isArray(item.values)) &&
            (item.op === "exists" ||
              item.op === "missing" ||
              (item.op === "in"
                ? Array.isArray(item.values) && item.values.length > 0
                : item.value !== undefined &&
                  item.value !== null &&
                  String(item.value).trim() !== "")),
        )
      );
    case "group":
      return (
        isJsonObject(value) &&
        isOptionalString(value.field) &&
        (value.fields === undefined ||
          (isStringArray(value.fields) && value.fields.every((field) => !!field.trim()))) &&
        (value.values === undefined ||
          (Array.isArray(value.values) &&
            value.values.every(
              (item) =>
                isJsonObject(item) &&
                isOptionalString(item.field) &&
                isString(item.value) &&
                isOptionalString(item.label) &&
                (item.order === undefined || isFiniteNumber(item.order)) &&
                (item.collapsedByDefault === undefined || isBoolean(item.collapsedByDefault)),
            ))) &&
        (value.bucket === undefined || value.bucket === "month")
      );
    case "filterPreset":
    case "columnField":
    case "laneField":
      return isString(value);
    default:
      return false;
  }
}

export function nativePreferenceError(values: JsonObject): Error | null {
  const keys = [
    ...[...DURABLE_QUERY_FIELDS, ...LAYOUT_FIELDS].map((field) => `native.${field}`),
    ...NATIVE_EXPANSION_KEYS,
  ];

  for (const key of keys)
    if (
      Object.hasOwn(values, key) &&
      !validNativePreference(key.slice("native.".length), values[key])
    )
      return new Error(`Invalid saved preference: ${key}`);

  return null;
}

export function durableRequest(values: JsonObject): ViewExecuteRequest {
  const request: ViewExecuteRequest = {};

  for (const field of DURABLE_QUERY_FIELDS) {
    const value = values[`native.${field}`];

    if (value !== undefined && validNativePreference(field, value))
      Object.assign(request, { [field]: value });
  }

  return request;
}

export function readBrowserItem(storage: () => Storage, key: string) {
  try {
    return storage().getItem(key);
  } catch {
    return null;
  }
}

export function nativeMigrations(
  view: ViewCatalogEntry,
  vaultKey: string | null,
): PreferenceMigration[] {
  if (vaultKey === null) return [];
  const migrations: PreferenceMigration[] = [];
  const stateKey = viewStorageKey("state", vaultKey, view.id);

  const state = decodeJson(
    readBrowserItem(() => window.sessionStorage, stateKey),
    isJsonObject,
  );

  if (state) {
    const values: JsonObject = {};

    for (const field of DURABLE_QUERY_FIELDS)
      if (validNativePreference(field, state[field])) values[`native.${field}`] = state[field];
    migrations.push({
      migrationId: stateKey,
      values,
      acknowledged: () => {
        try {
          window.sessionStorage.removeItem(stateKey);
        } catch {}
      },
    });
  }

  for (const field of LAYOUT_FIELDS) {
    const key = viewStorageKey(field, vaultKey, view.id);
    const raw = readBrowserItem(() => window.localStorage, key);

    const value = decodeJson(raw, (item): item is JsonObject[string] =>
      validNativePreference(field, item),
    );

    if (value !== null)
      migrations.push({
        migrationId: key,
        values: { [`native.${field}`]: value },
        acknowledged: () => {
          try {
            window.localStorage.removeItem(key);
          } catch {}
        },
      });
  }

  return migrations;
}
