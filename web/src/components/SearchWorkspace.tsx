import { publicTypeName } from "../lib/typeNames";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";

import {
  ApiError,
  publicOntologyListItemRef,
  publicWorkspaceRef,
  searchWorkspace,
} from "../api/client";
import type { OntologyTypeSummary, WorkspaceSearchMatch } from "../api/types";
import { queryKeys } from "../api/queryKeys";
import type { StagedSession } from "../staging/stagedQuery";
import { PSEUDO_TYPE_ALL } from "./notesRoute";
import { useOntologyTypeQuery } from "./useNotesQueries";
import { notifyLocationChange } from "./locationStore";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import type { OpenMode, SearchTab } from "./useNoteTabs";
import {
  ROOT_FOLDER,
  folderLabel,
  inFolder,
  normalizeSearchFilters,
  normalizeSearchQuery,
  type SearchFilters,
} from "./searchState";

const PAGE_SIZE = 40;

const TARGET_STATUS_LABELS = new Map([
  ["explicit_path", "Exact path"],
  ["inferred_path", "Exact path"],
  ["inferred_symbol", "Exact symbol"],
  ["ambiguous", "Ambiguous target"],
  ["downgraded", "Target downgraded"],
  ["unresolved", "Target unresolved"],
]);

function highlightLiteral(text: string, query: string) {
  const terms = normalizeSearchQuery(query)
    .split(" ")
    .filter(Boolean)
    .map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));

  if (terms.length === 0) return text;
  const matcher = new RegExp(`(${terms.join("|")})`, "gi");

  return text
    .split(matcher)
    .map((part, index) => (index % 2 === 1 ? <mark key={`${part}-${index}`}>{part}</mark> : part));
}

function excerptFor(match: WorkspaceSearchMatch): string | null {
  if (match.snippetStatus !== "available") return null;
  const value = match.snippet || "";

  return value.trim() ? value.trim() : null;
}

function resultLabel(match: WorkspaceSearchMatch): string {
  return match.title || match.symbol || match.fqn || match.path || "Untitled result";
}

function sourceLabel(match: WorkspaceSearchMatch): string {
  if (match.type === "code") return match.path || "Code source";

  return match.path || "Note source";
}

function isNoteMatch(match: WorkspaceSearchMatch): boolean {
  return match.type === "note" || match.path?.toLowerCase().endsWith(".md") === true;
}

function noteTargetForMatch(match: WorkspaceSearchMatch): string {
  const ref = match.linkTarget?.ref || match.nodeRef;

  return ref ? publicWorkspaceRef(ref) : match.path || "";
}

function openCodeSource(path: string, line?: number) {
  if (!path) return;
  const params = new URLSearchParams({ file: path });

  if (line && line > 0) params.set("line", String(line));
  window.history.pushState({}, "", `/explorer?${params.toString()}`);
  notifyLocationChange();
}

function NoteResultLink({
  target,
  children,
  onOpenNote,
}: {
  target: string;
  children: ReactNode;
  onOpenNote: SearchWorkspaceProps["onOpenNote"];
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    open: (path, mode) => onOpenNote(path, mode === "beside" ? "beside" : "activate"),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="search-workspace__result-link"
        onClick={(event) => {
          previewTrigger.close();
          onOpenNote(target, event.metaKey || event.ctrlKey ? "beside" : "activate");
        }}
      >
        {children}
      </button>
      {previewTrigger.preview}
    </>
  );
}

type SearchWorkspaceProps = {
  tab: SearchTab;
  active: boolean;
  types: OntologyTypeSummary[];
  editSession?: StagedSession;
  onRefineSearch: (id: string, filters: Partial<SearchFilters>, query?: string) => void;
  onOpenNote: (target: string, mode?: OpenMode) => void;
  onScrollPosition: (id: string, scrollTop: number) => void;
};

/** A search tab with no query lists its folder's notes instead of running ranked search. */
export function SearchWorkspace(props: SearchWorkspaceProps) {
  return props.tab.query ? (
    <RankedSearchWorkspace {...props} />
  ) : (
    <FolderNotesWorkspace {...props} />
  );
}

