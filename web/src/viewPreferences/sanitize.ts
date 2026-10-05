import { isJsonObject, isString, type JsonObject, type JsonValue } from "../api/parse";
import type { ViewCatalogEntry, ViewExecuteResponse } from "../api/types";
import { validNativePreference } from "./native";
import type { PreferencePatch } from "./types";

const BUILTIN_FIELDS = ["title", "path", "resolvedType", "updatedAt", "hasIssues", "tags"];

/** Remove only references a fresh schema-backed baseline proves no longer exist. */
export function sanitizeNativePreferences(
  values: JsonObject,
  view: ViewCatalogEntry,
  baseline: ViewExecuteResponse,
) {
  const effective = { ...values };
  const set: JsonObject = {};
  const unset: string[] = [];
  const known = new Set(BUILTIN_FIELDS);

  for (const capability of baseline.capabilities ?? []) {
    known.add(capability.key);

    if (capability.canonicalField) known.add(capability.canonicalField);

    for (const alias of capability.sourceKeys ?? []) known.add(alias);
  }

  // Authored selectors can use bare aliases for row-observed metadata.
  for (const field of [
    ...(view.defaults.filters ?? []).map((filter) => filter.field),
    ...(view.defaults.sort ?? []).map((sort) => sort.field),
    view.defaults.group?.field,
    ...(view.defaults.group?.fields ?? []),
    ...(view.variants.table?.columns ?? []).map((column) => column.field),
    view.variants.kanban?.columnField,
    view.variants.kanban?.laneField,
  ])
    if (field) known.add(field);

  // Schema capabilities list every field the type declares, so a bare name
  // missing from them is gone. Dotted frontmatter and inline keys are observed
  // only on the rows a baseline happened to read, so their absence proves nothing.
  const schemaBacked =
    ["ontology_type", "ontology_interface"].includes(view.source.kind) &&
    (baseline.capabilities ?? []).some((capability) => capability.source === "schema");

  const exists = (field: string, queryFilter = false) =>
    known.has(field) || field.includes(".") || (queryFilter && field === "stale");

  const replace = (key: string, value: JsonValue | undefined) => {
    if (JSON.stringify(values[key]) === JSON.stringify(value)) return;

    if (value === undefined) {
      delete effective[key];
      unset.push(key);
    } else {
      effective[key] = value;
      set[key] = value;
    }
  };

  const cleanArray = (key: string, keep: (item: JsonValue) => boolean) => {
    const current = values[key];

    if (!Array.isArray(current)) return;
    const remaining = current.filter(keep);

    if (remaining.length !== current.length) replace(key, remaining.length ? remaining : undefined);
  };

  if (schemaBacked) {
    for (const field of ["sort", "filters"])
      cleanArray(
        `native.${field}`,
        (item) =>
          isJsonObject(item) && isString(item.field) && exists(item.field, field === "filters"),
      );

    for (const field of ["columns", "reshown"])
      cleanArray(
        `native.${field}`,
        (item) => isString(item) && (exists(item) || item === "validationIssues"),
      );

    for (const field of ["columnField", "laneField"]) {
      const value = values[`native.${field}`];

      if (isString(value) && value !== "" && value !== "none" && !exists(value))
        replace(`native.${field}`, undefined);
    }

    const group = values["native.group"];

    if (isJsonObject(group) && validNativePreference("group", group)) {
      const next = { ...group };

      const fields = Array.isArray(group.fields)
        ? group.fields.filter((field) => isString(field) && (field === "none" || exists(field)))
        : null;

      if (fields) next.fields = fields;

      if (
        isString(group.field) &&
        group.field !== "" &&
        group.field !== "none" &&
        !exists(group.field)
      )
        delete next.field;

      if (Array.isArray(group.values))
        next.values = group.values.filter(
          (item) =>
            isJsonObject(item) &&
            (item.field === undefined || (isString(item.field) && exists(item.field))),
        );
      const hadFields = (Array.isArray(group.fields) && group.fields.length > 0) || !!group.field;
      const hasFields = (Array.isArray(next.fields) && next.fields.length > 0) || !!next.field;
      replace("native.group", hadFields && !hasFields ? undefined : next);
    }

    const widths = values["native.widths"];

    if (isJsonObject(widths)) {
      const remaining = Object.fromEntries(
        Object.entries(widths).filter(([field]) => exists(field) || field === "validationIssues"),
      );

      replace("native.widths", Object.keys(remaining).length ? remaining : undefined);
    }
  }

  const preset = values["native.filterPreset"];

  if (isString(preset) && preset) {
    const presets = view.definition.filterPresets;

    const available = Array.isArray(presets)
      ? presets.some((item) => isJsonObject(item) && item.id === preset)
      : false;

    if (!available) replace("native.filterPreset", undefined);
  }

  const patch: PreferencePatch = { set, unset };

  return { values: effective, patch, changed: Object.keys(set).length > 0 || unset.length > 0 };
}
