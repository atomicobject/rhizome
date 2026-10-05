import { useEffect, useRef, useState } from "react";

import { ApiError, savePublicView } from "../api/client";
import type { JsonObject } from "../api/parse";
import type {
  ViewCatalogEntry,
  ViewExecuteRequest,
  ViewExecuteResponse,
  ViewSaveRequest,
  ViewSaveResponse,
  ViewTableColumn,
} from "../api/types";
import { getPreferenceStore } from "../viewPreferences/store";
import type { ViewPreferenceScope } from "../viewPreferences/types";
import { usePopoverDismiss } from "./focusManagement";
import { filterPresets, type ConfiguredViewVariant } from "./ConfiguredViewModel";

/** How long the written path stays beside the Save action. */
const SAVED_NOTICE_MS = 4000;

type SaveState = ViewSaveRequest["state"];

type GroupSpec = NonNullable<ViewExecuteRequest["group"]>;

/** The group field that saves an explicit "no grouping". */
const GROUP_NONE = "none";

type SaveInput = {
  view: ViewCatalogEntry;
  execution: ViewExecuteResponse | null;
  state: ViewExecuteRequest;
  variant: ConfiguredViewVariant;
  /** The reader's row density choice; null keeps the view's. */
  densityChoice: "two-line" | "one-line" | null;
  /** Every column the view offers. */
  columns: ViewTableColumn[];
  /** The view's own column fields, in order. */
  defaultFields: string[];
  /**
   * The reader's column set; empty keeps the view's columns. Columns that
   * hide while empty stay in it, so emptiness never reaches the saved file.
   */
  chosenFields: string[];
};

function viewSaveRequest(input: SaveInput, includeSearch: boolean): ViewSaveRequest {
  const { view, execution, state, variant } = input;

  return {
    definitionFingerprint: execution?.definitionFingerprint,
    state: {
      variant,
      search: includeSearch ? (state.search ?? execution?.state?.search) : view.defaults.search,
      filters: savedFilters(input),
      sort: savedSort(input),
      ...layoutChanges(input),
    },
  };
}

function savedFilters({ view, state }: SaveInput) {
  const preset = filterPresets(view).find((item) => item.id === state.filterPreset);

  return [...(state.filters ?? view.defaults.filters ?? []), ...(preset?.filters ?? [])];
}

/** Not the executed sort: execution normalizes directions the file keeps as written. */
function savedSort({ view, state }: SaveInput) {
  return state.sort ?? view.defaults.sort ?? [];
}

const LAYOUT_LABELS: Record<keyof SaveState, string> = {
  variant: "Default layout",
  search: "Search",
  filters: "Filters",
  sort: "Sort",
  group: "Grouping",
  columns: "Columns",
  density: "Row density",
  columnField: "Board columns",
  laneField: "Board lanes",
};

function changedSettings(input: SaveInput) {
  const { view, state, variant } = input;
  const preset = filterPresets(view).find((item) => item.id === state.filterPreset);
  const changed: string[] = [];

  if (variant !== (view.defaults.variant ?? variant)) changed.push(LAYOUT_LABELS.variant);

  if (JSON.stringify(savedFilters(input)) !== JSON.stringify(view.defaults.filters ?? []))
    changed.push(preset ? `Filters, with the ${preset.label} preset` : LAYOUT_LABELS.filters);

  if (JSON.stringify(savedSort(input)) !== JSON.stringify(view.defaults.sort ?? []))
    changed.push(LAYOUT_LABELS.sort);

  const layout = layoutChanges(input);

  for (const [key, label] of Object.entries(LAYOUT_LABELS))
    if (Object.hasOwn(layout, key)) changed.push(label);

  return changed;
}

function temporarySearch({ view, state }: SaveInput) {
  const search = state.search?.trim() ?? "";

  return search && search !== (view.defaults.search ?? "").trim() ? search : null;
}

/** The layout settings the reader changed from the loaded definition. */
function layoutChanges({
  view,
  state,
  densityChoice,
  columns,
  defaultFields,
  chosenFields,
}: SaveInput): Partial<SaveState> {
  const kanban = view.variants.kanban;
  const changes: Partial<SaveState> = {};

  // Only the reader's own grouping saves, never the board's column grouping.
  if (state.group && groupIdentity(state.group) !== groupIdentity(view.defaults.group)) {
    changes.group = groupFields(state.group).length > 0 ? state.group : { field: GROUP_NONE };
  }

  const fields = chosenFields.filter((field) => field !== "validationIssues");

  if (fields.length > 0 && fields.join() !== defaultFields.join()) {
    changes.columns = fields.flatMap(
      (field) => columns.find((column) => column.field === field) ?? [],
    );
  }

  if (densityChoice && densityChoice !== (view.variants.table?.density ?? "two-line")) {
    changes.density = densityChoice;
  }

  if (state.columnField && state.columnField !== kanban?.columnField) {
    changes.columnField = state.columnField;
  }

  if (state.laneField && state.laneField !== (kanban?.laneField ?? "")) {
    changes.laneField = state.laneField;
  }

  return changes;
}

