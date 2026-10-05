import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";

import type {
  TypeProfile,
  ViewExecuteRequest,
  ViewFieldCapability,
  ViewTableColumn,
} from "../api/types";
import { NO_LANES } from "./boardLayout";
import { BoardFieldControls } from "./ConfiguredViewBoardParts";
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
  capabilityLabel,
  columnLabel,
  dedupeCapabilitiesForControls,
  type ConfiguredViewVariant,
} from "./ConfiguredViewModel";

type Props = {
  state: ViewExecuteRequest;
  capabilities: ViewFieldCapability[];
  columns: ViewTableColumn[];
  visibleFields: string[];
  issueCounts?: ReadonlyMap<string, number>;
  activeVariant: ConfiguredViewVariant;
  boardColumnField?: string;
  boardLaneField?: string;
  profile?: TypeProfile;
  executedGroupField?: string;
  onToggleColumn: (field: string) => void;
  onResetPage: (patch: ViewExecuteRequest) => void;
  onIssuesOnlyChange: (enabled: boolean) => void;
  /** Generated columns no matching row fills; the menu lists them as hidden empty. */
  emptyFields?: string[];
  /** Table row density; the Rows control shows only when it is set. */
  density?: "two-line" | "one-line";
  onDensityChange?: (density: "two-line" | "one-line") => void;
  /** Controls placed after the tools, such as an embedded view's layout switcher. */
  trailing?: ReactNode;
};

type FilterSpec = NonNullable<ViewExecuteRequest["filters"]>[number];

const FILTER_DEBOUNCE_MS = 300;

let nextFilterID = 0;

