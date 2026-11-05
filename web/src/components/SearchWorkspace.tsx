import { InfoTip } from "./InfoTip";
import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, searchWorkspace } from "../api/client";
import type { OntologyTypeSummary } from "../api/types";
import { queryKeys } from "../api/queryKeys";
import type { StagedSession } from "../staging/stagedQuery";
import type { OpenMode, SearchTab } from "./useNoteTabs";
import { normalizeSearchFilters, type SearchFilters } from "./searchState";
import { useSearchScrollRestoration } from "./searchState";
import { SearchBanner, FolderNotesWorkspace, RankedResults } from "./SearchResults";

const PAGE_SIZE = 40;

const TARGET_STATUS_LABELS = new Map([
  ["explicit_path", "Exact path"],
  ["inferred_path", "Exact path"],
  ["inferred_symbol", "Exact symbol"],
  ["ambiguous", "Ambiguous target"],
  ["downgraded", "Target downgraded"],
  ["unresolved", "Target unresolved"],
]);

export type SearchWorkspaceProps = {
  tab: SearchTab;
  active: boolean;
  restoreReady?: boolean;
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

function RankedSearchWorkspace({
  tab,
  active,
  restoreReady = true,
  types,
  onRefineSearch,
  onOpenNote,
  onScrollPosition,
}: SearchWorkspaceProps) {
  const [folderDraft, setFolderDraft] = useState(tab.filters.folder || "");
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

  const scrollRef = useSearchScrollRestoration(tab, active, restoreReady && Boolean(query.data));

  useEffect(() => {
    setFolderDraft(tab.filters.folder || "");
  }, [tab.id, tab.filters.folder, tab.filters.noteType, tab.filters.scope]);

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

  // One banner: a summary line when sources are missing, with each source's
  // own message behind its disclosure.
  // A single missing source is named by its warning's first clause ("Code
  // embeddings are unavailable"); the full message stays behind Details.
  const missingSources = availabilityWarnings.flatMap((warning) =>
    warning.message ? [warning.message.split(/[;.]/, 1)[0].trim()] : [],
  );

  const notices = [
    ...(incomplete && results.length > 0
      ? [
          missingSources.length === 1 && degradedLanes.length === 0
            ? `${missingSources[0]}. Showing the usable results.`
            : "Some search sources are unavailable. Showing the usable results.",
        ]
      : []),
    ...displayWarnings.flatMap((warning) => (warning.message ? [warning.message] : [])),
  ];

  const backgroundError = query.isError && !query.isFetchNextPageError && results.length > 0;
  const noteTypes = types.filter((type) => type.role === "note");

  const hasFilters =
    tab.filters.scope !== "all" || Boolean(tab.filters.noteType) || Boolean(tab.filters.folder);

  // Confidence, target resolution, and coverage describe the whole search, so they come from the
  // first page rather than whichever continuation page happens to be loaded last.
  const firstPage = pages[0];

  const statusSegments: { text: string; info?: string }[] = [
    {
      text: `Showing ${results.length}${total === null ? "" : ` of ${total}`} results${total !== null && total === response?.policy?.candidateWindow ? " (search limit)" : ""}`,
    },
  ];

  const targetLabel = TARGET_STATUS_LABELS.get(firstPage?.targetStatus ?? "");

  if (targetLabel) statusSegments.push({ text: targetLabel });
  const confidenceLevel = firstPage?.confidence?.level;

  if (confidenceLevel) {
    const reason = firstPage?.confidence?.reason;

    statusSegments.push({
      text: `${confidenceLevel[0].toUpperCase()}${confidenceLevel.slice(1)} confidence`,
      info: `Confidence is how much independent evidence supports the top results.${reason ? ` Here: ${reason}.` : ""}`,
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
        <div className="search-workspace__title-row">
          <h1>Search results</h1>
          <p className="search-workspace__query" title={tab.query}>
            for “{tab.query}”
          </p>
        </div>
        <div className="search-workspace__filters" aria-label="Search filters">
          <div className="segmented" role="group" aria-label="Search scope">
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
          </div>
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
            <button
              type="button"
              className="search-workspace__clear"
              onClick={() => onRefineSearch(tab.id, normalizeSearchFilters())}
            >
              Clear filters
            </button>
          ) : null}
        </div>
      </header>

      {!query.isPending && (notices.length > 0 || backgroundError) ? (
        <div className="search-workspace__warnings" aria-label="Search warnings">
          {backgroundError ? (
            <SearchBanner
              tone="alert"
              action={
                <button type="button" onClick={() => void query.refetch()}>
                  Retry refresh
                </button>
              }
            >
              Search refresh failed. The previous results remain visible.
            </SearchBanner>
          ) : null}
          {notices.length > 0 ? (
            <SearchBanner details={notices.slice(1)}>{notices[0]}</SearchBanner>
          ) : null}
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
                <span key={segment.text}>
                  {segment.text}
                  {segment.info ? (
                    <InfoTip label="About search confidence">{segment.info}</InfoTip>
                  ) : null}
                </span>
              ))}
            </span>
            {query.isFetching && !query.isFetchingNextPage ? <span>Refreshing…</span> : null}
          </div>
          <ol className="search-workspace__results">
            <RankedResults matches={results} query={tab.query} onOpenNote={onOpenNote} />
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