/** A grouping's fields; `none` as the only field means no grouping. */
function groupFields(group: GroupSpec | undefined) {
  const fields = (group?.fields?.length ? group.fields : [group?.field ?? ""]).filter(Boolean);

  return fields.length === 1 && fields[0] === GROUP_NONE ? [] : fields;
}

/** Compares groupings by what they group on; every form of "no grouping" is one. */
export function groupIdentity(group: GroupSpec | undefined) {
  const fields = groupFields(group);

  return fields.length > 0 ? JSON.stringify([fields, group?.bucket ?? ""]) : "";
}

type SaveEffects = {
  vaultKey: string | null;
  preferenceScope: ViewPreferenceScope | null;
  onViewSaved?: (response: ViewSaveResponse) => Promise<void> | void;
};

function promotedKeys(state: SaveState) {
  return [
    "native.filters",
    "native.filterPreset",
    "native.sort",
    ...(state.group ? ["native.group"] : []),
    ...(state.columns ? ["native.columns", "native.reshown"] : []),
    ...(state.density ? ["native.density"] : []),
    ...(state.columnField ? ["native.columnField"] : []),
    ...(state.laneField ? ["native.laneField"] : []),
  ];
}

/** A save request and what happens once the server acknowledges it. */
export type PreparedSave = {
  body: ViewSaveRequest;
  onSaved: (response: ViewSaveResponse) => Promise<void> | void;
};

/** The Save action's props: what the reader changed and how a save lands. */
export function saveViewProps({
  vaultKey,
  preferenceScope,
  onViewSaved,
  ...input
}: SaveInput & SaveEffects) {
  return {
    viewID: input.view.id,
    dirty: hasUnsavedChanges(input),
    changes: changedSettings(input),
    search: temporarySearch(input),
    prepare: (includeSearch: boolean): PreparedSave => {
      const body = viewSaveRequest(input, includeSearch);

      const store =
        preferenceScope && vaultKey !== null ? getPreferenceStore(preferenceScope, vaultKey) : null;

      const snapshotBeforeSave = store?.getSnapshot().values ?? {};

      return {
        body,
        onSaved: (response) => {
          if (store && preferenceScope && vaultKey !== null) {
            const promoted = promotedKeys(body.state);

            if (response.id === input.view.id) clearPromoted(store, promoted, snapshotBeforeSave);
            else
              carryUnpromoted(
                store,
                getPreferenceStore({ ...preferenceScope, viewId: response.id }, vaultKey),
                promoted,
                snapshotBeforeSave,
              );
          }

          return onViewSaved?.(response);
        },
      };
    },
  };
}

function clearPromoted(
  store: ReturnType<typeof getPreferenceStore>,
  keys: string[],
  snapshotBeforeSave: JsonObject,
) {
  void store
    .patch((values) => ({
      unset: keys.filter(
        (key) =>
          Object.hasOwn(values, key) &&
          JSON.stringify(values[key]) === JSON.stringify(snapshotBeforeSave[key]),
      ),
    }))
    .catch(() => {});
}

/**
 * A generated view saved under a new id keeps every personal setting the file
 * did not take, and any promoted one changed while the save was in flight.
 * Settings the new instance already holds are its own and stay.
 */
function carryUnpromoted(
  from: ReturnType<typeof getPreferenceStore>,
  to: ReturnType<typeof getPreferenceStore>,
  promoted: string[],
  snapshotBeforeSave: JsonObject,
) {
  const carried = Object.entries(from.getSnapshot().values).filter(
    ([key, value]) =>
      !promoted.includes(key) || JSON.stringify(value) !== JSON.stringify(snapshotBeforeSave[key]),
  );

  if (carried.length === 0) return;

  void to
    .patch((current) => ({
      set: Object.fromEntries(carried.filter(([key]) => !Object.hasOwn(current, key))),
    }))
    .catch(() => {});
}

