// The sidebar's row parts and its type tree, shared by the Notes and Ontology
// sidebars (SPEC-0128): display groups holding their types and interfaces,
// with the same rows, carets, keyboard model, and remembered expansion.

import {
  type CSSProperties,
  type FocusEvent,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { decodeJson, isBoolean, isJsonObject } from "../api/parse";
import { flattenRailNodes, label, type RailGroup, type RailNode } from "./noteRailGroups";
import { useValidationQuery } from "./useNotesQueries";
import { useValidationScopeSummaries, validationScopeKey } from "./useValidationScopeSummaries";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { validationHealth } from "./validation/validationPresentation";

type RailDepthStyle = CSSProperties & { "--ontology-type-depth": number };

function isExpandedMap(value: unknown): value is Record<string, boolean> {
  return isJsonObject(value) && Object.values(value).every(isBoolean);
}

// Sections and type groups stay the way the user left them across reloads.
function readExpanded(key: string): Record<string, boolean> {
  try {
    return decodeJson(window.localStorage.getItem(key), isExpandedMap) ?? {};
  } catch {
    return {};
  }
}

function activeRailAncestors(groups: RailGroup[], selectedType: string | null): Set<string> {
  const active = new Set<string>();

  const visit = (nodes: RailNode[], ancestors: string[]) => {
    for (const node of nodes) {
      const name = node.kind === "type" ? node.type.name : node.interface.name;

      if (name === selectedType) ancestors.forEach((ancestor) => active.add(ancestor));
      visit(node.children, [...ancestors, `node:${name}`]);
    }
  };

  for (const group of groups) visit(group.children, group.name ? [`group:${group.name}`] : []);

  return active;
}

// Stroke icons drawn on one 16px grid, in the row's text color.
const ICONS = {
  home: "M2.5 7 8 2.5 13.5 7v6a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5zM6.5 13.5V9.5h3v4",
  clock: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12zM8 4.5V8l2.5 1.5",
  alert: "M8 2.5 14 13H2zM8 6.5v3M8 11.25v.01",
  edit: "M10.5 2.5 13.5 5.5 6 13H3v-3zM9 4l3 3",
  view: "M2.5 3.5h11v9h-11zM2.5 6.5h11M6.5 6.5v6",
  atlas:
    "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12zM2 8h12M8 2c1.7 1.6 2.5 3.6 2.5 6S9.7 12.4 8 14M8 2C6.3 3.6 5.5 5.6 5.5 8s.8 4.4 2.5 6",
  caret: "M6 4l4 4-4 4",
} as const;

export type RailIconName = keyof typeof ICONS;

export function RailIcon({ name }: { name: RailIconName }) {
  return (
    <svg
      className={`notes-left-rail__icon notes-left-rail__icon--${name}`}
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={ICONS[name]} />
    </svg>
  );
}

const ROW = "[data-rail-row]";

/**
 * The sidebar is one Tab stop: arrow keys move between rows, Home and End
 * jump to the ends, and Right and Left open and close a row's section.
 */
function moveRailFocus(event: KeyboardEvent<HTMLElement>) {
  if (event.altKey || event.metaKey || event.ctrlKey) return;
  const target = event.target;

  if (!(target instanceof HTMLElement) || !target.matches(ROW)) return;
  const rows = [...event.currentTarget.querySelectorAll<HTMLElement>(`${ROW}:not(:disabled)`)];
  const index = rows.indexOf(target);
  const caret = target.parentElement?.querySelector<HTMLButtonElement>(":scope > [aria-expanded]");
  const expanded = (caret ?? target).getAttribute("aria-expanded");

  const next =
    event.key === "ArrowDown"
      ? rows[index + 1]
      : event.key === "ArrowUp"
        ? rows[index - 1]
        : event.key === "Home"
          ? rows[0]
          : event.key === "End"
            ? rows.at(-1)
            : null;

  if (next) {
    event.preventDefault();
    next.focus();
  } else if (
    (event.key === "ArrowRight" && expanded === "false") ||
    (event.key === "ArrowLeft" && expanded === "true")
  ) {
    event.preventDefault();
    (caret ?? target).click();
  }
}

export type RailExpansion = {
  isExpanded: (key: string, openByDefault?: boolean) => boolean;
  toggle: (key: string, open?: boolean) => void;
};

/**
 * A sidebar's remembered expansion (under `storageKey`, opening the selected
 * type's ancestors) and its roving focus. Spread `railProps` on the rail.
 */
export function useRail(storageKey: string, groups: RailGroup[], selectedType: string | null) {
  const [expanded, setExpanded] = useState(() => readExpanded(storageKey));
  const railRef = useRef<HTMLElement>(null);
  const lastFocused = useRef<HTMLElement | null>(null);

  const activeAncestorKey = useMemo(
    () => [...activeRailAncestors(groups, selectedType)].sort().join("\n"),
    [groups, selectedType],
  );

  useEffect(() => {
    setExpanded((current) => {
      const next = { ...current };
      let changed = false;

      for (const key of activeAncestorKey.split("\n")) {
        if (!key || next[key]) continue;
        next[key] = true;
        changed = true;
      }

      return changed ? next : current;
    });
  }, [activeAncestorKey, selectedType]);
  useEffect(() => {
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(expanded));
    } catch {
      // Browser storage is optional; the rail keeps its state until reload.
    }
  }, [expanded, storageKey]);

  // Roving focus: one row is tabbable, the last one focused while it is still
  // shown, else the current row, else the first.
  useLayoutEffect(() => {
    const rail = railRef.current;

    if (!rail) return;
    const rows = [...rail.querySelectorAll<HTMLElement>(`${ROW}:not(:disabled)`)];

    const tabbable =
      (lastFocused.current && rows.includes(lastFocused.current) ? lastFocused.current : null) ??
      rows.find((row) => row.getAttribute("aria-current") === "page") ??
      rows[0];

    for (const row of rows) row.tabIndex = row === tabbable ? 0 : -1;
  });

  const isExpanded = (key: string, openByDefault = false) => expanded[key] ?? openByDefault;

  const toggle = (key: string, open = isExpanded(key)) =>
    setExpanded((current) => ({ ...current, [key]: !open }));

  const railProps = {
    ref: railRef,
    onKeyDown: moveRailFocus,
    onFocus: (event: FocusEvent<HTMLElement>) => {
      if (!(event.target instanceof HTMLElement) || !event.target.matches(ROW)) return;
      lastFocused.current = event.target;

      for (const row of event.currentTarget.querySelectorAll<HTMLElement>(ROW))
        row.tabIndex = row === event.target ? 0 : -1;
    },
  };

  return { isExpanded, toggle, railProps };
}

