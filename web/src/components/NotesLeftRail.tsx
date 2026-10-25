import {
  buildRailGroups,
  flattenRailNodes,
  label,
  type RailGroup,
  type RailNode,
} from "./noteRailGroups";
import {
  type CSSProperties,
  type KeyboardEvent,
  memo,
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { decodeJson, isBoolean, isJsonObject } from "../api/parse";
import { vaultLabel } from "../lib/labels";
import type {
  OntologyEditSessionResponse,
  OntologySummaryResponse,
  ViewCatalogEntry,
} from "../api/types";
import { PSEUDO_TYPE_ALL, PSEUDO_TYPE_ISSUES, PSEUDO_TYPE_MODIFIED } from "./notesRoute";
import { summaryCountsKnown, useValidationQuery, useViewCatalogQuery } from "./useNotesQueries";
import { useValidationScopeSummaries, validationScopeKey } from "./useValidationScopeSummaries";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { validationHealth } from "./validation/validationPresentation";
import { groupStandaloneViews } from "./viewCatalog";

type RailDepthStyle = CSSProperties & { "--ontology-type-depth": number };

const EXPANDED_KEY = "rhizome:notes:left-rail:expanded:v1";

function isExpandedMap(value: unknown): value is Record<string, boolean> {
  return isJsonObject(value) && Object.values(value).every(isBoolean);
}

// Sections and type groups stay the way the user left them across reloads.
function readExpanded(): Record<string, boolean> {
  try {
    return decodeJson(window.localStorage.getItem(EXPANDED_KEY), isExpandedMap) ?? {};
  } catch {
    return {};
  }
}

export type NotesLeftRailProps = {
  active?: boolean;
  /** The vault this browser tab serves; Rhizome Desktop names it in its toolbar instead. */
  vaultName?: string | null;
  vaultPath?: string | null;
  summary: OntologySummaryResponse | null;
  selectedType: string | null;
  activeCollection: string | null;
  activeViewID: string | null;
  editSession: OntologyEditSessionResponse | null;
  touchedPaths: Set<string>;
  activeGroup?: string | null;
  /** Recently changed is showing (All notes in its Briefing). */
  recentActive?: boolean;
  onSelectGroup?: (group: string) => void;
  onSelectCollection: (typeName: string) => void;
  onOpenRecent?: () => void;
  onOpenConfiguredView: (view: ViewCatalogEntry) => void;
};

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
  caret: "M6 4l4 4-4 4",
} as const;