/** Whether the reader changed anything Save would write, relative to the loaded definition. */
function hasUnsavedChanges(input: SaveInput) {
  const { view, state } = input;
  const defaults = view.defaults;

  // Each pair is a setting the reader may have changed and its saved value.
  const pairs = [
    [state.filters, defaults.filters ?? []],
    [savedSort(input), defaults.sort ?? []],
  ] as const;

  return (
    // An active preset adds filters the file does not hold.
    Boolean(state.filterPreset) ||
    // Picking a layout in the switcher is navigation, so the variant alone is not a change.
    Object.keys(layoutChanges(input)).length > 0 ||
    pairs.some(
      ([current, saved]) =>
        current !== undefined && JSON.stringify(current) !== JSON.stringify(saved),
    )
  );
}

type Status =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "saved"; path: string }
  | { kind: "error"; message: string; conflict: boolean };

/**
 * Save writes the view's YAML directly (SPEC-0112): view files are trusted
 * repository configuration reviewed through version control. A short review
 * names the shared settings first, since everything else the reader changes
 * stays personal (SPEC-0114).
 */
export function SaveViewAction({
  viewID,
  dirty,
  changes,
  search,
  prepare,
}: {
  viewID: string;
  dirty: boolean;
  /** The shared settings this save changes. */
  changes: string[];
  /** A temporary search the reader may choose to include. */
  search: string | null;
  /** Builds the request; its `onSaved` runs only after the server writes the file. */
  prepare: (includeSearch: boolean) => PreparedSave;
}) {
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [reviewing, setReviewing] = useState(false);
  const [includeSearch, setIncludeSearch] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  usePopoverDismiss(reviewing, rootRef, () => setReviewing(false));

  useEffect(() => {
    if (status.kind !== "saved") return;
    const timer = window.setTimeout(() => setStatus({ kind: "idle" }), SAVED_NOTICE_MS);

    return () => window.clearTimeout(timer);
  }, [status]);

  async function save(overwrite = false) {
    setReviewing(false);
    setStatus({ kind: "saving" });
    const { body, onSaved } = prepare(includeSearch && search !== null);

    try {
      const response = await savePublicView(
        viewID,
        // Saving anyway after a conflict replaces what changed on disk.
        overwrite ? { ...body, definitionFingerprint: undefined } : body,
      );

      setStatus({ kind: "saved", path: response.path });
      setIncludeSearch(false);
      await onSaved(response);
    } catch (error) {
      const code = error instanceof ApiError ? error.status : undefined;

      setStatus({
        kind: "error",
        conflict: code === 409,
        message:
          code === 409
            ? "The view file changed since this view loaded."
            : `Could not save view${error instanceof Error && error.message ? `: ${error.message}` : "."}`,
      });
    }
  }

  return (
    <div className="configured-view__save-menu" ref={rootRef}>
      <button
        type="button"
        className="configured-view__tool-button configured-view__save"
        aria-label={dirty ? "Save view, unsaved changes" : "Save view"}
        aria-expanded={reviewing}
        title="Review and save shared settings to this view's YAML"
        disabled={status.kind === "saving"}
        onClick={() => setReviewing((open) => !open)}
      >
        {status.kind === "saving" ? "Saving…" : "Save view"}
        {dirty && <span className="configured-view__unsaved-dot" aria-hidden />}
      </button>
      {reviewing && (
        <div
          className="configured-view__save-review"
          role="dialog"
          aria-label="Save shared view configuration"
        >
          <p className="configured-view__save-review-title">
            Write shared settings to <code>.rhizome/views</code>
          </p>
          {changes.length > 0 ? (
            <ul>
              {changes.map((change) => (
                <li key={change}>{change}</li>
              ))}
            </ul>
          ) : (
            <p>Nothing differs from the view file; saving rewrites its current settings.</p>
          )}
          {search !== null && (
            <label>
              <input
                type="checkbox"
                checked={includeSearch}
                onChange={(event) => setIncludeSearch(event.target.checked)}
              />
              Include search “{search}”
            </label>
          )}
          <p className="configured-view__save-review-note">
            Column widths and expanded groups stay personal.
          </p>
          <div className="configured-view__save-review-actions">
            <button type="button" onClick={() => setReviewing(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="configured-view__save-confirm"
              onClick={() => void save()}
            >
              Write view YAML
            </button>
          </div>
        </div>
      )}
      {status.kind === "saved" && (
        <span className="configured-view__save-status" role="status">
          Saved <code>{status.path}</code>
        </span>
      )}
      {status.kind === "error" && (
        <span className="configured-view__save-error" role="alert">
          {status.message}
          {status.conflict && (
            <button type="button" onClick={() => void save(true)}>
              Save anyway
            </button>
          )}
        </span>
      )}
    </div>
  );
}
