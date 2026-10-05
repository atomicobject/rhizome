import { useQueries, useQuery } from "@tanstack/react-query";
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import {
  getExpandedModuleGraph,
  getFileView,
  getGlobalGraph,
  getLocalGraph,
  getSuggestions,
  getTree,
  searchPaths as searchPathMatches,
} from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { FileView, GraphNode, GraphResponse, TreeEntry } from "../api/types";
import { encodeURLFragment } from "../lib/noteDeepLink";
import { graphNodeNavigationTarget, type GraphScope } from "./graphViewShared";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { NOTES_ROOT_PATH } from "./notesRoute";
import { TreeList } from "./TreeList";

const FilePanel = lazy(() =>
  import("./FilePanel").then((module) => ({
    default: module.FilePanel,
  })),
);

const GraphView = lazy(() =>
  import("./GraphView").then((module) => ({
    default: module.GraphView,
  })),
);

function moduleKey(path: string, depth: number) {
  const cleaned = path.replace(/^\/+/, "");
  const parts = cleaned.split("/");

  if (parts.length === 0) return "root";

  if (parts.length < depth) return parts.join("/");

  return parts.slice(0, depth).join("/");
}

function isNoteTarget(path: string, kind?: string): boolean {
  if (kind) return ["note", "section", "embedded"].includes(kind.toLowerCase());
  const barePath = path.split("#", 1)[0] || path;

  return /\.(?:md|html?)$/i.test(barePath);
}

function openNoteWorkspace(path: string) {
  const hashIndex = path.indexOf("#");
  const notePath = hashIndex >= 0 ? path.slice(0, hashIndex) : path;
  const noteHash = hashIndex >= 0 ? path.slice(hashIndex) : "";
  const params = new URLSearchParams();
  params.set("note", notePath);
  window.history.pushState(
    {},
    "",
    `${NOTES_ROOT_PATH}?${params.toString()}${encodeURLFragment(noteHash)}`,
  );
  window.dispatchEvent(new PopStateEvent("popstate"));
}

type ExplorerWorkspaceProps = {
  /** Vault name lifted from AppShell so this component avoids a redundant
   *  `/api/v1/status` fetch. Empty string = still loading; renders the
   *  "Vault" fallback in the rail header. */
  vaultName: string;
};

type ExplorerSelection =
  | { kind: "root" }
  | { kind: "file"; path: string }
  | { kind: "module"; path: string };

function explorerFileFromLocation(search: string): { path: string; line?: number } | null {
  const params = new URLSearchParams(search);
  const path = params.get("file")?.trim();

  if (!path) return null;
  const parsedLine = Number(params.get("line"));

  return {
    path,
    line: Number.isInteger(parsedLine) && parsedLine > 0 ? parsedLine : undefined,
  };
}

const EMPTY_GRAPH: GraphResponse = {
  nodes: [],
  edges: [],
  truncated: false,
};

function useDebouncedValue(value: string, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);

    return () => window.clearTimeout(timer);
  }, [delay, value]);

  return debounced;
}

