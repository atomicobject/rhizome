import { useQueries, useQuery } from "@tanstack/react-query";
import { FolderOpen } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import {
  getFileView,
  getGlobalGraph,
  getLocalGraph,
  getSuggestions,
  getTree,
  searchPaths as searchPathMatches,
} from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { FileView, GraphNode, GraphResponse, TreeEntry } from "../api/types";
import { vaultLabel } from "../lib/labels";
import { useTypeAccentKey } from "../lib/typeAccent";
import { publicTypeName } from "../lib/typeNames";
import { useDebouncedValue, useFolderExplorer } from "./useFolderExplorer";
import { ExplorerHeader } from "./ExplorerHeader";
import { buildTypeColors, graphNodeNavigationTarget, type GraphScope } from "./graphViewShared";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { TreeList } from "./TreeList";
import { WorkbenchSidebar } from "./WorkbenchSidebar";
import {
  moduleKey,
  isNoteTarget,
  openNoteWorkspace,
  explorerFileFromLocation,
} from "./explorerNavigation";

export { folderCoverage } from "./explorerNavigation";

const FilePanel = lazy(() => import("./FilePanel").then((m) => ({ default: m.FilePanel })));

const GraphView = lazy(() => import("./GraphView").then((m) => ({ default: m.GraphView })));

type ExplorerWorkspaceProps = {
  vaultName: string;
  sidebarTitle?: string | null;
  sidebarHint?: string | null;
};

type ExplorerSelection =
  | { kind: "root" }
  | { kind: "file"; path: string }
  | { kind: "module"; path: string };

const EMPTY_GRAPH: GraphResponse = {
  nodes: [],
  edges: [],
  truncated: false,
};

