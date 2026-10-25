import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";

import type {
  TypeProfile,
  ViewExecuteRequest,
  ViewFieldCapability,
  ViewTableColumn,
} from "../api/types";
import { humanizeLabel } from "./ConfiguredTableCellHelpers";
import {
  type DraftFilter,
  filterIsRunnable,
  FilterRow,
  filterSpecForDraft,
  opLabel,
  optionLabel,
} from "./ConfiguredViewFilterRow";
import { usePopoverDismiss } from "./focusManagement";
import { capabilityOptions, viewFieldControl } from "./viewFieldControls";
import {
  capabilityFor,
  dedupeCapabilitiesForControls,
  type ConfiguredViewVariant,
  type FilterPreset,
} from "./ConfiguredViewModel";
import type { SortSpec } from "./ConfiguredViewTableModel";
import { ConfiguredViewViewMenu } from "./ConfiguredViewViewMenu";

type Props = {
  state: ViewExecuteRequest;
  capabilities: ViewFieldCapability[];
  columns: ViewTableColumn[];
  visibleFields: string[];
  activeVariant: ConfiguredViewVariant;
  boardColumnField?: string;
  boardLaneField?: string;
  profile?: TypeProfile;
  executedGroupField?: string;
  /** The sort the view runs with: the reader's, else its configured one. */
  sort?: SortSpec[];
  onToggleColumn: (field: string) => void;
  onResetPage: (patch: ViewExecuteRequest) => void;
  /** Generated columns no matching row fills; the menu lists them as hidden empty. */
  emptyFields?: string[];
  /** Table row density; the Rows control shows only when it is set. */
  density?: "two-line" | "one-line";
  onDensityChange?: (density: "two-line" | "one-line") => void;
  /** Less frequent actions, such as Save view and refresh, shown under View. */
  viewActions?: ReactNode;
  /** Forgets the reader's personal settings, shown under View. */
  reset?: ReactNode;
  /** The shared view has unsaved changes; View marks it while closed. */
  optionsDirty?: boolean;
  /** A shared save is running: View stays open, and reopens for its result. */
  optionsPinned?: boolean;
  /** The view's named filter presets, offered at the top of Filter. */
  presets?: FilterPreset[];
  /** One-click filters with their counts, such as lifecycle stages, offered in Filter. */
  quickFilters?: ReactNode;
  /** Status shown beside the active filters, such as preference errors. */
  status?: ReactNode;
  /** Only the active view takes ⌘F. */
  active?: boolean;
  /** Places Find, Filter, and View: in the page header, or in a row of their own. */
  placeTools: (tools: ReactNode) => ReactNode;
};

type FilterSpec = NonNullable<ViewExecuteRequest["filters"]>[number];

const FILTER_DEBOUNCE_MS = 300;

let nextFilterID = 0;