export function ExplorerWorkspace({ vaultName }: ExplorerWorkspaceProps) {
  const browserLocation = useLocationSnapshot();
  const deepLink = explorerFileFromLocation(browserLocation.search);
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());

  const [selection, setSelection] = useState<ExplorerSelection>(() =>
    deepLink ? { kind: "file", path: deepLink.path } : { kind: "root" },
  );

  const [selectedLine, setSelectedLine] = useState<number | undefined>(deepLink?.line);
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebouncedValue(query.trim(), 150);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [submittedSearch, setSubmittedSearch] = useState<string | null>(null);
  const [searchOnlyMatches, setSearchOnlyMatches] = useState(false);
  const [graphZoomKey, setGraphZoomKey] = useState(0);

  const treeQuery = useQuery({
    queryKey: queryKeys.files.tree(undefined, 200),
    queryFn: ({ signal }) => getTree(undefined, 200, { signal }),
  });

  const tree = treeQuery.data?.entries ?? [];

  const expandedPathList = useMemo(() => Array.from(expandedPaths).sort(), [expandedPaths]);

  const expandedQueries = useQueries({
    queries: expandedPathList.map((path) => ({
      queryKey: queryKeys.files.tree(path, 200),
      queryFn: ({ signal }: { signal: AbortSignal }) => getTree(path, 200, { signal }),
    })),
  });

  const expanded = useMemo(() => {
    const result: Record<string, TreeEntry[]> = {};
    expandedPathList.forEach((path, index) => {
      result[path] = expandedQueries[index]?.data?.entries ?? [];
    });

    return result;
  }, [expandedPathList, expandedQueries]);

  const selectedFilePath = selection.kind === "file" ? selection.path : "";

  const fileQuery = useQuery({
    queryKey: queryKeys.files.detail(selectedFilePath),
    queryFn: ({ signal }) => getFileView(selectedFilePath, { signal }),
    enabled: selection.kind === "file",
  });

  const file: FileView | null = selection.kind === "file" ? (fileQuery.data ?? null) : null;

  const selectedModulePath = selection.kind === "module" ? selection.path : "";

  const moduleQuery = useQuery({
    queryKey: queryKeys.files.tree(selectedModulePath, 200),
    queryFn: ({ signal }) => getTree(selectedModulePath, 200, { signal }),
    enabled: selection.kind === "module",
  });

  const moduleFocus =
    selection.kind === "module"
      ? { path: selection.path, entries: moduleQuery.data?.entries ?? [] }
      : null;

  const graphQuery = useQuery({
    queryKey:
      selection.kind === "root"
        ? queryKeys.graph.global({})
        : selection.kind === "file"
          ? queryKeys.graph.local(selection.path, 200, {})
          : queryKeys.graph.expanded(selection.path, 600),
    queryFn: ({ signal }) => {
      if (selection.kind === "root") return getGlobalGraph({}, { signal });

      if (selection.kind === "file") {
        return getLocalGraph(selection.path, 200, {}, { signal });
      }

      return getExpandedModuleGraph(selection.path, 600, { signal });
    },
  });

  const graph = graphQuery.data ?? EMPTY_GRAPH;

  const graphScope: GraphScope =
    selection.kind === "root"
      ? { mode: "global" }
      : selection.kind === "file"
        ? { mode: "local", label: selection.path }
        : { mode: "module", label: selection.path };

  const suggestionsQuery = useQuery({
    queryKey: queryKeys.search.suggestions(debouncedQuery, 20),
    queryFn: ({ signal }) => getSuggestions(debouncedQuery, 20, { signal }),
    enabled: debouncedQuery.length > 0,
  });

  const suggestions = debouncedQuery === query.trim() ? (suggestionsQuery.data?.matches ?? []) : [];

  const searchQuery = useQuery({
    queryKey: queryKeys.search.paths(submittedSearch ?? "", 200),
    queryFn: ({ signal }) => searchPathMatches(submittedSearch ?? "", 200, { signal }),
    enabled: submittedSearch !== null,
  });

  const searchPaths = (searchQuery.data?.matches ?? []).flatMap((match) =>
    match.path ? [match.path] : [],
  );

  const searchModules = useMemo(() => {
    const modules = new Set<string>();

    for (const path of searchPaths) {
      const key = moduleKey(path, 2);

      if (key) modules.add(key);
    }

    return Array.from(modules);
  }, [searchPaths]);

  const searchActive = submittedSearch !== null;
  const searching = searchQuery.isFetching;

  useEffect(() => {
    const next = explorerFileFromLocation(browserLocation.search);

    if (!next) {
      if (selection.kind === "file") setSelection({ kind: "root" });
      setSelectedLine(undefined);

      return;
    }

    if (selection.kind !== "file" || selection.path !== next.path) {
      setSelection({ kind: "file", path: next.path });
      setGraphZoomKey((key) => key + 1);
    }

    setSelectedLine(next.line);
  }, [browserLocation.search, selection]);

  const clearSearch = useCallback(() => {
    setSubmittedSearch(null);
    setSearchOnlyMatches(false);
  }, []);

  const select = useCallback((next: ExplorerSelection, line?: number) => {
    setSelection(next);
    setSelectedLine(line);
    setGraphZoomKey((key) => key + 1);

    if (next.kind === "file") {
      const params = new URLSearchParams({ file: next.path });

      if (line && line > 0) params.set("line", String(line));
      window.history.pushState({}, "", `/explorer?${params.toString()}`);
      notifyLocationChange();
    } else if (new URLSearchParams(window.location.search).has("file")) {
      window.history.replaceState({}, "", "/explorer");
      notifyLocationChange();
    }
  }, []);

  const onSelectFile = useCallback(
    (path: string, kind?: string) => {
      if (isNoteTarget(path, kind)) {
        openNoteWorkspace(path);

        return;
      }

      select({ kind: "file", path });
    },
    [select],
  );

  const focusModule = useCallback(
    (path: string) => {
      setExpandedPaths((previous) => new Set(previous).add(path));
      select({ kind: "module", path });
    },
    [select],
  );

  const onToggleDir = (entry: TreeEntry) => {
    if (expandedPaths.has(entry.path)) {
      setExpandedPaths((previous) => {
        const next = new Set(previous);
        next.delete(entry.path);

        return next;
      });

      return;
    }

    focusModule(entry.path);
  };

  const onNodeClick = useCallback(
    (node: GraphNode) => {
      if (node.kind === "module" && node.path) {
        focusModule(node.path);
      } else if (node.kind === "embedded" || node.kind === "note" || node.kind === "code") {
        const target = graphNodeNavigationTarget(node);

        if (target) onSelectFile(target, node.kind);
      }
    },
    [focusModule, onSelectFile],
  );

  const closePanel = useCallback(() => {
    select({ kind: "root" });
  }, [select]);

  const onVaultRootClick = useCallback(() => {
    clearSearch();
    select({ kind: "root" });
  }, [clearSearch, select]);

  const resetGraphScope = useCallback(() => {
    select({ kind: "root" });
  }, [select]);

  const runSearch = useCallback(() => {
    const q = query.trim();

    if (!q) {
      clearSearch();

      return;
    }

    setSubmittedSearch(q);
  }, [query, clearSearch]);

  const hasPanel = selection.kind !== "root";
  const searchPathSet = useMemo(() => new Set(searchPaths), [searchPaths]);
  const searchModuleSet = useMemo(() => new Set(searchModules), [searchModules]);

  const graphScopeLabel =
    graphScope.mode === "global" ? "Global scope" : graphScope.label || "Local scope";

  return (
    <div className="workspace workspace--explorer">
      <header className="explorer-bar">
        <div className="explorer-bar__title">
          <h1>Explorer</h1>
          <p>Navigate the vault as files, folders, and a live graph.</p>
        </div>
        <div
          className="explorer-bar__search"
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) setShowSuggestions(false);
          }}
        >
          <input
            type="search"
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
            }}
            onFocus={() => {
              setShowSuggestions(true);
            }}
            placeholder="Search notes + code"
          />
          <button
            type="button"
            className="explorer-bar__search-btn"
            onClick={() => {
              if (query.trim() !== "" || searchActive) {
                setQuery("");
                setShowSuggestions(false);
                clearSearch();
              } else {
                runSearch();
              }
            }}
            disabled={searching}
            title={query.trim() !== "" || searchActive ? "Clear search" : "Search"}
          >
            {query.trim() !== "" || searchActive ? "Clear" : searching ? "Searching…" : "Search"}
          </button>
          <label
            className={`explorer-bar__filter ${searchActive ? "" : "explorer-bar__filter--disabled"}`}
          >
            <input
              type="checkbox"
              checked={searchOnlyMatches}
              disabled={!searchActive}
              onChange={(e) => setSearchOnlyMatches(e.target.checked)}
            />
            Only matches
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
                    onSelectFile(suggestion.path, suggestion.kind);
                    setShowSuggestions(false);
                  }}
                >
                  {suggestion.path} ({suggestion.kind})
                </button>
              ))}
            </div>
          )}
          {searchQuery.isError ? (
            <div className="explorer-bar__search-error" role="alert">
              Search unavailable.{" "}
              <button type="button" onClick={() => searchQuery.refetch()}>
                Retry
              </button>
            </div>
          ) : null}
        </div>
      </header>
      <div className={`explorer-grid ${hasPanel ? "explorer-grid--with-pane" : ""}`}>
        <aside className="explorer-rail">
          <div className="explorer-rail__header">
            <span>Vault</span>
            <span>{tree.length}</span>
          </div>
          <div className="explorer-rail__body">
            {treeQuery.isPending && !treeQuery.data && (
              <div className="explorer-rail__status" role="status">
                Loading folders…
              </div>
            )}
            {treeQuery.isError && !treeQuery.data && (
              <div className="explorer-rail__status" role="alert">
                Could not load folders.{" "}
                <button type="button" onClick={() => treeQuery.refetch()}>
                  Retry
                </button>
              </div>
            )}
            <button
              type="button"
              className={`explorer-rail__root ${selection.kind === "root" ? "is-selected" : ""}`}
              onClick={onVaultRootClick}
              title="View full graph"
            >
              <span className="explorer-rail__root-icon">📂</span>
              <span>{vaultName || "Vault"}</span>
            </button>
            <TreeList
              entries={tree}
              expanded={expanded}
              selectedPath={moduleFocus?.path || file?.path || null}
              onSelect={(entry) => onSelectFile(entry.path, entry.kind)}
              onToggle={onToggleDir}
            />
          </div>
        </aside>

        <section className="explorer-focus">
          <div className="explorer-focus__header">
            <h2>Graph</h2>
            <span className="explorer-focus__scope">{graphScopeLabel}</span>
          </div>
          <div className="explorer-focus__body">
            {graphQuery.isPending && !graphQuery.data ? (
              <div className="graph-notice" role="status">
                <div className="graph-notice-title">Loading graph</div>
                <div className="graph-notice-body">Preparing graph workspace…</div>
              </div>
            ) : graphQuery.isError && !graphQuery.data ? (
              <div className="graph-notice" role="alert">
                <div className="graph-notice-title">Graph unavailable</div>
                <button type="button" onClick={() => graphQuery.refetch()}>
                  Retry
                </button>
              </div>
            ) : (
              <Suspense
                fallback={
                  <div className="graph-notice">
                    <div className="graph-notice-title">Loading graph</div>
                    <div className="graph-notice-body">Preparing graph workspace…</div>
                  </div>
                }
              >
                <GraphView
                  nodes={graph.nodes}
                  edges={graph.edges}
                  truncated={graph.truncated}
                  needsIndex={graph.needsIndex}
                  scope={graphScope}
                  onResetScope={resetGraphScope}
                  onNodeClick={onNodeClick}
                  searchActive={searchActive}
                  searchPaths={searchPathSet}
                  searchModules={searchModuleSet}
                  searchOnlyMatches={searchOnlyMatches}
                  zoomKey={graphZoomKey}
                />
              </Suspense>
            )}
          </div>
        </section>

        {hasPanel &&
          ((selection.kind === "file" && fileQuery.isPending && !fileQuery.data) ||
          (selection.kind === "module" && moduleQuery.isPending && !moduleQuery.data) ? (
            <section className="explorer-pane" role="status">
              <div className="explorer-pane__header">
                <div className="explorer-pane__head-main">
                  <span className="explorer-pane__label">Loading</span>
                  <h2>Preparing pane…</h2>
                </div>
              </div>
            </section>
          ) : (selection.kind === "file" && fileQuery.isError && !fileQuery.data) ||
            (selection.kind === "module" && moduleQuery.isError && !moduleQuery.data) ? (
            <section className="explorer-pane" role="alert">
              <div className="explorer-pane__header">
                <div className="explorer-pane__head-main">
                  <span className="explorer-pane__label">Unavailable</span>
                  <h2>Could not load selection</h2>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    if (selection.kind === "file") void fileQuery.refetch();
                    else void moduleQuery.refetch();
                  }}
                >
                  Retry
                </button>
              </div>
            </section>
          ) : (
            <Suspense
              fallback={
                <section className="explorer-pane">
                  <div className="explorer-pane__header">
                    <div className="explorer-pane__head-main">
                      <span className="explorer-pane__label">Loading</span>
                      <h2>Preparing pane…</h2>
                    </div>
                  </div>
                </section>
              }
            >
              <FilePanel
                file={file}
                line={selectedLine}
                moduleFocus={moduleFocus}
                onClose={closePanel}
                onSelectFile={onSelectFile}
                onFocusModule={focusModule}
              />
            </Suspense>
          ))}
      </div>
    </div>
  );
}
