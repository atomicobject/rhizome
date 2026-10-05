import { useMemo } from "react";

import { isStringArray } from "../api/parse";
import type {
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewTableColumn,
} from "../api/types";
import { useStoredViewChoice } from "./useViewState";
import type { ViewContext } from "../views/context";
import { useViewPreferences } from "../viewPreferences/hooks";
import {
  nativeMigrations,
  nativePreferenceScope,
  nativePreferenceError,
  validNativePreference,
} from "../viewPreferences/native";

export type RowDensity = "two-line" | "one-line";

function isRowDensity(value: unknown): value is RowDensity {
  return value === "two-line" || value === "one-line";
}

function isColumnWidths(value: unknown): value is Record<string, number> {
  return validNativePreference("widths", value);
}

/**
 * Layout overrides for one view instance (SPEC-0114). Absent choices follow
 * the view's shared configuration.
 */
export function useViewLayout(
  view: ViewCatalogEntry,
  vaultKey: string | null,
  execution: ViewExecuteResponse | null,
  defaultColumns: ViewTableColumn[],
  columns: ViewTableColumn[],
  filters: ViewExecuteRequest["filters"],
  context?: ViewContext,
  slot?: string,
) {
  const preferences = useViewPreferences(
    nativePreferenceScope(view, context, slot),
    vaultKey,
    nativeMigrations(view, vaultKey),
  );

  const [chosenFields, setChosenFields] = useStoredViewChoice(
    "columns",
    vaultKey,
    view,
    isStringArray,
    context,
    slot,
  );

  // Empty columns the reader showed again, kept apart from the column set so
  // the other empty columns still hide and show as rows fill them.
  const [reshownChoice, setReshown] = useStoredViewChoice(
    "reshown",
    vaultKey,
    view,
    isStringArray,
    context,
    slot,
  );

  const [densityChoice, setDensity] = useStoredViewChoice(
    "density",
    vaultKey,
    view,
    isRowDensity,
    context,
    slot,
  );

  const [columnWidths, setColumnWidths] = useStoredViewChoice(
    "widths",
    vaultKey,
    view,
    isColumnWidths,
    context,
    slot,
  );

  const profile = execution?.profile;
  const stats = execution?.stats;
  const summaryField = profile?.summaryField;

  const density: RowDensity = summaryField
    ? (densityChoice ?? view.variants.table?.density ?? "two-line")
    : "one-line";

  const emptyFields = useMemo(
    () =>
      emptyColumnFields(defaultColumns, stats, profile?.lifecycleField).filter(
        // A column the reader filters on stays, even when the filter empties it.
        (field) => !filters?.some((filter) => filter.field === field),
      ),
    [defaultColumns, filters, profile?.lifecycleField, stats],
  );

  const reshown = reshownChoice ?? [];
  const availableFields = columns.map((column) => column.field);
  const chosen = chosenFields?.filter((field) => availableFields.includes(field)) ?? [];

  // Chosen fields the view no longer offers drop out; none left means the
  // defaults. Either way, columns no matching row fills hide unless re-shown.
  const baseFields =
    chosen.length > 0
      ? chosen
      : defaultColumns.flatMap(({ field }) => (field === "validationIssues" ? [] : [field]));

  const visibleFields = baseFields.filter(
    (field) => !emptyFields.includes(field) || reshown.includes(field),
  );

  const toggleColumn = (field: string) => {
    const empty = emptyFields.includes(field);

    if (!visibleFields.includes(field)) {
      if (!baseFields.includes(field)) setChosenFields([...baseFields, field]);

      if (empty) setReshown([...reshown, field]);

      return;
    }

    if (visibleFields.length === 1) return;
    setReshown(reshown.filter((item) => item !== field));

    // An empty column goes back to hiding while empty; a filled one leaves the set.
    if (!empty) setChosenFields(baseFields.filter((item) => item !== field));
  };

  return {
    visibleFields,
    toggleColumn,
    emptyFields,
    /** The reader's column set; empty while they keep the view's. Save writes it. */
    chosenFields: chosen,
    /** Forgets the column choices once the saved columns come from the view file. */
    resetColumns: () => {
      if (preferences.store)
        void preferences.store
          .patch({ unset: ["native.columns", "native.reshown"] })
          .catch(() => {});
      else {
        setChosenFields([]);
        setReshown([]);
      }
    },
    density,
    densityChoice,
    setDensity,
    columnWidths,
    setColumnWidths,
    preferences: {
      ...preferences,
      error: preferences.error ?? nativePreferenceError(preferences.values),
    },
  };
}

/**
 * Generated columns no matching row fills, which the table hides by default;
 * the title and lifecycle always show.
 */
function emptyColumnFields(
  columns: ViewTableColumn[],
  stats: ViewExecuteResponse["stats"],
  lifecycleField: string | undefined,
) {
  const filled = new Map(stats?.fields?.map((item) => [item.field, item.filled]));

  return columns.flatMap((column) =>
    filled.get(column.field) === 0 && column.field !== "title" && column.field !== lifecycleField
      ? [column.field]
      : [],
  );
}