function FolderNotesWorkspace({
  tab,
  active,
  editSession = null,
  onRefineSearch,
  onOpenNote,
  onScrollPosition,
}: SearchWorkspaceProps) {
  const folder = tab.filters.folder || "";
  const [folderDraft, setFolderDraft] = useState(folder);
  const [queryDraft, setQueryDraft] = useState("");
  const scrollRef = useRef<HTMLElement>(null);
  const notesQuery = useOntologyTypeQuery(PSEUDO_TYPE_ALL, editSession, active);

  useEffect(() => {
    setFolderDraft(folder);
  }, [tab.id, folder]);

  useEffect(() => {
    if (active && scrollRef.current) scrollRef.current.scrollTop = tab.scrollTop;
  }, [active, tab.scrollTop]);

  const notes = useMemo(
    () =>
      (notesQuery.data?.notes ?? [])
        .filter((note) => inFolder(note.path, folder))
        .sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0)),
    [folder, notesQuery.data?.notes],
  );

  const applyFolder = () => {
    // Clearing the folder would leave nothing to list, so the field reverts instead.
    if (!normalizeSearchFilters({ folder: folderDraft }).folder) setFolderDraft(folder);
    else onRefineSearch(tab.id, { ...tab.filters, folder: folderDraft });
  };

  const submitQuery = () => {
    if (normalizeSearchQuery(queryDraft)) onRefineSearch(tab.id, tab.filters, queryDraft);
  };

  return (
    <main
      ref={scrollRef}
      className="search-workspace"
      aria-label={`Notes in ${folderLabel(folder)}`}
      onScroll={(event) => {
        if (active) onScrollPosition(tab.id, event.currentTarget.scrollTop);
      }}
    >
      <header className="search-workspace__header">
        <div>
          <h1>Notes in {folderLabel(folder)}</h1>
          {notesQuery.data ? (
            <p className="search-workspace__query">
              {notes.length} {notes.length === 1 ? "note" : "notes"}
            </p>
          ) : null}
        </div>
        <div className="search-workspace__filters" aria-label="Search filters">
          {/* Ranked search filters by path prefix, which cannot hold to the root's own notes. */}
          {folder !== ROOT_FOLDER && (
            <label>
              <span>Search</span>
              <input
                aria-label="Search this folder"
                placeholder="Search this folder"
                value={queryDraft}
                onChange={(event) => setQueryDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") submitQuery();
                }}
              />
            </label>
          )}
          <label>
            <span>Folder</span>
            <input
              aria-label="Search folder"
              value={folderDraft}
              onChange={(event) => setFolderDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") applyFolder();
              }}
              onBlur={applyFolder}
            />
          </label>
        </div>
      </header>
      {notesQuery.isError && !notesQuery.data ? (
        <div className="search-workspace__state" role="alert">
          <strong>Notes unavailable</strong>
          <span>The workspace could not list this folder's notes.</span>
          <button type="button" onClick={() => void notesQuery.refetch()}>
            Retry
          </button>
        </div>
      ) : notesQuery.isPending ? (
        <div className="search-workspace__state" role="status">
          Loading notes…
        </div>
      ) : notes.length === 0 ? (
        <div className="search-workspace__state" role="status">
          <strong>No notes in {folderLabel(folder)}</strong>
        </div>
      ) : (
        <ol className="search-workspace__results">
          {notes.map((note) => (
            <li key={note.path}>
              <article className="search-workspace__result">
                <div className="search-workspace__result-kind">
                  {publicTypeName(note.resolvedType) || "untyped"}
                </div>
                <NoteResultLink target={publicOntologyListItemRef(note)} onOpenNote={onOpenNote}>
                  {note.title || note.path}
                </NoteResultLink>
                <div className="search-workspace__result-source">{note.path}</div>
              </article>
            </li>
          ))}
        </ol>
      )}
    </main>
  );
}