function RailIcon({ name }: { name: keyof typeof ICONS }) {
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

export const NotesLeftRail = memo(function NotesLeftRail({
  active = true,
  vaultName,
  vaultPath,
  summary,
  selectedType,
  activeCollection,
  activeViewID,
  editSession,
  touchedPaths,
  onSelectCollection,
  activeGroup,
  recentActive = false,
  onSelectGroup,
  onOpenRecent,
  onOpenConfiguredView,
}: NotesLeftRailProps) {
  const [expanded, setExpanded] = useState(readExpanded);
  const railRef = useRef<HTMLElement>(null);
  const lastFocused = useRef<HTMLElement | null>(null);
  const catalogQuery = useViewCatalogQuery(active);
  const validationQuery = useValidationQuery(active);
  const catalog = catalogQuery.data?.views ?? [];
  const standaloneGroups = useMemo(() => groupStandaloneViews(catalog), [catalog]);
  const railGroups = useMemo(() => buildRailGroups(summary), [summary]);

  const totalNotes = summary && summaryCountsKnown(summary) ? summary.totalNotes : null;

  const activeAncestors = useMemo(
    () => activeRailAncestors(railGroups, selectedType),
    [railGroups, selectedType],
  );

  const activeAncestorKey = [...activeAncestors].sort().join("\n");

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
      window.localStorage.setItem(EXPANDED_KEY, JSON.stringify(expanded));
    } catch {
      // Browser storage is optional; the rail keeps its state until reload.
    }
  }, [expanded]);

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

  const validationScopes = useMemo(
    () =>
      flattenRailNodes(railGroups.flatMap((group) => group.children)).map((node) =>
        node.kind === "type"
          ? { kind: "type" as const, key: node.type.name }
          : { kind: "interface" as const, key: node.interface.name },
      ),
    [railGroups],
  );

  const validationSummaries = useValidationScopeSummaries(
    validationQuery.data?.snapshot?.generation ?? null,
    validationScopes,
  );

  const issueCount = validationQuery.data?.snapshot?.issueCount;
  const health = validationHealth(validationQuery.data ?? null);

  // A group that holds configured views starts open so its views stay one click away.
  const isExpanded = (key: string, openByDefault = false) => expanded[key] ?? openByDefault;

  const toggle = (key: string, open = isExpanded(key)) =>
    setExpanded((current) => ({ ...current, [key]: !open }));

  const typeGroupNames = new Set(railGroups.map((group) => group.name));
  const viewsByGroup = new Map(standaloneGroups.map((group) => [group.group, group.views]));

  // Views whose group matches a type group render inside it, so a group is named once.
  const standaloneViews = standaloneGroups
    .filter((group) => !typeGroupNames.has(group.group))
    .flatMap((group) => group.views);

  const viewsOpen = isExpanded("views", true);

  const renderView = (view: ViewCatalogEntry) => (
    <div className="ontology-type-row-wrap" key={view.id}>
      <button
        type="button"
        data-rail-row
        className={`ontology-view-row${activeViewID === view.id ? " is-selected" : ""}`}
        aria-current={activeViewID === view.id ? "page" : undefined}
        onClick={() => onOpenConfiguredView(view)}
      >
        <RailIcon name="view" />
        <span className="ontology-view-row__label">{view.name}</span>
      </button>
    </div>
  );

  const caret = (key: string, open: boolean, name: string, onToggle = () => toggle(key, open)) => (
    <button
      type="button"
      className="ontology-type-row__collapse"
      aria-label={`${open ? "Collapse" : "Expand"} ${name}`}
      aria-expanded={open}
      // Right and Left on the row open and close it, so the caret is not a Tab stop.
      tabIndex={-1}
      onClick={onToggle}
    >
      <RailIcon name="caret" />
    </button>
  );

  const renderNode = (node: RailNode, depth = 0) => {
    const item = node.kind === "type" ? node.type : node.interface;
    const embedded = node.kind === "type" && node.type.role === "embedded";

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
            caret(key, expandedNode, label(item))
          ) : (
            <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
          )}
          <button
            type="button"
            data-rail-row
            disabled={embedded}
            aria-label={[
              embedded || totalNotes === null ? label(item) : `${label(item)} (${item.count})`,
              typeIssues ? `${typeIssues} validation ${typeIssues === 1 ? "issue" : "issues"}` : "",
            ]
              .filter(Boolean)
              .join(", ")}
            aria-current={activeCollection === item.name ? "page" : undefined}
            className={`ontology-type-row${depth > 1 ? " ontology-type-row--child" : ""}${node.kind === "interface" ? " ontology-type-row--interface" : ""}${embedded ? " ontology-type-row--embedded" : ""}${activeCollection === item.name ? " is-selected" : ""}`}
            onClick={() => onSelectCollection(item.name)}
          >
            <span className="ontology-type-row__label">{label(item)}</span>
            <ValidationIssueBadge
              count={typeIssues}
              health={health}
              presenceOnly
              label={`${typeIssues ?? "Unknown"} validation issues in ${label(item)}`}
            />
            {!embedded && totalNotes !== null && (
              <span className="ontology-type-row__count">{item.count}</span>
            )}
          </button>
        </div>
        {hasChildren && expandedNode && node.children.map((child) => renderNode(child, depth + 1))}
      </div>
    );
  };

  const quickRow = (
    current: boolean,
    icon: keyof typeof ICONS,
    text: string,
    accessibleName: string,
    onClick: () => void,
    trailing?: ReactNode,
    tone = "",
  ) => (
    <div className="ontology-type-row-wrap">
      <button
        type="button"
        data-rail-row
        className={`ontology-type-row ontology-type-row--pseudo${tone}${current ? " is-selected" : ""}`}
        aria-label={accessibleName}
        aria-current={current ? "page" : undefined}
        onClick={onClick}
      >
        <RailIcon name={icon} />
        <span className="ontology-type-row__label">{text}</span>
        {trailing}
      </button>
    </div>
  );

  return (
    <aside
      ref={railRef}
      className="notes-left-rail"
      aria-label="Notes navigation"
      onKeyDown={moveRailFocus}
      onFocus={(event) => {
        if (!event.target.matches(ROW)) return;
        lastFocused.current = event.target;

        for (const row of event.currentTarget.querySelectorAll<HTMLElement>(ROW))
          row.tabIndex = row === event.target ? 0 : -1;
      }}
    >
      {vaultName ? (
        <div className="notes-left-rail__header" title={vaultPath ?? undefined}>
          {vaultLabel(vaultName)}
        </div>
      ) : null}
      <section className="notes-left-rail__collections" role="region" aria-label="Collections">
        <div className="ontology-type-list">
          {quickRow(
            activeCollection === PSEUDO_TYPE_ALL && !recentActive,
            "home",
            "All notes",
            totalNotes === null ? "All notes" : `All notes (${totalNotes})`,
            () => onSelectCollection(PSEUDO_TYPE_ALL),
            totalNotes !== null && <span className="ontology-type-row__count">{totalNotes}</span>,
          )}
          {onOpenRecent &&
            quickRow(recentActive, "clock", "Recently changed", "Recently changed", onOpenRecent)}
          {quickRow(
            activeCollection === PSEUDO_TYPE_ISSUES,
            "alert",
            "Problems",
            issueCount ? `Problems ${issueCount}` : "Problems",
            () => onSelectCollection(PSEUDO_TYPE_ISSUES),
            issueCount == null ? (
              <ValidationIssueBadge count={issueCount} health={health} label="Validation status" />
            ) : (
              <span className={`ontology-type-row__count${issueCount > 0 ? " is-risk" : ""}`}>
                {issueCount}
              </span>
            ),
            " ontology-type-row--issues",
          )}
          {editSession?.hasUncommittedChanges &&
            quickRow(
              activeCollection === PSEUDO_TYPE_MODIFIED,
              "edit",
              "Changes",
              `Changes ${touchedPaths.size}`,
              () => onSelectCollection(PSEUDO_TYPE_MODIFIED),
              <span className="ontology-type-row__count">{touchedPaths.size}</span>,
              " ontology-type-row--modified",
            )}
          {standaloneViews.length > 0 && (
            <nav
              className={`ontology-type-group ontology-type-group--presentation${viewsOpen ? " is-expanded" : " is-collapsed"}`}
              aria-label="Views"
            >
              <div className="ontology-type-row-wrap notes-left-rail__section">
                <button
                  type="button"
                  data-rail-row
                  className="ontology-type-row ontology-type-row--group"
                  aria-expanded={viewsOpen}
                  onClick={() => toggle("views", viewsOpen)}
                >
                  <span className="ontology-type-row__collapse" aria-hidden="true">
                    <RailIcon name="caret" />
                  </span>
                  Views
                </button>
              </div>
              {viewsOpen && standaloneViews.map(renderView)}
            </nav>
          )}
          {railGroups.map((group) => {
            const key = `group:${group.name}`;
            const groupViews = viewsByGroup.get(group.name);
            const expandedGroup = isExpanded(key, Boolean(groupViews));

            return (
              <div
                className={`ontology-type-group ontology-type-group--presentation${expandedGroup ? " is-expanded" : " is-collapsed"}`}
                key={group.name}
              >
                <div className="ontology-type-row-wrap notes-left-rail__section">
                  {caret(key, expandedGroup, group.name)}
                  <button
                    type="button"
                    data-rail-row
                    className={`ontology-type-row ontology-type-row--group${activeGroup === group.name ? " is-selected" : ""}`}
                    aria-current={activeGroup === group.name ? "page" : undefined}
                    onClick={() => onSelectGroup?.(group.name)}
                  >
                    {group.name}
                  </button>
                </div>
                {expandedGroup && groupViews && (
                  <nav className="notes-left-rail__nested-views" aria-label={`${group.name} views`}>
                    {groupViews.map(renderView)}
                  </nav>
                )}
                {expandedGroup && group.children.map((node) => renderNode(node, 1))}
              </div>
            );
          })}
        </div>
      </section>
    </aside>
  );
});
