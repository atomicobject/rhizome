import { lazy, Suspense, useMemo } from "react";

import type {
  GraphResponse,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ValidateEnvelope,
  ValidationScopeSummary,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { graphNodeNavigationTarget, type GraphScope } from "./graphViewShared";
import { noteIssueCounts as countNoteIssues } from "./useValidationScopeSummaries";
import { NotesTypeHome } from "./NotesTypeHome";
import { isPseudoType } from "./notesRoute";
import type { GraphDisplay, GraphNodeClickOptions } from "./useSigmaGraph";

const GraphView = lazy(() =>
  import("./GraphView").then((module) => ({
    default: module.GraphView,
  })),
);

const GLOBAL_SCOPE: GraphScope = { mode: "global" };

const EMPTY_SEARCH_PATHS = new Set<string>();

const EMPTY_SEARCH_MODULES = new Set<string>();

const NOOP = () => undefined;

type CommonProps = {
  summary: OntologySummaryResponse | null;
  typeDetail: OntologyTypeResponse | null;
  selectedType: string | null;
  graph: GraphResponse | null;
  graphLoading?: boolean;
  graphError?: string | null;
  onRetryGraph: () => void;
  typeColors: Map<string, string>;
  loading?: boolean;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  validationSummaries?: ReadonlyMap<string, ValidationScopeSummary>;
  /** The graph's remembered display toggles. */
  graphDisplay?: GraphDisplay;
  onGraphDisplayChange?: (patch: Partial<GraphDisplay>) => void;
};

export type HomeGraphLayoutProps = CommonProps & {
  unavailable?: string | null;
  onRetry: () => void;
  onOpenIssues?: () => void;
  validation?: ValidateEnvelope | null;
};

function buildHomeHighlightTypes(
  selectedType: string | null,
  summary: OntologySummaryResponse | null,
): ReadonlySet<string> | undefined {
  if (!selectedType || isPseudoType(selectedType)) return undefined;
  const selectedInterface = summary?.interfaces?.find((item) => item.name === selectedType);

  return new Set([selectedType, ...(selectedInterface?.implementors ?? [])]);
}

function buildHomeGraphProps(
  graph: GraphResponse,
  typeColors: Map<string, string>,
  selectedHighlights: ReadonlySet<string> | undefined,
  onOpenNote: (path: string, mode?: OpenMode) => void,
) {
  return {
    nodes: graph.nodes,
    edges: graph.edges,
    truncated: graph.truncated,
    scope: GLOBAL_SCOPE,
    onResetScope: NOOP,
    onNodeClick: (
      node: Parameters<typeof graphNodeNavigationTarget>[0],
      options: GraphNodeClickOptions,
    ) => {
      const target = graphNodeNavigationTarget(node);

      if (target) onOpenNote(target, options.beside ? "beside" : "activate");
    },
    searchActive: false,
    searchPaths: EMPTY_SEARCH_PATHS,
    searchModules: EMPTY_SEARCH_MODULES,
    searchOnlyMatches: false,
    typeColors,
    zoomKey: 0,
    highlightTypes: selectedHighlights,
    showTypeLegend: true,
  };
}

export function buildHomeProblemCounts(
  graph: GraphResponse,
  summaries: ReadonlyMap<string, ValidationScopeSummary> | undefined,
) {
  const counts = new Map<string, number>();

  for (const node of graph.nodes) {
    const count = noteIssueCount(summaries, node.notePath || node.path);

    if (count !== undefined) counts.set(node.id, count);
  }

  return counts;
}

export function HomeGraphLayout(props: HomeGraphLayoutProps) {
  const { graph, loading, onOpenNote, selectedType, summary, typeColors, typeDetail } = props;

  const selectedHighlights = useMemo(
    () => buildHomeHighlightTypes(selectedType, summary),
    [selectedType, summary],
  );

  const typeLabels = useMemo(
    () =>
      new Map(
        [...(summary?.types ?? []), ...(summary?.interfaces ?? [])].map((item) => [
          item.name,
          item.label || item.name,
        ]),
      ),
    [summary],
  );

  const graphProps = useMemo(() => {
    if (!graph) return null;
    const problemCounts = buildHomeProblemCounts(graph, props.validationSummaries);

    return {
      ...buildHomeGraphProps(graph, typeColors, selectedHighlights, onOpenNote),
      problemCounts,
      typeLabels,
      display: props.graphDisplay,
      onDisplayChange: props.onGraphDisplayChange,
    };
  }, [
    graph,
    onOpenNote,
    props.graphDisplay,
    props.onGraphDisplayChange,
    props.validationSummaries,
    selectedHighlights,
    typeColors,
    typeLabels,
  ]);

  const openIssues = props.onOpenIssues ?? NOOP;

  const noteIssueCounts = countNoteIssues(props.validationSummaries);

  const selectedIssueCount = selectedType
    ? [...(props.validationSummaries?.values() ?? [])].find(
        (summary) =>
          (summary.scope.kind === "type" || summary.scope.kind === "interface") &&
          summary.scope.key === selectedType,
      )?.issueCount
    : undefined;

  return (
    <div className="home-graph-layout home-graph-layout--type">
      <NotesTypeHome
        typeDetail={typeDetail}
        onOpenNote={onOpenNote}
        onOpenIssues={openIssues}
        validation={props.validation}
        issueCount={selectedIssueCount}
        noteIssueCounts={noteIssueCounts}
        loading={loading}
        unavailable={props.unavailable}
        onRetry={props.onRetry}
      />
      <section className="ontology-home__graph home-graph-layout__graph">
        <header className="ontology-home__card-head">
          <h3>Graph</h3>
          {graph && (
            <span className="ontology-home__hint">
              {graph.nodes.length} nodes · {graph.edges.length} edges
            </span>
          )}
        </header>
        {props.graphError && (
          <div className="ontology-home__unavailable" role="alert">
            <span>Unable to load graph: {props.graphError}</span>
            <button type="button" onClick={props.onRetryGraph}>
              Retry graph
            </button>
          </div>
        )}
        {props.graphLoading && (
          <div className="ontology-home__hero-loading" role="status">
            {graph ? "Refreshing graph…" : "Loading graph…"}
          </div>
        )}
        <div className="ontology-home__graph-canvas ontology-home__graph-canvas--tall">
          {graphProps && (
            <Suspense fallback={<div className="ontology-empty">Loading graph…</div>}>
              <GraphView {...graphProps} />
            </Suspense>
          )}
        </div>
      </section>
    </div>
  );
}

function noteIssueCount(
  summaries: ReadonlyMap<string, ValidationScopeSummary> | undefined,
  path: string | undefined,
) {
  if (!path) return undefined;

  return summaries?.get(`note:${path}`)?.issueCount;
}