function RankedSearchWorkspace({
  tab,
  active,
  types,
  onRefineSearch,
  onOpenNote,
  onScrollPosition,
}: SearchWorkspaceProps) {
  const [folderDraft, setFolderDraft] = useState(tab.filters.folder || "");
  const scrollRef = useRef<HTMLElement>(null);
  const queryClient = useQueryClient();

  const request = useMemo(
    () => ({
      scope: tab.filters.scope,
      noteType: tab.filters.noteType,
      folder: tab.filters.folder,
      limit: PAGE_SIZE,
    }),
    [tab.filters.folder, tab.filters.noteType, tab.filters.scope],
  );

  const queryKey = queryKeys.search.workspace(tab.query, request);

  const query = useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      searchWorkspace(tab.query, { ...request, continuationToken: pageParam || null }, { signal }),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.continuationToken || undefined,
    enabled: active,
  });

  useEffect(() => {
    setFolderDraft(tab.filters.folder || "");
  }, [tab.id, tab.filters.folder, tab.filters.noteType, tab.filters.scope]);

  useEffect(() => {
    if (active && scrollRef.current) scrollRef.current.scrollTop = tab.scrollTop;
  }, [active, tab.scrollTop]);

  const pages = query.data?.pages ?? [];
  const results = pages.flatMap((page) => page.matches || []);
  const response = pages.at(-1);
  const total = response?.total ?? null;
  const hasMore = query.hasNextPage;

  const continuationStale =
    query.isFetchNextPageError &&
    query.error instanceof ApiError &&
    query.error.code === "CONTINUATION_STALE";

  const degradedLanes =
    response?.lanes?.filter((lane) =>
      ["timed_out", "degraded", "canceled", "unknown"].includes(lane.status),
    ) ?? [];

  const displayWarnings =
    response?.warnings?.filter(
      (warning) => warning.code !== "scope_filter_applied" && warning.kind !== "result_shaping",
    ) ?? [];

  const availabilityWarnings = displayWarnings.filter((warning) =>
    ["retrieval_error", "retrieval_config", "index_unavailable", "index_state"].includes(
      warning.kind || "",
    ),
  );

  const incomplete = degradedLanes.length > 0 || availabilityWarnings.length > 0;
  const backgroundError = query.isError && !query.isFetchNextPageError && results.length > 0;
  const noteTypes = types.filter((type) => type.role === "note");

  const hasFilters =
    tab.filters.scope !== "all" || Boolean(tab.filters.noteType) || Boolean(tab.filters.folder);

  // Confidence, target resolution, and coverage describe the whole search, so they come from the
  // first page rather than whichever continuation page happens to be loaded last.
  const firstPage = pages[0];

  const statusSegments: { text: string; title?: string }[] = [
    {
      text:
        total !== null
          ? `${results.length} of ${total} ranked`
          : `${results.length} results loaded`,
    },
  ];

  const targetLabel = TARGET_STATUS_LABELS.get(firstPage?.targetStatus ?? "");

  if (targetLabel) statusSegments.push({ text: targetLabel });
  const confidenceLevel = firstPage?.confidence?.level;

  if (confidenceLevel) {
    statusSegments.push({
      text: `Confidence ${confidenceLevel}`,
      title: firstPage?.confidence?.reason || undefined,
    });
  }

  const missingCoverage = firstPage?.coverage?.missing;

  if (missingCoverage && missingCoverage.length > 0) {
    statusSegments.push({ text: `Missing: ${missingCoverage.join(", ")}` });
  }

  const applyFolder = () => {
    onRefineSearch(tab.id, normalizeSearchFilters({ ...tab.filters, folder: folderDraft }));
  };

  const refreshFromFirstPage = () => queryClient.resetQueries({ queryKey, exact: true });

  return (
    <main
      ref={scrollRef}
      className="search-workspace"
      aria-label={`Search results for ${tab.query}`}
      onScroll={(event) => {
        // Keeping the scroll container local to each retained tab means tab switches do not
        // disturb another search's position. The tab model remains serializable and reload-safe.
        if (active) onScrollPosition(tab.id, event.currentTarget.scrollTop);
      }}
    >
      <header className="search-workspace__header">
        <div>
          <h1>Search results</h1>
          <p className="search-workspace__query">{tab.query}</p>
        </div>
        <div className="search-workspace__filters" aria-label="Search filters">
          {(["all", "notes", "code"] as const).map((scope) => (
            <button
              key={scope}
              type="button"
              className={tab.filters.scope === scope ? "is-active" : undefined}
              aria-pressed={tab.filters.scope === scope}
              onClick={() =>
                onRefineSearch(tab.id, {
                  ...tab.filters,
                  scope,
                  noteType: scope === "notes" ? tab.filters.noteType : null,
                })
              }
            >
              {scope === "all" ? "All" : scope === "notes" ? "Notes" : "Code"}
            </button>
          ))}
          {noteTypes.length > 0 && tab.filters.scope !== "code" ? (
            <label>
              <span>Type</span>
              <select
                aria-label="Search note type"
                value={tab.filters.noteType || ""}
                onChange={(event) =>
                  onRefineSearch(tab.id, {
                    ...tab.filters,
                    noteType: event.target.value || null,
                  })
                }
              >
                <option value="">Any type</option>
                {noteTypes.map((type) => (
                  <option key={type.name} value={type.name}>
                    {type.label || type.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label>
            <span>Folder</span>
            <input
              aria-label="Search folder"
              placeholder="Anywhere"
              value={folderDraft}
              onChange={(event) => setFolderDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") applyFolder();
              }}
              onBlur={applyFolder}
            />
          </label>
          {hasFilters ? (
            <button type="button" onClick={() => onRefineSearch(tab.id, normalizeSearchFilters())}>
              Clear filters
            </button>
          ) : null}
        </div>
      </header>

      {!query.isPending &&
      (displayWarnings.length > 0 || (incomplete && results.length > 0) || backgroundError) ? (
        <div className="search-workspace__warnings" aria-label="Search warnings">
          {incomplete && results.length > 0 ? (
            <div className="search-workspace__warning" role="status">
              Some search sources are unavailable. Showing the usable results.
            </div>
          ) : null}
          {backgroundError ? (
            <div className="search-workspace__warning" role="alert">
              Search refresh failed. The previous results remain visible.
              <button type="button" onClick={() => void query.refetch()}>
                Retry refresh
              </button>
            </div>
          ) : null}
          {displayWarnings.map((warning) => (
            <div className="search-workspace__warning" key={warning.code} role="status">
              {warning.message}
            </div>
          ))}
        </div>
      ) : null}
      {query.isError && results.length === 0 ? (
        <div className="search-workspace__state" role="alert">
          <strong>Search unavailable</strong>
          <span>The workspace could not complete this search.</span>
          <button type="button" onClick={() => void query.refetch()}>
            Retry
          </button>
        </div>
      ) : query.isPending ? (
        <div className="search-workspace__state" role="status">
          Searching…
        </div>
      ) : results.length === 0 ? (
        <div className="search-workspace__state" role="status">
          <strong>{incomplete ? "Search incomplete" : "No matches"}</strong>
          <span>
            {incomplete
              ? "Some search sources were unavailable, and no usable matches were returned."
              : "No indexed source matches this query and filter set."}
          </span>
          {hasFilters ? (
            <button type="button" onClick={() => onRefineSearch(tab.id, normalizeSearchFilters())}>
              Clear filters
            </button>
          ) : null}
        </div>
      ) : (
        <>
          <div className="search-workspace__meta">
            <span className="search-workspace__status" aria-label="Search status">
              {statusSegments.map((segment) => (
                <span key={segment.text} title={segment.title}>
                  {segment.text}
                </span>
              ))}
            </span>
            {query.isFetching && !query.isFetchingNextPage ? <span>Refreshing…</span> : null}
          </div>
          <ol className="search-workspace__results">
            {results.map((match, index) => {
              const excerpt = excerptFor(match);
              const line = match.startLine && match.startLine > 0 ? match.startLine : undefined;

              return (
                <li key={`${match.type}:${match.path}:${match.anchorID || match.nodeID || index}`}>
                  <article className="search-workspace__result">
                    <div className="search-workspace__result-kind">
                      {match.type || "source"}
                      {publicTypeName(match.noteType) ? ` · ${publicTypeName(match.noteType)}` : ""}
                    </div>
                    {isNoteMatch(match) ? (
                      <NoteResultLink target={noteTargetForMatch(match)} onOpenNote={onOpenNote}>
                        {highlightLiteral(resultLabel(match), tab.query)}
                      </NoteResultLink>
                    ) : (
                      <button
                        type="button"
                        className="search-workspace__result-link"
                        onClick={() => openCodeSource(match.path || "", line)}
                      >
                        {highlightLiteral(resultLabel(match), tab.query)}
                      </button>
                    )}
                    <div className="search-workspace__result-source">
                      {sourceLabel(match)}
                      {line ? `:${line}` : ""}
                      {match.heading ? ` · ${match.heading}` : ""}
                    </div>
                    <p className={excerpt ? undefined : "is-missing"}>
                      {excerpt ? highlightLiteral(excerpt, tab.query) : "Excerpt unavailable."}
                    </p>
                  </article>
                </li>
              );
            })}
          </ol>
          {continuationStale ? (
            <button
              type="button"
              className="search-workspace__load-more"
              onClick={() => void refreshFromFirstPage()}
            >
              Results changed. Refresh search
            </button>
          ) : query.isFetchNextPageError ? (
            <button
              type="button"
              className="search-workspace__load-more"
              onClick={() => void query.fetchNextPage()}
            >
              Retry loading more
            </button>
          ) : hasMore ? (
            <button
              type="button"
              className="search-workspace__load-more"
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage ? "Loading…" : "Load more results"}
            </button>
          ) : (
            <div className="search-workspace__end">End of results</div>
          )}
        </>
      )}
    </main>
  );
}
