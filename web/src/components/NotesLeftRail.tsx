import {
  buildRailGroups,
  flattenRailNodes,
  label,
  type RailGroup,
  type RailNode,
} from "./noteRailGroups";
import { displayTitle } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import {
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent,
  memo,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

import { publicOntologyListItemRef } from "../api/client";
import { decodeJson, isBoolean, isJsonObject } from "../api/parse";
import type {
  OntologyEditSessionResponse,
  OntologySummaryResponse,
  ValidationHealth,
  ViewCatalogEntry,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { queryErrorMessage } from "../views/viewErrors";
import {
  isPseudoType,
  PSEUDO_TYPE_ALL,
  PSEUDO_TYPE_ISSUES,
  PSEUDO_TYPE_MODIFIED,
  splitNoteTarget,
} from "./notesRoute";
import {
  summaryCountsKnown,
  useOntologyTypeQuery,
  useValidationQuery,
  useViewCatalogQuery,
} from "./useNotesQueries";
import { usePopoverDismiss } from "./focusManagement";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { useValidationScopeSummaries, validationScopeKey } from "./useValidationScopeSummaries";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { validationHealth } from "./validation/validationPresentation";
import { useTypeLabel } from "./typeLabels";
import { groupStandaloneViews } from "./viewCatalog";

type RailDepthStyle = CSSProperties & { "--ontology-type-depth": number };

const EXPANDED_KEY = "rhizome:notes:left-rail:expanded:v1";

function isExpandedMap(value: unknown): value is Record<string, boolean> {
  return isJsonObject(value) && Object.values(value).every(isBoolean);
}

// Type groups stay the way the user left them across reloads.
function readExpanded(): Record<string, boolean> {
  try {
    return decodeJson(window.localStorage.getItem(EXPANDED_KEY), isExpandedMap) ?? {};
  } catch {
    return {};
  }
}

export type NotesLeftRailProps = {
  active?: boolean;
  summary: OntologySummaryResponse | null;
  selectedType: string | null;
  activeCollection: string | null;
  activeViewID: string | null;
  activeTabID: string;
  editSession: OntologyEditSessionResponse | null;
  touchedPaths: Set<string>;
  findTabByPath: (path: string) => { id: string } | undefined;
  activeGroup?: string | null;
  onSelectGroup?: (group: string) => void;
  onSelectCollection: (typeName: string) => void;
  onOpenConfiguredView: (view: ViewCatalogEntry) => void;
  onOpenNote: (path: string, mode: OpenMode) => void;
  /** A type or interface collection view is showing, which lists the records itself. */
  collectionView?: boolean;
};

function matchesFilter(values: (string | undefined)[], filter: string): boolean {
  return !filter || values.some((value) => value?.toLocaleLowerCase().includes(filter));
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

type RailNoteHandlers = {
  open: (event: MouseEvent<HTMLButtonElement>, path: string) => void;
  move: (event: KeyboardEvent<HTMLButtonElement>, path: string) => void;
  focus: (path: string) => void;
  openBeside: (path: string) => void;
  preview: (path: string, mode: "stack" | "beside") => void;
  register: (path: string, element: HTMLButtonElement | null) => void;
};

const RailNoteRow = memo(function RailNoteRow({
  path,
  title,
  detail,
  type,
  issueCount,
  health,
  active,
  modified,
  keyboardSelected,
  tabbable,
  handlers,
}: {
  path: string;
  title: string;
  detail: string;
  type?: string;
  issueCount?: number;
  health: ValidationHealth;
  active: boolean;
  modified: boolean;
  keyboardSelected: boolean;
  /** The list is one Tab stop; arrow keys move between rows. */
  tabbable: boolean;
  handlers: RailNoteHandlers;
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target: path,
    open: handlers.preview,
  });

  return (
    <div
      className={`ontology-note ontology-note-list__item${active ? " is-active" : ""}${modified ? " is-modified" : ""}${keyboardSelected ? " is-keyboard-selected" : ""}`}
    >
      <button
        {...previewTrigger.triggerProps}
        ref={(element) => {
          previewTrigger.triggerProps.ref.current = element;
          handlers.register(path, element);
        }}
        type="button"
        className="ontology-note__main"
        tabIndex={tabbable ? 0 : -1}
        aria-keyshortcuts="Enter O X"
        onClick={(event) => {
          previewTrigger.close();
          handlers.open(event, path);
        }}
        onFocus={() => {
          previewTrigger.triggerProps.onFocus();
          handlers.focus(path);
        }}
        onKeyDown={(event) => {
          previewTrigger.triggerProps.onKeyDown(event);

          if (["enter", "o", "x"].includes(event.key.toLowerCase())) previewTrigger.close();
          handlers.move(event, path);
        }}
      >
        <div className="ontology-note__title">{title || path}</div>
        <div className="ontology-note__meta">
          <span className="ontology-note__path">
            <bdo dir="ltr">{detail || path}</bdo>
          </span>
          {type && <span className="ontology-note__type-mini">{type}</span>}
          <ValidationIssueBadge
            count={issueCount}
            health={health}
            presenceOnly
            label={`${issueCount ?? "Unknown"} validation issues in ${title || path}`}
          />
        </div>
      </button>
      <button
        type="button"
        className="ontology-compare-icon"
        aria-label="Open beside"
        title="Open beside (O)"
        // The row's O and X keys open beside, so this stays out of the Tab order.
        tabIndex={-1}
        onClick={() => {
          previewTrigger.close();
          handlers.openBeside(path);
        }}
      >
        ⇆
      </button>
      {previewTrigger.preview}
    </div>
  );
});

export function NotesLeftRail({
  active = true,
  summary,
  selectedType,
  activeCollection,
  activeViewID,
  activeTabID,
  editSession,
  touchedPaths,
  findTabByPath,
  onSelectCollection,
  activeGroup,
  onSelectGroup,
  onOpenConfiguredView,
  onOpenNote,
  collectionView = false,
}: NotesLeftRailProps) {
  const [listFilter, setListFilter] = useState("");
  // Typing stays responsive while a long list re-filters behind it.
  const deferredListFilter = useDeferredValue(listFilter);
  const [sort, setSort] = useState<"title" | "recent" | "relations">("title");
  const [onlyIssues, setOnlyIssues] = useState(false);
  const [onlyModified, setOnlyModified] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [expanded, setExpanded] = useState(readExpanded);
  const [keyboardPath, setKeyboardPath] = useState<string | null>(null);
  // A type's collection view already lists its records, so the rail's copy starts collapsed.
  const [listOpen, setListOpen] = useState(false);
  const buttonRefs = useRef(new Map<string, HTMLButtonElement>());
  const filterRef = useRef<HTMLInputElement>(null);
  const settingsRef = useRef<HTMLDivElement>(null);
  usePopoverDismiss(settingsOpen, settingsRef, () => setSettingsOpen(false));

  const typeQuery = useOntologyTypeQuery(
    selectedType,
    editSession,
    active && selectedType !== PSEUDO_TYPE_ISSUES && selectedType !== PSEUDO_TYPE_MODIFIED,
  );

  const catalogQuery = useViewCatalogQuery(active);
  const typeLabel = useTypeLabel();
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
    setListFilter("");
    setOnlyIssues(false);
    setOnlyModified(false);
    setListOpen(false);
  }, [selectedType]);
  useEffect(() => {
    setExpanded((current) => {
      const next = { ...current };
      let changed = false;

      for (const key of activeAncestorKey.split("\n")) {
        if (!key) continue;

        if (next[key]) continue;
        next[key] = true;
        changed = true;
      }

      return changed ? next : current;
    });
  }, [activeAncestorKey, selectedType]);
  useEffect(() => {
    if (!editSession?.hasUncommittedChanges) setOnlyModified(false);
  }, [editSession?.hasUncommittedChanges]);
  useEffect(() => {
    try {
      window.localStorage.setItem(EXPANDED_KEY, JSON.stringify(expanded));
    } catch {
      // Browser storage is optional; the rail keeps its state until reload.
    }
  }, [expanded]);

  const validationScopes = useMemo(
    () => [
      ...flattenRailNodes(railGroups.flatMap((group) => group.children)).map((node) =>
        node.kind === "type"
          ? { kind: "type" as const, key: node.type.name }
          : { kind: "interface" as const, key: node.interface.name },
      ),
      ...(typeQuery.data?.notes ?? []).map((note) => ({
        kind: "note" as const,
        key: note.path,
      })),
    ],
    [railGroups, typeQuery.data?.notes],
  );

  const validationSummaries = useValidationScopeSummaries(
    validationQuery.data?.snapshot?.generation ?? null,
    validationScopes,
  );

  const notes = useMemo(() => {
    const filter = deferredListFilter.trim().toLocaleLowerCase();
    let next = [...(typeQuery.data?.notes ?? [])];

    if (filter)
      next = next.filter((note) =>
        matchesFilter([note.title, note.path, publicTypeName(note.resolvedType)], filter),
      );

    if (onlyIssues)
      next = next.filter((note) => {
        const count = validationSummaries.summaries.get(
          validationScopeKey({ kind: "note", key: note.path }),
        )?.issueCount;

        return count === undefined || count > 0;
      });

    if (onlyModified) next = next.filter((note) => touchedPaths.has(note.path));

    return next.sort((a, b) =>
      sort === "recent"
        ? (b.updatedAt || 0) - (a.updatedAt || 0)
        : sort === "relations"
          ? (b.relationCount || 0) - (a.relationCount || 0)
          : (a.title || a.path).localeCompare(b.title || b.path),
    );
  }, [
    deferredListFilter,
    onlyIssues,
    onlyModified,
    sort,
    touchedPaths,
    typeQuery.data?.notes,
    validationSummaries.summaries,
  ]);

  const paths = notes.map(publicOntologyListItemRef);
  // The list is one Tab stop; arrow keys and j/k move within it.
  const tabbablePath = keyboardPath && paths.includes(keyboardPath) ? keyboardPath : paths[0];

  const open = (event: MouseEvent<HTMLButtonElement>, path: string) => {
    setKeyboardPath(path);
    onOpenNote(path, event.metaKey || event.ctrlKey ? "beside" : "activate");
  };

  const move = (event: KeyboardEvent<HTMLButtonElement>, path: string) => {
    const key = event.key.toLowerCase();

    if (key === "enter" || key === "o" || key === "x") {
      event.preventDefault();
      onOpenNote(path, key === "enter" ? "activate" : "beside");

      return;
    }

    const delta =
      key === "arrowdown" || key === "j" ? 1 : key === "arrowup" || key === "k" ? -1 : 0;

    if (!delta) return;
    event.preventDefault();
    const index = Math.max(0, paths.indexOf(path));

    if (delta < 0 && index === 0) {
      filterRef.current?.focus();

      return;
    }

    const next = paths[Math.max(0, Math.min(paths.length - 1, index + delta))];

    if (next) {
      setKeyboardPath(next);
      buttonRefs.current.get(next)?.focus();
    }
  };

  const enterListFromFilter = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "Enter") return;
    const firstPath = paths[0];

    if (!firstPath) return;
    event.preventDefault();
    setKeyboardPath(firstPath);

    if (event.key === "Enter") onOpenNote(firstPath, "activate");
    else buttonRefs.current.get(firstPath)?.focus();
  };

  // Rows are memoized so opening a note re-renders two rows, not thousands;
  // their handlers stay stable and read the latest list through this ref.
  const latestRowActions = useRef({ open, move, onOpenNote });
  latestRowActions.current = { open, move, onOpenNote };

  const rowHandlers = useMemo<RailNoteHandlers>(
    () => ({
      open: (event, path) => latestRowActions.current.open(event, path),
      move: (event, path) => latestRowActions.current.move(event, path),
      focus: setKeyboardPath,
      openBeside: (path) => latestRowActions.current.onOpenNote(path, "beside"),
      preview: (path, mode) =>
        latestRowActions.current.onOpenNote(path, mode === "beside" ? "beside" : "activate"),
      register: (path, element) => {
        if (element) buttonRefs.current.set(path, element);
        else buttonRefs.current.delete(path);
      },
    }),
    [],
  );

  const issueCount = validationQuery.data?.snapshot?.issueCount;
  const selectedInterface = summary?.interfaces?.find((item) => item.name === selectedType);

  const selectedLabel =
    (selectedInterface ?? summary?.types?.find((item) => item.name === selectedType))?.label ||
    selectedType;

  // A single-type list does not repeat the type on every row.
  const showRowType = selectedType === PSEUDO_TYPE_ALL || Boolean(selectedInterface);
  const health = validationHealth(validationQuery.data ?? null);

  // A group that holds configured views starts open so its views stay one click away.
  const isExpanded = (key: string, openByDefault = false) => expanded[key] ?? openByDefault;

  const toggle = (key: string, open = isExpanded(key)) =>
    setExpanded((current) => ({ ...current, [key]: !open }));

  const typeGroupNames = new Set(railGroups.map((group) => group.name));
  const viewsByGroup = new Map(standaloneGroups.map((group) => [group.group, group.views]));

  const renderView = (view: ViewCatalogEntry) => (
    <button
      type="button"
      key={view.id}
      className={`ontology-view-row${activeViewID === view.id ? " is-selected" : ""}`}
      onClick={() => onOpenConfiguredView(view)}
    >
      <span className="ontology-view-row__icon" aria-hidden="true">
        ▦
      </span>
      <span className="ontology-view-row__label">{view.name}</span>
    </button>
  );

  // Built once per change that matters: parents re-render this rail often,
  // and thousands of row elements are the cost even when rows are memoized.
  const noteRows = useMemo(
    () =>
      notes.map((note) => {
        const path = publicOntologyListItemRef(note);
        const notePath = splitNoteTarget(path).path;

        return (
          <RailNoteRow
            key={path}
            path={path}
            title={displayTitle(note.title)}
            detail={note.path}
            type={showRowType ? typeLabel(note.resolvedType) : undefined}
            issueCount={
              validationSummaries.summaries.get(
                validationScopeKey({ kind: "note", key: note.path }),
              )?.issueCount
            }
            health={health}
            active={findTabByPath(notePath)?.id === activeTabID}
            modified={touchedPaths.has(notePath)}
            keyboardSelected={keyboardPath === path}
            tabbable={path === tabbablePath}
            handlers={rowHandlers}
          />
        );
      }),
    [
      activeTabID,
      findTabByPath,
      health,
      keyboardPath,
      notes,
      rowHandlers,
      showRowType,
      tabbablePath,
      typeLabel,
      touchedPaths,
      validationSummaries.summaries,
    ],
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
            <button
              type="button"
              className="ontology-type-row__collapse"
              aria-label={`${expandedNode ? "Collapse" : "Expand"} ${label(item)}`}
              aria-expanded={expandedNode}
              onClick={() => toggle(key)}
            >
              {expandedNode ? "▾" : "▸"}
            </button>
          ) : (
            <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
          )}
          <button
            type="button"
            disabled={embedded}
            aria-label={
              embedded || totalNotes === null ? label(item) : `${label(item)} (${item.count})`
            }
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

  const showNoteList =
    !activeGroup && selectedType !== PSEUDO_TYPE_ISSUES && selectedType !== PSEUDO_TYPE_MODIFIED;

  const collectionShowing = collectionView && Boolean(selectedType) && !isPseudoType(selectedType);

  const listCollapsed = collectionShowing && !listOpen;

  const listTitle =
    selectedType === PSEUDO_TYPE_ALL
      ? "All notes"
      : selectedType === PSEUDO_TYPE_ISSUES
        ? "Notes with issues"
        : selectedType === PSEUDO_TYPE_MODIFIED
          ? "Modified notes"
          : selectedType
            ? `${selectedLabel} notes`
            : "Notes";

  return (
    <aside
      className={`ontology-rail notes-left-rail${listCollapsed ? " notes-left-rail--list-collapsed" : ""}`}
      aria-label="Notes navigation"
    >
      <section
        className="notes-left-rail__collections ontology-rail__navigation"
        role="region"
        aria-label="Collections"
      >
        {/* Views whose group matches a type group render inside it, so a group is named once. */}
        {standaloneGroups
          .filter((group) => !typeGroupNames.has(group.group))
          .map((group) => (
            <nav className="ontology-rail__views" aria-label={group.group} key={group.group}>
              <span className="ontology-rail__views-label" aria-hidden="true">
                {group.group}
              </span>
              {group.views.map(renderView)}
            </nav>
          ))}
        <div className="ontology-type-list">
          <div className="ontology-type-row-wrap">
            <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
            <button
              type="button"
              className={`ontology-type-row ontology-type-row--pseudo ontology-type-row--issues${activeCollection === PSEUDO_TYPE_ISSUES ? " is-selected" : ""}`}
              aria-label={issueCount ? `Problems ${issueCount}` : "Problems"}
              onClick={() => onSelectCollection(PSEUDO_TYPE_ISSUES)}
            >
              <span className="ontology-type-row__label">Problems</span>
              <ValidationIssueBadge
                count={issueCount}
                health={health}
                label={issueCount ? `${issueCount} validation issues` : "Validation status"}
              />
            </button>
          </div>
          {editSession?.hasUncommittedChanges && (
            <div className="ontology-type-row-wrap">
              <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
              <button
                type="button"
                className={`ontology-type-row ontology-type-row--pseudo ontology-type-row--modified${activeCollection === PSEUDO_TYPE_MODIFIED ? " is-selected" : ""}`}
                aria-label={`Changes ${touchedPaths.size}`}
                onClick={() => onSelectCollection(PSEUDO_TYPE_MODIFIED)}
              >
                <span className="ontology-type-row__label">Changes</span>
                <span className="ontology-type-row__count">{touchedPaths.size}</span>
              </button>
            </div>
          )}
          <div className="ontology-type-row-wrap">
            <span className="ontology-type-row__collapse-spacer" aria-hidden="true" />
            <button
              type="button"
              className={`ontology-type-row ontology-type-row--pseudo${activeCollection === PSEUDO_TYPE_ALL ? " is-selected" : ""}`}
              aria-label={totalNotes === null ? "All notes" : `All notes (${totalNotes})`}
              onClick={() => onSelectCollection(PSEUDO_TYPE_ALL)}
            >
              <span className="ontology-type-row__label">All notes</span>
              {totalNotes !== null && (
                <span className="ontology-type-row__count">{totalNotes}</span>
              )}
            </button>
          </div>
          {railGroups.map((group) => {
            const key = `group:${group.name}`;
            const groupViews = viewsByGroup.get(group.name);
            const expandedGroup = isExpanded(key, Boolean(groupViews));

            return (
              <div
                className={`ontology-type-group ontology-type-group--presentation${expandedGroup ? " is-expanded" : " is-collapsed"}`}
                key={group.name}
              >
                <div className="ontology-type-row-wrap">
                  <button
                    type="button"
                    className="ontology-type-row__collapse"
                    aria-label={`${expandedGroup ? "Collapse" : "Expand"} ${group.name}`}
                    aria-expanded={expandedGroup}
                    onClick={() => toggle(key, expandedGroup)}
                  >
                    {expandedGroup ? "▾" : "▸"}
                  </button>
                  <button
                    type="button"
                    className={`ontology-type-row ontology-type-row--group${activeGroup === group.name ? " is-selected" : ""}`}
                    onClick={() => onSelectGroup?.(group.name)}
                  >
                    {group.name}
                  </button>
                </div>
                {expandedGroup && groupViews && (
                  <nav
                    className="ontology-rail__views ontology-rail__views--nested"
                    aria-label={`${group.name} views`}
                  >
                    {groupViews.map(renderView)}
                  </nav>
                )}
                {expandedGroup && group.children.map((node) => renderNode(node, 1))}
              </div>
            );
          })}
        </div>
      </section>
      {showNoteList && (
        <section
          className="notes-left-rail__notes ontology-rail__list"
          role="region"
          aria-label="Note list"
        >
          <div className="ontology-list__toolbar">
            <div className="ontology-list__title">
              {collectionShowing ? (
                <h2>
                  <button
                    type="button"
                    className="notes-left-rail__list-toggle"
                    aria-expanded={!listCollapsed}
                    onClick={() => setListOpen(listCollapsed)}
                  >
                    <span aria-hidden="true">{listCollapsed ? "▸" : "▾"}</span>
                    {listTitle}
                  </button>
                </h2>
              ) : (
                <h2>{listTitle}</h2>
              )}
              {typeQuery.data && (
                <span className="ontology-list__count">
                  {`${notes.length} / ${typeQuery.data.notes?.length ?? 0}`}
                </span>
              )}
            </div>
            <div className="ontology-list__filters" hidden={listCollapsed}>
              <input
                ref={filterRef}
                type="search"
                value={listFilter}
                onChange={(event) => setListFilter(event.target.value)}
                aria-label="Filter note list"
                placeholder="Filter notes…"
                onKeyDown={enterListFromFilter}
              />
              <div
                ref={settingsRef}
                className={`ontology-list-settings${settingsOpen ? " is-open" : ""}`}
              >
                <button
                  type="button"
                  className="ontology-list-settings__trigger"
                  aria-label="Open note list settings"
                  aria-expanded={settingsOpen}
                  onClick={() => setSettingsOpen((value) => !value)}
                >
                  ...
                </button>
                {settingsOpen && (
                  <div
                    className="ontology-list-settings__menu"
                    role="group"
                    aria-label="Note list settings"
                  >
                    <button
                      type="button"
                      aria-pressed={sort === "title"}
                      onClick={() => setSort("title")}
                    >
                      Title
                    </button>
                    <button
                      type="button"
                      aria-pressed={sort === "recent"}
                      onClick={() => setSort("recent")}
                    >
                      Recent
                    </button>
                    <button
                      type="button"
                      aria-pressed={sort === "relations"}
                      onClick={() => setSort("relations")}
                    >
                      Relations
                    </button>
                    <label>
                      <input
                        type="checkbox"
                        checked={onlyIssues}
                        onChange={(event) => setOnlyIssues(event.target.checked)}
                      />
                      Issues
                    </label>
                    {editSession?.hasUncommittedChanges && (
                      <label>
                        <input
                          type="checkbox"
                          checked={onlyModified}
                          onChange={(event) => setOnlyModified(event.target.checked)}
                        />
                        Modified
                      </label>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>
          <div className="ontology-list__body" hidden={listCollapsed}>
            {typeQuery.isError && !typeQuery.data ? (
              <div className="ontology-list__empty" role="alert">
                {queryErrorMessage(typeQuery.error)}{" "}
                <button type="button" onClick={() => void typeQuery.refetch()}>
                  Retry
                </button>
              </div>
            ) : typeQuery.isPending ? (
              <div className="ontology-list__empty" role="status">
                Loading notes…
              </div>
            ) : notes.length ? (
              noteRows
            ) : (
              <div className="ontology-list__empty">No notes match the current filter.</div>
            )}
          </div>
        </section>
      )}
    </aside>
  );
}
