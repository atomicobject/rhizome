import {
  initialDraftFilter,
  draftsFromState,
  filterSummary,
  filterCapabilitiesForDraft,
} from "./ConfiguredViewFilterDrafts";
import { Filter } from "lucide-react";
import { ConfiguredViewFind } from "./ConfiguredViewFind";
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
} from "./ConfiguredViewFilterRow";
import { usePopoverDismiss } from "./focusManagement";
import { viewFieldControl } from "./viewFieldControls";
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
  /** Columns hidden because one value fills nearly every row. */
  repeatedFields?: string[];
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

const FILTER_DEBOUNCE_MS = 300;

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
  repeatedFields = [],
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
  const lastEmittedFiltersKeyRef = useRef<string | null>(null);

  usePopoverDismiss(filterOpen, filterRef, () => setFilterOpen(false));

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
    updateFilter(index, {
      field,
      op: viewFieldControl(capability).defaultOp,
      value: "",
      values: [],
    });
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
      <ConfiguredViewFind
        active={active}
        value={state.search ?? ""}
        onChange={(search) => onResetPage({ search })}
      />
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
          <Filter size={16} strokeWidth={1.5} absoluteStrokeWidth aria-hidden />
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
        repeatedFields={repeatedFields}
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

  // The editor shows only while it has rows; removing the last one returns
  // the chips, so an active preset never hides with nothing to edit.
  const editingFilters = filtersExpanded && draftFilters.length > 0;
  const showChips = !editingFilters && (chips.length > 0 || activePreset);

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