// `asRow` makes the caret the keyboard row when its row is disabled (an
// embedded type with children), so arrow keys still reach and open it.
function RailCaret({
  open,
  name,
  onToggle,
  asRow = false,
}: {
  open: boolean;
  name: string;
  onToggle: () => void;
  asRow?: boolean;
}) {
  return (
    <button
      type="button"
      className="ontology-type-row__collapse"
      aria-label={`${open ? "Collapse" : "Expand"} ${name}`}
      aria-expanded={open}
      data-rail-row={asRow ? "" : undefined}
      // Right and Left on the row open and close it, so the caret is not a Tab stop.
      tabIndex={-1}
      onClick={(event) => {
        onToggle();

        // A clicked caret hands focus to its row so the arrow keys keep working.
        if (!asRow) event.currentTarget.parentElement?.querySelector<HTMLElement>(ROW)?.focus();
      }}
    >
      <RailIcon name="caret" />
    </button>
  );
}

/** A collapsible section header with its caret inside, such as Views. */
export function RailSectionHeader({
  open,
  onToggle,
  children,
}: {
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) {
  return (
    <div className="ontology-type-row-wrap notes-left-rail__section">
      <button
        type="button"
        data-rail-row
        className="ontology-type-row ontology-type-row--group"
        aria-expanded={open}
        onClick={onToggle}
      >
        <span className="ontology-type-row__collapse" aria-hidden="true">
          <RailIcon name="caret" />
        </span>
        {children}
      </button>
    </div>
  );
}

export type TypeTreeProps = {
  groups: RailGroup[];
  expansion: RailExpansion;
  /** Fetches validation summaries for the badges while true. */
  active: boolean;
  /** The current type or interface. */
  selected: string | null;
  showCounts: boolean;
  onSelect: (name: string) => void;
  /** Notes keeps embedded types unselectable; Ontology shows their detail. */
  selectEmbedded?: boolean;
  /** With this, a group header selects the group and its caret toggles it. */
  onSelectGroup?: (group: string) => void;
  activeGroup?: string | null;
  /** Extra rows above a group's types; a group with them starts open. */
  groupContent?: (group: string) => ReactNode;
};

/** The display groups with their types and interfaces. */
export function TypeTree({
  groups,
  expansion: { isExpanded, toggle },
  active,
  selected,
  showCounts,
  onSelect,
  selectEmbedded = false,
  onSelectGroup,
  activeGroup,
  groupContent,
}: TypeTreeProps) {
  const validationQuery = useValidationQuery(active);

  const validationScopes = useMemo(
    () =>
      flattenRailNodes(groups.flatMap((group) => group.children)).map((node) =>
        node.kind === "type"
          ? { kind: "type" as const, key: node.type.name }
          : { kind: "interface" as const, key: node.interface.name },
      ),
    [groups],
  );

  const validationSummaries = useValidationScopeSummaries(
    validationQuery.data?.snapshot?.generation ?? null,
    validationScopes,
  );

  const health = validationHealth(validationQuery.data ?? null);

  const renderNode = (node: RailNode, depth = 0) => {
    const item = node.kind === "type" ? node.type : node.interface;
    const embedded = node.kind === "type" && node.type.role === "embedded";
    const disabled = embedded && !selectEmbedded;

    const typeIssues = validationSummaries.summaries.get(
      validationScopeKey({ kind: node.kind, key: item.name }),
    )?.issueCount;

    const key = `node:${item.name}`;
    const hasChildren = node.children.length > 0;
    const expandedNode = isExpanded(key);
    const depthStyle: RailDepthStyle = { "--ontology-type-depth": depth };

    return (
      <div
        className={`ontology-type-group${hasChildren ? "" : " ontology-type-group--leaf"}`}
        key={item.name}
      >
        <div className="ontology-type-row-wrap" style={depthStyle}>
          {hasChildren ? (
            <RailCaret
              open={expandedNode}
              name={label(item)}
              onToggle={() => toggle(key, expandedNode)}
              asRow={disabled}
            />
          ) : (
            <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
          )}
          <button
            type="button"
            data-rail-row
            disabled={disabled}
            aria-label={[
              embedded || !showCounts ? label(item) : `${label(item)} (${item.count})`,
              typeIssues ? `${typeIssues} validation ${typeIssues === 1 ? "issue" : "issues"}` : "",
            ]
              .filter(Boolean)
              .join(", ")}
            aria-current={selected === item.name ? "page" : undefined}
            className={`ontology-type-row${depth > 1 ? " ontology-type-row--child" : ""}${node.kind === "interface" ? " ontology-type-row--interface" : ""}${disabled ? " ontology-type-row--embedded" : ""}${selected === item.name ? " is-selected" : ""}`}
            onClick={() => onSelect(item.name)}
          >
            <span className="ontology-type-row__label">{label(item)}</span>
            <ValidationIssueBadge
              count={typeIssues}
              health={health}
              presenceOnly
              label={`${typeIssues ?? "Unknown"} validation issues in ${label(item)}`}
            />
            {!embedded && showCounts && (
              <span className="ontology-type-row__count">{item.count}</span>
            )}
          </button>
        </div>
        {hasChildren && expandedNode && node.children.map((child) => renderNode(child, depth + 1))}
      </div>
    );
  };

  return groups.map((group) => {
    const key = `group:${group.name}`;
    const content = groupContent?.(group.name);
    const expandedGroup = isExpanded(key, Boolean(content));

    return (
      <div
        className={`ontology-type-group ontology-type-group--presentation${expandedGroup ? " is-expanded" : " is-collapsed"}`}
        key={group.name}
      >
        {onSelectGroup ? (
          <div className="ontology-type-row-wrap notes-left-rail__section">
            <RailCaret
              open={expandedGroup}
              name={group.name}
              onToggle={() => toggle(key, expandedGroup)}
            />
            <button
              type="button"
              data-rail-row
              className={`ontology-type-row ontology-type-row--group${activeGroup === group.name ? " is-selected" : ""}`}
              aria-current={activeGroup === group.name ? "page" : undefined}
              onClick={() => onSelectGroup(group.name)}
            >
              {group.name}
            </button>
          </div>
        ) : (
          <RailSectionHeader open={expandedGroup} onToggle={() => toggle(key, expandedGroup)}>
            {group.name}
          </RailSectionHeader>
        )}
        {expandedGroup && content}
        {expandedGroup && group.children.map((node) => renderNode(node, 1))}
      </div>
    );
  });
}