export function ConfiguredViewToolbar({
  state,
  capabilities,
  columns,
  visibleFields,
  activeVariant,
  boardColumnField,
  boardLaneField,
  profile,
  executedGroupField,
  sort = [],
  onToggleColumn,
  onResetPage,
  emptyFields = [],
  density,
  onDensityChange,
  viewActions,
  reset,
  optionsDirty = false,
  optionsPinned = false,
  presets = [],
  quickFilters,
  status,
  active = true,
  placeTools,
}: Props) {
  const displayCapabilities = useMemo(
    () => dedupeCapabilitiesForControls(capabilities),
    [capabilities],
  );

  const [filterOpen, setFilterOpen] = useState(false);
  const [filtersExpanded, setFiltersExpanded] = useState(false);

  const [draftFilters, setDraftFilters] = useState<DraftFilter[]>(() =>
    draftsFromState(state.filters ?? [], capabilities),
  );

  const filterRef = useRef<HTMLDivElement | null>(null);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const lastEmittedFiltersKeyRef = useRef<string | null>(null);

  usePopoverDismiss(filterOpen, filterRef, () => setFilterOpen(false));

  // ⌘F (Ctrl+F elsewhere) finds in the view while it shows.
  useEffect(() => {
    if (!active) return;

    const onKeyDown = (event: KeyboardEvent) => {
      const search = searchRef.current;

      if (
        event.key.toLowerCase() !== "f" ||
        !(event.metaKey || event.ctrlKey) ||
        event.shiftKey ||
        event.altKey ||
        !search?.isConnected
      )
        return;
      event.preventDefault();
      search.focus();
      search.select();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active]);

  useEffect(() => {
    const stateFiltersKey = JSON.stringify(state.filters ?? []);

    if (lastEmittedFiltersKeyRef.current === stateFiltersKey) return;
    setDraftFilters(draftsFromState(state.filters ?? [], displayCapabilities));
  }, [displayCapabilities, state.filters]);

  const executableFilters = useMemo(
    () =>
      draftFilters.flatMap((draft) => {
        const filter = filterSpecForDraft(draft);

        return filterIsRunnable(filter) ? [filter] : [];
      }),
    [draftFilters],
  );

  const executableFiltersKey = JSON.stringify(executableFilters);

  useEffect(() => {
    if (JSON.stringify(state.filters ?? []) === executableFiltersKey) return;

    const timer = window.setTimeout(() => {
      lastEmittedFiltersKeyRef.current = executableFiltersKey;
      onResetPage({ filters: executableFilters });
    }, FILTER_DEBOUNCE_MS);

    return () => window.clearTimeout(timer);
  }, [executableFilters, executableFiltersKey, onResetPage, state.filters]);

  const filterButtonRef = useRef<HTMLButtonElement | null>(null);
  const filtersRef = useRef<HTMLDivElement | null>(null);
  const [focusFilterID, setFocusFilterID] = useState<string | null>(null);

  // A new filter takes focus at its field; a removed one hands focus back to
  // Filter instead of dropping it on the page.
  useEffect(() => {
    if (!focusFilterID) return;
    filtersRef.current
      ?.querySelector<HTMLElement>(`[data-filter-id="${CSS.escape(focusFilterID)}"] select`)
      ?.focus();
    setFocusFilterID(null);
  }, [focusFilterID]);

  function addFilter() {
    const draft = initialDraftFilter(displayCapabilities);
    setFilterOpen(false);
    setFiltersExpanded(true);
    setDraftFilters((current) => [...current, draft]);
    setFocusFilterID(draft.id);
  }

  function removeFilter(index: number) {
    setDraftFilters((current) => current.filter((_, filterIndex) => filterIndex !== index));
    filterButtonRef.current?.focus();
  }

  function updateFilter(index: number, patch: Partial<DraftFilter>) {
    setDraftFilters((current) =>
      current.map((filter, filterIndex) =>
        filterIndex === index ? { ...filter, ...patch } : filter,
      ),
    );
  }

  function updateFilterField(index: number, field: string) {
    const capability = capabilityFor(displayCapabilities, field);
    updateFilter(index, { field, op: defaultFilterOp(capability), value: "", values: [] });
  }

  function updateFilterOperator(index: number, op: string) {
    setDraftFilters((current) =>
      current.map((filter, filterIndex) => {
        if (filterIndex !== index || filter.op === op) return filter;

        const value =
          filter.op === "in" ? String(filterSpecForDraft(filter).values?.[0] ?? "") : filter.value;

        return { ...filter, op, value, values: op === "in" && value ? [value] : [] };
      }),
    );
  }

  function toggleFilterValue(index: number, value: string) {
    setDraftFilters((current) =>
      current.map((filter, filterIndex) => {
        if (filterIndex !== index) return filter;

        if (filter.op !== "in") return { ...filter, value, values: [] };

        const values = filter.values.includes(value)
          ? filter.values.filter((item) => item !== value)
          : [...filter.values, value];

        return { ...filter, value: values.join(", "), values };
      }),
    );
  }

  const activePreset = presets.find((preset) => preset.id === state.filterPreset);
  const activeCount = executableFilters.length + (activePreset ? 1 : 0);

  const tools = (
    <>
      <label className="configured-view__find">
        <SearchIcon />
        <input
          ref={searchRef}
          className="configured-view__search"
          type="search"
          aria-label="Search view"
          aria-keyshortcuts="Meta+F Control+F"
          placeholder="Find in view"
          value={state.search ?? ""}
          onChange={(event) => onResetPage({ search: event.target.value })}
        />
        {!state.search && <kbd aria-hidden="true">⌘F</kbd>}
      </label>
      <div className="configured-view__options" ref={filterRef}>
        <button
          ref={filterButtonRef}
          type="button"
          className="configured-view__tool-button configured-view__options-button"
          aria-label={activeCount ? `Filter, ${activeCount} active` : "Filter"}
          aria-expanded={filterOpen}
          onClick={() => setFilterOpen((open) => !open)}
          disabled={capabilities.length === 0 && presets.length === 0}
        >
          <FilterIcon />
          Filter
          {activeCount > 0 && <span className="configured-view__tool-count">{activeCount}</span>}
        </button>
        {/* Stays mounted while closed: quick filters remember the values the
            unfiltered collection offered, so the reader can add another. */}
        <div
          className="configured-view__options-panel configured-view__filter-menu"
          role="group"
          aria-label="Filter options"
          hidden={!filterOpen}
        >
          {presets.length > 0 && (
            <div className="configured-view__options-section" aria-label="Filter presets">
              <h4>Presets</h4>
              {presets.map((preset) => (
                <button
                  key={preset.id}
                  type="button"
                  className="configured-view__menu-toggle"
                  aria-pressed={state.filterPreset === preset.id}
                  onClick={() =>
                    onResetPage({
                      filterPreset: state.filterPreset === preset.id ? "" : preset.id,
                    })
                  }
                >
                  {preset.label}
                </button>
              ))}
            </div>
          )}
          {quickFilters}
          <div className="configured-view__options-actions">
            <button
              type="button"
              className="configured-view__tool-button"
              aria-label="Add filter"
              onClick={addFilter}
              disabled={capabilities.length === 0}
            >
              + Add filter
            </button>
            {draftFilters.length > 0 && (
              <button
                type="button"
                className="configured-view__tool-button"
                onClick={() => {
                  setFilterOpen(false);
                  setFiltersExpanded(true);
                }}
              >
                Edit filters ({draftFilters.length})
              </button>
            )}
          </div>
        </div>
      </div>
      <ConfiguredViewViewMenu
        state={state}
        capabilities={capabilities}
        displayCapabilities={displayCapabilities}
        columns={columns}
        visibleFields={visibleFields}
        activeVariant={activeVariant}
        boardColumnField={boardColumnField}
        boardLaneField={boardLaneField}
        profile={profile}
        executedGroupField={executedGroupField}
        sort={sort}
        onToggleColumn={onToggleColumn}
        onResetPage={onResetPage}
        emptyFields={emptyFields}
        density={density}
        onDensityChange={onDensityChange}
        viewActions={viewActions}
        reset={reset}
        dirty={optionsDirty}
        pinned={optionsPinned}
      />
    </>
  );

  const chips = draftFilters.flatMap((draft, index) => {
    const filter = filterSpecForDraft(draft);

    if (!filterIsRunnable(filter)) return [];

    const summary = [
      humanizeLabel(capabilityFor(capabilities, filter.field)?.label || filter.field),
      opLabel(filter.op),
      filterSummary(filter, capabilityFor(capabilities, filter.field)),
    ]
      .filter(Boolean)
      .join(" ");

    return [
      <span key={draft.id} className="configured-view__active-filter">
        {summary}
        <button
          type="button"
          aria-label={`Remove filter ${summary}`}
          title="Remove filter"
          onClick={() => removeFilter(index)}
        >
          ×
        </button>
      </span>,
    ];
  });

  const showChips = !filtersExpanded && (chips.length > 0 || activePreset);

  return (
    <>
      {placeTools(tools)}
      {(showChips || status) && (
        <div className="configured-view__active-filters">
          {showChips && activePreset && (
            <span className="configured-view__active-filter">
              {activePreset.label}
              <button
                type="button"
                aria-label={`Remove preset ${activePreset.label}`}
                title="Remove preset"
                onClick={() => onResetPage({ filterPreset: "" })}
              >
                ×
              </button>
            </span>
          )}
          {showChips && chips}
          {showChips && draftFilters.length > 0 && (
            <button
              type="button"
              className="configured-view__chip-action"
              aria-expanded={false}
              onClick={() => setFiltersExpanded(true)}
            >
              Edit
            </button>
          )}
          {status}
        </div>
      )}
      {draftFilters.length > 0 && filtersExpanded && (
        <div className="configured-view__filters" ref={filtersRef}>
          {draftFilters.map((filter, index) => (
            <FilterRow
              key={filter.id}
              filter={filter}
              capabilities={filterCapabilitiesForDraft(
                displayCapabilities,
                capabilities,
                filter.field,
              )}
              onFieldChange={(field) => updateFilterField(index, field)}
              onOpChange={(op) => updateFilterOperator(index, op)}
              onValueChange={(value) => updateFilter(index, { value })}
              onToggleValue={(value) => toggleFilterValue(index, value)}
              onRemove={() => removeFilter(index)}
            />
          ))}
          <span className="configured-view__filters-actions">
            <button type="button" className="configured-view__tool-button" onClick={addFilter}>
              + Add filter
            </button>
            <button
              type="button"
              className="configured-view__tool-button"
              aria-expanded
              onClick={() => setFiltersExpanded(false)}
            >
              Done
            </button>
          </span>
        </div>
      )}
    </>
  );
}

function SearchIcon() {
  return (
    <svg
      className="configured-view__tool-icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
    >
      <circle cx="7" cy="7" r="4.5" />
      <path d="m10.5 10.5 3 3" />
    </svg>
  );
}

function FilterIcon() {
  return (
    <svg
      className="configured-view__tool-icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinejoin="round"
    >
      <path d="M2.5 3.5h11l-4.25 5v4l-2.5 1v-5z" />
    </svg>
  );
}

function initialDraftFilter(capabilities: ViewFieldCapability[]): DraftFilter {
  const first = [...capabilities]
    .filter((capability) => viewFieldControl(capability).ops.length > 0)
    .sort((left, right) => draftFilterRank(right) - draftFilterRank(left))[0];

  return {
    id: newFilterID(),
    field: first?.key ?? capabilities[0]?.key ?? "",
    op: defaultFilterOp(first),
    value: "",
    values: [],
  };
}

function draftFilterRank(capability: ViewFieldCapability) {
  let rank = capabilityOptions(capability).length > 0 ? 100 : 0;

  const names = [
    capability.key,
    capability.label,
    capability.canonicalField,
    capability.edit?.field,
  ]
    .filter(Boolean)
    .join(" ");

  if (capability.semanticRole === "status" || /status/i.test(names)) rank += 80;

  if (viewFieldControl(capability).ops.includes("in")) rank += 10;

  if (capability.semanticRole === "identifier" || capability.key === "id") rank -= 60;

  return rank;
}

function draftsFromState(filters: FilterSpec[], capabilities: ViewFieldCapability[]) {
  return filters.map((filter) => {
    const values = (filter.values ?? []).map((value) => String(value));

    return {
      id: newFilterID(),
      field: filter.field,
      op: filter.op || defaultFilterOp(capabilityFor(capabilities, filter.field)),
      value: String(filter.value ?? values.join(", ")),
      values,
    };
  });
}

function newFilterID() {
  nextFilterID += 1;

  return `filter-${nextFilterID}`;
}

function defaultFilterOp(capability: ViewFieldCapability | undefined) {
  return viewFieldControl(capability).defaultOp;
}

function filterSummary(filter: FilterSpec, capability: ViewFieldCapability | undefined) {
  if (filter.op === "in") {
    return (filter.values ?? []).map((value) => optionLabel(capability, String(value))).join(", ");
  }

  if (filter.op === "exists" || filter.op === "missing") return "";

  if (viewFieldControl(capability).kind === "relation")
    return optionLabel(capability, String(filter.value ?? ""));

  return String(filter.value ?? "");
}

function filterCapabilitiesForDraft(
  display: ViewFieldCapability[],
  all: ViewFieldCapability[],
  field: string,
) {
  if (capabilityFor(display, field)) return display;
  const selected = capabilityFor(all, field);

  return selected ? [...display, selected] : display;
}
