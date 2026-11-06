import { lazy, Suspense, useMemo } from "react";
import type {
  NodeWorkspace,
  NodeWorkspaceGraph,
  StructuralNode,
  ValidationHealth,
} from "../../api/types";

type OpenTarget = "current" | "stack" | "beside";

function graphHasContent(graph: NodeWorkspaceGraph | null | undefined): boolean {
  return Boolean(
    graph?.views?.localGraph &&
    ((graph.views.localGraph.nodeIds?.length || 0) > 0 ||
      (graph.nodes?.length || 0) > 0 ||
      (graph.edges?.length || 0) > 0),
  );
}

function graphWorkspace(
  workspace: NodeWorkspace,
  graph: NodeWorkspaceGraph | null | undefined,
): NodeWorkspace | null {
  if (!graphHasContent(graph)) return null;

  return {
    ...workspace,
    focusedNodeId: graph?.focusedNodeId,
    nodes: graph?.nodes || [],
    edges: graph?.edges || [],
    views: graph?.views,
  };
}

// Sigma, graphology, and ForceAtlas2 load with the Graph tab, not the Notes entry.
const OntologyLocalGraph = lazy(() =>
  import("../OntologyLocalGraph").then((module) => ({ default: module.OntologyLocalGraph })),
);

export function GraphPanel({
  workspace,
  graph,
  graphLoading,
  graphError,
  onRetry,
  onOpen,
  onOpenNode,
}: {
  workspace: NodeWorkspace;
  graph: NodeWorkspaceGraph | null | undefined;
  graphLoading: boolean;
  graphError: boolean;
  onRetry: () => void;
  onOpen: (path: string, target?: OpenTarget) => void;
  onOpenNode: (node: StructuralNode) => void;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
}) {
  const localWorkspace = useMemo(() => graphWorkspace(workspace, graph), [graph, workspace]);

  if (localWorkspace) {
    return (
      <div className="notes-right-rail__graph">
        <Suspense
          fallback={
            <p className="notes-right-rail__graph-status" role="status">
              Loading local graph…
            </p>
          }
        >
          <OntologyLocalGraph workspace={localWorkspace} onOpen={onOpen} onOpenNode={onOpenNode} />
        </Suspense>
      </div>
    );
  }

  return (
    <section className="notes-right-rail__graph notes-right-rail__graph-status" aria-label="Graph">
      {graphLoading ? (
        "Loading local graph…"
      ) : graphError ? (
        <>
          <span>Unable to load local graph.</span>
          <button type="button" onClick={onRetry}>
            Retry
          </button>
        </>
      ) : (
        "No local graph yet."
      )}
    </section>
  );
}