export function ExplorerWorkspace({
  vaultName,
  sidebarTitle,
  sidebarHint,
}: ExplorerWorkspaceProps) {
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

  const graphQuery = useQuery({
    queryKey:
      selection.kind === "file"
        ? queryKeys.graph.local(selection.path, 200, {})
        : queryKeys.graph.global({ limit: 200000 }),
    queryFn: ({ signal }) =>
      selection.kind === "file"
        ? getLocalGraph(selection.path, 200, {}, { signal })
        : getGlobalGraph({ limit: 200000 }, { signal }),
  });

  const graph = graphQuery.data ?? EMPTY_GRAPH;
  const atRoot = selection.kind !== "file";
  const [displaySlot, setDisplaySlot] = useState<HTMLDivElement | null>(null);

  const graphScope: GraphScope =
    selection.kind === "file" ? { mode: "local", label: selection.path } : { mode: "global" };

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

    if (next.kind !== "module") setGraphZoomKey((key) => key + 1);

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

  const accentKey = useTypeAccentKey();

  const typeColors = useMemo(
    () =>
      buildTypeColors([
        ...new Set(graph.nodes.map((node) => publicTypeName(node.resolvedType)).filter(Boolean)),
      ]),
    // The summary decides each type's palette slot.
    [graph.nodes, accentKey],
  );

  const byFolder = atRoot && !searchActive;

  const folderExplorer = useFolderExplorer(graph, typeColors);
  useEffect(() => {
    if (atRoot) setExpandedPaths(new Set([...folderExplorer.initial].filter(Boolean)));
  }, [folderExplorer.initial, atRoot]);
  const folders = byFolder ? folderExplorer.drawn : null;
  const drawn = folders ?? graph;

  const focusModule = useCallback(
    (path: string) => {
      setExpandedPaths((previous) => {
        const next = new Set(previous);
        const parts = path.split("/");

        parts.forEach((_, index) => next.add(parts.slice(0, index + 1).join("/")));

        return next;
      });
      folderExplorer.expand(path);
      select({ kind: "module", path });
    },
    [select, folderExplorer],
  );

  const onToggleDir = (entry: TreeEntry) => {
    if (expandedPaths.has(entry.path)) {
      setExpandedPaths((previous) => {
        const next = new Set(previous);

        for (const path of next)
          if (path === entry.path || path.startsWith(`${entry.path}/`)) next.delete(path);

        return next;
      });

      folderExplorer.collapse(entry.path);

      return;
    }

    focusModule(entry.path);
  };

  const onNodeClick = useCallback(
    (node: GraphNode) => {
      if (node.kind === "module") {
        const path = node.path ?? "";

        if (node.id.startsWith("more:")) folderExplorer.reveal(path);
        else if (path && folderExplorer.open.has(path)) folderExplorer.collapse(path);
        else if (path) focusModule(path);
      } else if (node.kind === "embedded" || node.kind === "note" || node.kind === "code") {
        const target = graphNodeNavigationTarget(node);

        if (target) onSelectFile(target, node.kind);
      }
    },
    [focusModule, onSelectFile, folderExplorer],
  );

  const closePanel = useCallback(() => {
    select({ kind: "root" });
  }, [select]);

  const onVaultRootClick = useCallback(() => {
    clearSearch();
    folderExplorer.reset();
    setExpandedPaths(new Set([...folderExplorer.initial].filter(Boolean)));
    select({ kind: "root" });
  }, [clearSearch, select, folderExplorer]);

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

  const hasPanel = selection.kind === "file";
  const searchPathSet = useMemo(() => new Set(searchPaths), [searchPaths]);
  const searchModuleSet = useMemo(() => new Set(searchModules), [searchModules]);

  const graphScopeLabel =
    graphScope.mode === "global"
      ? "By folder; click a folder to expand its contents"
      : graphScope.mode === "module"
        ? `Folder ${graphScope.label}`
        : `File ${graphScope.label || "graph"}`;

  return (
    <div className="workspace workbench">
      <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
        <aside className="explorer-rail" aria-label="Vault files">
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
              <FolderOpen
                className="explorer-rail__root-icon"
                size={16}
                strokeWidth={1.5}
                aria-hidden="true"
              />
              <span className="tree-name">{vaultName ? vaultLabel(vaultName) : "Vault"}</span>
            </button>
            <TreeList
              entries={tree}
              expanded={expanded}
              selectedPath={selection.kind === "root" ? null : selection.path}
              onSelect={(entry) => onSelectFile(entry.path, entry.kind)}
              onToggle={onToggleDir}
            />
          </div>
        </aside>
      </WorkbenchSidebar>
      <div className="workbench__panel workspace--explorer">
        <ExplorerHeader
          graph={graph}
          graphScopeLabel={graphScopeLabel}
          displaySlot={setDisplaySlot}
          searchActive={searchActive}
          searching={searching}
          searchError={searchQuery.isError}
          matchCount={searchPathSet.size}
          searchOnlyMatches={searchOnlyMatches}
          setSearchOnlyMatches={setSearchOnlyMatches}
          query={query}
          setQuery={setQuery}
          clearSearch={clearSearch}
          runSearch={runSearch}
          showSuggestions={showSuggestions}
          setShowSuggestions={setShowSuggestions}
          suggestions={suggestions}
          onSelectFile={onSelectFile}
          retrySearch={() => {
            void searchQuery.refetch();
          }}
        />
        <div className={`explorer-grid ${hasPanel ? "explorer-grid--with-pane" : ""}`}>
          <section className="explorer-focus" aria-label="Graph">
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
                    nodes={drawn.nodes}
                    edges={drawn.edges}
                    truncated={drawn.truncated}
                    legendNodes={folders ? graph.nodes : undefined}
                    folderCaption={undefined}
                    sizeCaption={
                      folders ? "Size counts the notes and code files a folder holds" : undefined
                    }
                    {...(folders ? { layoutExtent: folders.layoutExtent } : {})}
                    displaySlot={displaySlot}
                    needsIndex={graph.needsIndex}
                    scope={graphScope}
                    onResetScope={resetGraphScope}
                    onNodeClick={onNodeClick}
                    searchActive={searchActive}
                    searchPaths={searchPathSet}
                    searchModules={searchModuleSet}
                    searchOnlyMatches={searchOnlyMatches}
                    typeColors={typeColors}
                    showTypeLegend
                    showFind={false}
                    zoomKey={graphZoomKey}
                  />
                </Suspense>
              )}
            </div>
          </section>

          {hasPanel &&
            (fileQuery.isPending && !fileQuery.data ? (
              <section className="explorer-pane" role="status">
                <div className="explorer-pane__header">
                  <div className="explorer-pane__head-main">
                    <span className="explorer-pane__label">Loading</span>
                    <h2>Preparing pane…</h2>
                  </div>
                </div>
              </section>
            ) : fileQuery.isError && !fileQuery.data ? (
              <section className="explorer-pane" role="alert">
                <div className="explorer-pane__header">
                  <div className="explorer-pane__head-main">
                    <span className="explorer-pane__label">Unavailable</span>
                    <h2>Could not load selection</h2>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      void fileQuery.refetch();
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
                  onClose={closePanel}
                  onSelectFile={onSelectFile}
                />
              </Suspense>
            ))}
        </div>
      </div>
    </div>
  );
}
