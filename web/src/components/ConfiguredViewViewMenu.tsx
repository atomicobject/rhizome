import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";

import type {
  TypeProfile,
  ViewExecuteRequest,
  ViewFieldCapability,
  ViewTableColumn,
} from "../api/types";
import { NO_LANES } from "./boardLayout";
import { BoardFieldControls } from "./ConfiguredViewBoardParts";
import {
  capabilityFor,
  capabilityLabel,
  columnLabel,
  type ConfiguredViewVariant,
} from "./ConfiguredViewModel";
import type { SortSpec } from "./ConfiguredViewTableModel";
import { usePopoverDismiss } from "./focusManagement";
import { viewFieldControl } from "./viewFieldControls";

type Density = "two-line" | "one-line";

type Props = {
  state: ViewExecuteRequest;
  capabilities: ViewFieldCapability[];
  /** Capabilities without duplicate controls for one field. */
  displayCapabilities: ViewFieldCapability[];
  columns: ViewTableColumn[];
  visibleFields: string[];
  activeVariant: ConfiguredViewVariant;
  boardColumnField?: string;
  boardLaneField?: string;
  profile?: TypeProfile;
  executedGroupField?: string;
  /** The sort the view runs with: the reader's, else its configured one. */
  sort: SortSpec[];
  onToggleColumn: (field: string) => void;
  onResetPage: (patch: ViewExecuteRequest) => void;
  emptyFields: string[];
  density?: Density;
  onDensityChange?: (density: Density) => void;
  /** Save view and refresh. */
  viewActions?: ReactNode;
  /** Forgets the reader's personal settings for this view. */
  reset?: ReactNode;
  /** The shared view has unsaved changes; the button marks it while closed. */
  dirty: boolean;
  /** A shared save is running: the popover stays open, and reopens for its result. */
  pinned: boolean;
};

/**
 * The View popover (SPEC-0126 US3): grouping, sort, row density, columns,
 * reset, and Save view, one click behind the header's View button.
 */
export function ConfiguredViewViewMenu({
  state,
  capabilities,
  displayCapabilities,
  columns,
  visibleFields,
  activeVariant,
  boardColumnField,
  boardLaneField,
  profile,
  executedGroupField,
  sort,
  onToggleColumn,
  onResetPage,
  emptyFields,
  density,
  onDensityChange,
  viewActions,
  reset,
  dirty,
  pinned,
}: Props) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  usePopoverDismiss(open && !pinned, rootRef, () => setOpen(false));

  // A save's progress and result live in the panel: keep it open while the
  // request runs and show it again when the result arrives. Afterwards the
  // panel dismisses normally, so a failed save never traps it open.
  const wasPinned = useRef(pinned);

  useEffect(() => {
    if (pinned || wasPinned.current) setOpen(true);
    wasPinned.current = pinned;
  }, [pinned]);

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

  const sortable = displayCapabilities.filter((capability) => capability.sortable !== false);
  const sortKey = sort[0];

  const sortOption =
    sortable.find(
      (capability) =>
        capability.key === sortKey?.field ||
        capability.canonicalField === sortKey?.field ||
        (sortKey !== undefined && capability.sourceKeys?.includes(sortKey.field)),
    )?.key ??
    sortKey?.field ??
    "";

  const hiddenEmpty = emptyFields.filter((field) => !visibleFields.includes(field));

  return (
    <div className="configured-view__options" ref={rootRef}>
      <button
        type="button"
        className="configured-view__tool-button configured-view__options-button"
        aria-label={dirty ? "View options, unsaved changes" : "View options"}
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        <SlidersIcon />
        View
        {dirty && <span className="configured-view__unsaved-dot" aria-hidden />}
      </button>
      {/* Stays mounted while closed so a save in progress keeps its status. */}
      <div
        className="configured-view__options-panel"
        role="group"
        aria-label="View options"
        hidden={!open}
      >
        <div className="configured-view__options-section">
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
              <span>Group by</span>
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
          {sortable.length > 0 && (
            <div className="configured-view__select">
              <span>Sort</span>
              <span className="configured-view__sort-control">
                <select
                  aria-label="Sort by"
                  value={sortOption}
                  onChange={(event) =>
                    onResetPage({
                      sort: [{ field: event.target.value, direction: sortKey?.direction ?? "asc" }],
                    })
                  }
                >
                  {!sortOption && <option value="">Default</option>}
                  {sortable.map((capability) => (
                    <option key={capability.key} value={capability.key}>
                      {capabilityLabel(capability)}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="configured-view__tool-button"
                  disabled={!sortKey}
                  aria-label={sortKey?.direction === "desc" ? "Sort descending" : "Sort ascending"}
                  title="Reverse the sort"
                  onClick={() =>
                    sortKey &&
                    onResetPage({
                      sort: sort.map((item, index) =>
                        index === 0
                          ? { ...item, direction: item.direction === "desc" ? "asc" : "desc" }
                          : item,
                      ),
                    })
                  }
                >
                  {sortKey?.direction === "desc" ? "↓" : "↑"}
                </button>
              </span>
            </div>
          )}
          {activeVariant === "table" && density && (
            <div className="configured-view__select">
              <span>Rows</span>
              <span className="configured-view__segmented" role="group" aria-label="Rows">
                {(["one-line", "two-line"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={density === value}
                    onClick={() => onDensityChange?.(value)}
                  >
                    {value === "one-line" ? "1 line" : "2 lines"}
                  </button>
                ))}
              </span>
            </div>
          )}
        </div>
        {activeVariant === "table" && (
          <fieldset className="configured-view__columns-list" aria-label="Visible columns">
            <legend>
              Columns{" "}
              <small>
                {visibleFields.length}/{columns.length}
                {hiddenEmpty.length > 0 && ` · ${hiddenEmpty.length} hidden empty`}
              </small>
            </legend>
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
        {(viewActions || reset) && (
          <div className="configured-view__options-actions">
            {reset}
            {viewActions}
          </div>
        )}
      </div>
    </div>
  );
}

function SlidersIcon() {
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
      <path d="M2.5 4.5h7M12.5 4.5h1M2.5 11.5h1M6.5 11.5h7" />
      <circle cx="11" cy="4.5" r="1.5" />
      <circle cx="5" cy="11.5" r="1.5" />
    </svg>
  );
}

/** Dates group by calendar month; other fields by value. */
function groupSpec(field: string, capabilities: ViewFieldCapability[]) {
  const kind = viewFieldControl(capabilityFor(capabilities, field)).kind;

  return field && (kind === "date" || kind === "datetime")
    ? { field, bucket: "month" as const }
    : { field };
}

function columnImportanceRank(importance: ViewFieldCapability["importance"] | undefined) {
  if (importance === "KEY") return 0;

  if (importance === "DETAIL") return 2;

  return 1;
}