export function ConfiguredViewToolbar({
  state,
  capabilities,
  columns,
  visibleFields,
  issueCounts,
  activeVariant,
  boardColumnField,
  boardLaneField,
  profile,
  executedGroupField,
  onToggleColumn,
  onResetPage,
  onIssuesOnlyChange,
  emptyFields = [],
  density,
  onDensityChange,
  trailing,
}: Props) {
  const displayCapabilities = useMemo(
    () => dedupeCapabilitiesForControls(capabilities),
    [capabilities],
  );

  const columnOptions = useMemo(
    () =>
      [...columns].sort(
        (left, right) =>
          columnImportanceRank(capabilityFor(capabilities, left.field)?.importance) -
          columnImportanceRank(capabilityFor(capabilities, right.field)?.importance),
      ),
    [capabilities, columns],
  );

  const groupField = state.group?.field ?? executedGroupField ?? "";

  // A view may group by a selector, such as frontmatter.status, whose control shows under its canonical key.
  const groupOption =
    displayCapabilities.find(
      (capability) =>
        capability.key === groupField ||
        capability.canonicalField === groupField ||
        capability.sourceKeys?.includes(groupField),
    )?.key ?? groupField;

  const hiddenEmpty = emptyFields.filter((field) => !visibleFields.includes(field));
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [filtersExpanded, setFiltersExpanded] = useState(false);

  const [draftFilters, setDraftFilters] = useState<DraftFilter[]>(() =>
    draftsFromState(state.filters ?? [], capabilities),
  );

  const columnPopoverRef = useRef<HTMLDivElement | null>(null);
  const lastEmittedFiltersKeyRef = useRef<string | null>(null);

  useEffect(() => {
    const stateFiltersKey = JSON.stringify(state.filters ?? []);

    if (lastEmittedFiltersKeyRef.current === stateFiltersKey) return;
    setDraftFilters(draftsFromState(state.filters ?? [], displayCapabilities));
  }, [displayCapabilities, state.filters]);

  usePopoverDismiss(columnsOpen, columnPopoverRef, () => setColumnsOpen(false));

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

  const addFilterRef = useRef<HTMLButtonElement | null>(null);
  const filtersRef = useRef<HTMLDivElement | null>(null);
  const [focusFilterID, setFocusFilterID] = useState<string | null>(null);

  // A new filter takes focus at its field; a removed one hands focus back to
  // "+ Filter" instead of dropping it on the page.
  useEffect(() => {
    if (!focusFilterID) return;
    filtersRef.current
      ?.querySelector<HTMLElement>(`[data-filter-id="${CSS.escape(focusFilterID)}"] select`)
      ?.focus();
    setFocusFilterID(null);
  }, [focusFilterID]);

  function addFilter() {
    const draft = initialDraftFilter(displayCapabilities);
    setFiltersExpanded(true);
    setDraftFilters((current) => [...current, draft]);
    setFocusFilterID(draft.id);
  }

  function removeFilter(index: number) {
    setDraftFilters((current) => current.filter((_, filterIndex) => filterIndex !== index));
    addFilterRef.current?.focus();
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

  return (
    <>
      <div className="configured-view__toolbar">
        <input
          className="configured-view__search"
          type="search"
          aria-label="Search view"
          placeholder="Search view…"
          value={state.search ?? ""}
          onChange={(event) => onResetPage({ search: event.target.value })}
        />
        {activeVariant === "kanban" ? (
          <BoardFieldControls
            capabilities={capabilities}
            profile={profile}
            columnField={state.columnField ?? boardColumnField}
            laneField={state.laneField ?? (boardLaneField || NO_LANES)}
            onChange={onResetPage}
          />
        ) : (
          <label className="configured-view__select">
            <span>Group</span>
            <select
              aria-label="Group by"
              value={groupField ? groupOption : ""}
              onChange={(event) =>
                onResetPage({ group: groupSpec(event.target.value, capabilities) })
              }
            >
              <option value="">None</option>
              {displayCapabilities
                .filter((capability) => capability.groupable)
                .map((capability) => (
                  <option key={capability.key} value={capability.key}>
                    {capabilityLabel(capability)}
                  </option>
                ))}
            </select>
          </label>
        )}
        {activeVariant === "table" && density && (
          <label className="configured-view__select">
            <span>Rows</span>
            <select
              aria-label="Rows"
              value={density}
              onChange={(event) =>
                onDensityChange?.(event.target.value === "one-line" ? "one-line" : "two-line")
              }
            >
              <option value="two-line">Two-line</option>
              <option value="one-line">One-line</option>
            </select>
          </label>
        )}
        <div className="configured-view__tools">
          {activeVariant === "table" && (
            <div className="configured-view__columns-menu" ref={columnPopoverRef}>
              <button
                type="button"
                className="configured-view__tool-button"
                aria-label="Columns"
                aria-expanded={columnsOpen}
                onClick={() => setColumnsOpen((open) => !open)}
              >
                Columns {visibleFields.length}/{columns.length}
                {hiddenEmpty.length > 0 && ` · ${hiddenEmpty.length} hidden empty`}
              </button>
              {columnsOpen && (
                <fieldset className="configured-view__columns-popover">
                  <legend>Visible columns</legend>
                  {columnOptions.map((column) => (
                    <label key={column.field}>
                      <input
                        type="checkbox"
                        checked={visibleFields.includes(column.field)}
                        onChange={() => onToggleColumn(column.field)}
                      />
                      <span>{columnLabel(column, capabilities)}</span>
                      {hiddenEmpty.includes(column.field) && (
                        <small className="configured-view__column-note">hidden empty</small>
                      )}
                    </label>
                  ))}
                </fieldset>
              )}
            </div>
          )}
          <button
            ref={addFilterRef}
            type="button"
            className="configured-view__tool-button"
            aria-label="Add filter"
            onClick={addFilter}
            disabled={capabilities.length === 0}
          >
            + Filter
          </button>
          {issueCounts && (
            <label className="configured-view__issues-filter">
              <input
                type="checkbox"
                onChange={(event) => onIssuesOnlyChange(event.target.checked)}
              />
              Problems on this page
            </label>
          )}
          {draftFilters.length > 0 && (
            <button
              type="button"
              className="configured-view__tool-button"
              aria-expanded={filtersExpanded}
              onClick={() => setFiltersExpanded((expanded) => !expanded)}
            >
              Filters ({executableFilters.length})
            </button>
          )}
          {trailing}
        </div>
      </div>
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
        </div>
      )}
      {executableFilters.length > 0 && !filtersExpanded && (
        <div className="configured-view__active-filters">
          {draftFilters.flatMap((draft, index) => {
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
          })}
        </div>
      )}
    </>
  );
}

/** Dates group by calendar month; other fields by value. */
function groupSpec(field: string, capabilities: ViewFieldCapability[]) {
  const kind = viewFieldControl(capabilityFor(capabilities, field)).kind;

  return field && (kind === "date" || kind === "datetime")
    ? { field, bucket: "month" as const }
    : { field };
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

function columnImportanceRank(importance: ViewFieldCapability["importance"] | undefined) {
  if (importance === "KEY") return 0;

  if (importance === "DETAIL") return 2;

  return 1;
}
