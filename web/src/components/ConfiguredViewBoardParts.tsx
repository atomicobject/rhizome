import { useEffect, useRef } from "react";

import type { TypeProfile, ViewExecuteRequest, ViewFieldCapability } from "../api/types";
import {
  type BoardColumn,
  type BoardLane,
  boardColumnFieldOptions,
  boardLaneFieldOptions,
  NO_LANES,
} from "./boardLayout";
import { capabilityLabel, GroupValueMark } from "./ConfiguredViewModel";
import { moveMenuFocus } from "./focusManagement";
import { STALE_DAYS } from "./viewProfile";

/** A board card's Move button and its menu of other columns; `m` on a card opens it too. */
export function BoardMoveMenu({
  title,
  open,
  busy,
  targets,
  onOpenChange,
  onChoose,
}: {
  title: string;
  open: boolean;
  busy: boolean;
  targets: Array<{ key: string; label: string }>;
  onOpenChange: (open: boolean) => void;
  onChoose: (key: string) => Promise<void>;
}) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    window.requestAnimationFrame(() =>
      menuRef.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus(),
    );
  }, [open]);

  const restoreFocus = () => window.requestAnimationFrame(() => buttonRef.current?.focus());

  return (
    <div className="configured-card__move-wrap">
      <button
        ref={buttonRef}
        type="button"
        className="configured-card__move"
        aria-label={`Move ${title}`}
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={busy}
        onClick={() => onOpenChange(!open)}
      >
        Move
      </button>
      {open && (
        <div
          className="configured-card__move-menu"
          role="menu"
          aria-label="Move to column"
          ref={menuRef}
          onBlur={(event) => {
            const next = event.relatedTarget;

            if (!(next instanceof Node) || !event.currentTarget.contains(next)) onOpenChange(false);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              onOpenChange(false);
              restoreFocus();

              return;
            }

            if (moveMenuFocus(event.currentTarget, event.key)) event.preventDefault();
          }}
        >
          {targets.map((target) => (
            <button
              type="button"
              role="menuitem"
              key={target.key}
              onClick={() => {
                onOpenChange(false);
                void onChoose(target.key).finally(restoreFocus);
              }}
            >
              {target.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/** The board's Columns and Lanes pickers (SPEC-0112). */
export function BoardFieldControls({
  capabilities,
  profile,
  columnField,
  laneField,
  onChange,
}: {
  capabilities: ViewFieldCapability[];
  profile: TypeProfile | undefined;
  columnField: string | undefined;
  laneField: string;
  onChange: (patch: ViewExecuteRequest) => void;
}) {
  const columnOptions = boardColumnFieldOptions(profile, capabilities, columnField);
  const laneOptions = boardLaneFieldOptions(profile, capabilities, columnField, laneField);

  const column = columnOptions.find(
    (option) => option.key === columnField || option.canonicalField === columnField,
  );

  const lane = laneOptions.find(
    (option) => option.key === laneField || option.canonicalField === laneField,
  );

  return (
    <>
      <label className="configured-view__select">
        <span>Columns</span>
        <select
          aria-label="Board columns"
          value={column?.key ?? ""}
          disabled={columnOptions.length < 2}
          onChange={(event) => {
            const next = event.target.value;
            // The lane field cannot also be the column field.
            onChange(
              next === lane?.key
                ? { columnField: next, laneField: NO_LANES }
                : { columnField: next },
            );
          }}
        >
          {columnOptions.map((option) => (
            <option key={option.key} value={option.key}>
              {capabilityLabel(option)}
            </option>
          ))}
        </select>
      </label>
      <label className="configured-view__select">
        <span>Lanes</span>
        <select
          aria-label="Board lanes"
          value={lane?.key ?? NO_LANES}
          onChange={(event) => onChange({ laneField: event.target.value })}
        >
          <option value={NO_LANES}>None</option>
          {laneOptions.map((option) => (
            <option key={option.key} value={option.key}>
              {capabilityLabel(option)}
            </option>
          ))}
        </select>
      </label>
    </>
  );
}

type HeaderItem = {
  column: BoardColumn;
  label: string;
  count: number;
  collapsed: boolean;
  strip: boolean;
  stale: number;
};

/** The board's header row: a column header, an empty strip, or a collapsed strip per value. */
export function BoardColumnHeaders({
  items,
  capability,
  onToggle,
}: {
  items: HeaderItem[];
  capability: ViewFieldCapability | undefined;
  onToggle: (key: string) => void;
}) {
  return items.map((item) => {
    const { column, label, count } = item;
    const mark = <GroupValueMark capability={capability} tone={column.tone} label={label} />;

    if (item.collapsed) {
      return (
        <button
          type="button"
          key={column.key}
          className="configured-view__board-strip"
          aria-label={`Expand ${label} column, ${count} items`}
          title={`Expand ${label}`}
          onClick={() => onToggle(column.key)}
        >
          {mark}
          <span>{count}</span>
        </button>
      );
    }

    if (item.strip) {
      return (
        <div
          key={column.key}
          className="configured-view__board-strip is-empty"
          title={`${label}: no records`}
        >
          {mark}
          <span>0</span>
        </div>
      );
    }

    return (
      <header key={column.key} className="configured-view__board-column-header">
        {mark}
        {item.stale > 0 && (
          <span
            className="configured-view__board-stale"
            title={`Active and unchanged for ${STALE_DAYS} days`}
          >
            {item.stale} stale
          </span>
        )}
        <span className="configured-view__board-count">{count}</span>
        <button
          type="button"
          aria-label={`Collapse ${label} column`}
          title="Collapse column"
          onClick={() => onToggle(column.key)}
        >
          Collapse
        </button>
      </header>
    );
  });
}

/** A lane names an enum value or a link target; a toned target shows its status mark. */
export function LaneMark({ lane }: { lane: BoardLane }) {
  return lane.tone ? (
    <GroupValueMark capability={undefined} tone={lane.tone} label={lane.label} />
  ) : (
    <span className="configured-view__group-label">{lane.label}</span>
  );
}
