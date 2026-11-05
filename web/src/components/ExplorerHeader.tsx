import { Info, Search } from "lucide-react";
import type { GraphResponse, SearchResponse } from "../api/types";
import { folderCoverage } from "./explorerNavigation";

type Props = {
  graph: GraphResponse;
  focusedPath?: string;
  filePath?: string;
  graphScopeLabel: string;
  displaySlot: (element: HTMLDivElement | null) => void;
  searchActive: boolean;
  searching: boolean;
  searchError: boolean;
  matchCount: number;
  searchOnlyMatches: boolean;
  setSearchOnlyMatches: (value: boolean) => void;
  query: string;
  setQuery: (value: string) => void;
  clearSearch: () => void;
  runSearch: () => void;
  showSuggestions: boolean;
  setShowSuggestions: (value: boolean) => void;
  suggestions: NonNullable<SearchResponse["matches"]>;
  onSelectFile: (path: string, kind?: string) => void;
  retrySearch: () => void;
};

export function ExplorerHeader({
  graph,
  focusedPath,
  filePath,
  graphScopeLabel,
  displaySlot: setDisplaySlot,
  searchActive,
  searching,
  searchError,
  matchCount,
  searchOnlyMatches,
  setSearchOnlyMatches,
  query,
  setQuery,
  clearSearch,
  runSearch,
  showSuggestions,
  setShowSuggestions,
  suggestions,
  onSelectFile,
  retrySearch,
}: Props) {
  const counts = (path?: string) => {
    const nodes = path
      ? graph.nodes.filter((n) => (n.notePath || n.path || "").startsWith(`${path}/`))
      : graph.nodes;

    return `${nodes.filter((n) => n.kind === "note").length.toLocaleString("en-US")} notes · ${nodes.filter((n) => n.kind === "code").length.toLocaleString("en-US")} code files`;
  };

  return (
    <header className="view-header explorer-header">
      <div className="view-header__title-row">
        <h2>Explorer</h2>
        <span className="view-header__count num">
          {filePath && <span className="explorer-header__breadcrumb">File neighbourhood · </span>}
          {focusedPath && (
            <span className="explorer-header__breadcrumb">
              {focusedPath.split("/").join(" / ")} ·{" "}
            </span>
          )}
          {graph.nodes.length ? counts(focusedPath) : ""}
          {focusedPath && <span className="explorer-header__totals"> of {counts()}</span>}
        </span>
        <button
          type="button"
          className="explorer-count-info"
          aria-label="About the displayed graph"
          title={`${graphScopeLabel}. ${folderCoverage(graph)}`}
        >
          <Info size={16} strokeWidth={1.5} absoluteStrokeWidth aria-hidden="true" />
        </button>
        <div className="explorer-bar__display" ref={setDisplaySlot} />
        <div
          className="view-header__tools explorer-bar__search"
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) setShowSuggestions(false);
          }}
        >
          {searchActive && !searching && !searchError ? (
            <span className="explorer-bar__matches" role="status">
              {matchCount} {matchCount === 1 ? "match" : "matches"}
            </span>
          ) : null}
          {searchActive ? (
            <label className="explorer-bar__filter">
              <input
                type="checkbox"
                checked={searchOnlyMatches}
                onChange={(e) => setSearchOnlyMatches(e.target.checked)}
              />
              Only matches
            </label>
          ) : null}
          <label className="configured-view__find explorer-bar__find">
            <Search className="configured-view__tool-icon" aria-hidden="true" strokeWidth={1.75} />
            <input
              className="configured-view__search"
              type="search"
              aria-label="Search notes and code"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);

                if (e.target.value.trim() === "") {
                  clearSearch();
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  runSearch();
                  setShowSuggestions(false);
                }

                if (e.key === "Escape") {
                  setQuery("");
                  setShowSuggestions(false);
                  clearSearch();
                }
              }}
              onFocus={() => {
                setShowSuggestions(true);
              }}
              placeholder={searching ? "Searching…" : "Search notes and code"}
            />
          </label>
          {showSuggestions && suggestions.length > 0 && (
            <div className="explorer-suggestions">
              {suggestions.map((suggestion) => (
                <button
                  type="button"
                  key={suggestion.path}
                  onMouseDown={(e) => {
                    e.preventDefault();
                  }}
                  onClick={() => {
                    if (suggestion.path) onSelectFile(suggestion.path, suggestion.kind);
                    setShowSuggestions(false);
                  }}
                >
                  <span className="explorer-suggestions__path">{suggestion.path}</span>
                  <span className="explorer-suggestions__kind">{suggestion.kind}</span>
                </button>
              ))}
            </div>
          )}
          {searchError ? (
            <div className="explorer-bar__search-error" role="alert">
              Search unavailable.{" "}
              <button type="button" onClick={() => retrySearch()}>
                Retry
              </button>
            </div>
          ) : null}
        </div>
      </div>
    </header>
  );
}
