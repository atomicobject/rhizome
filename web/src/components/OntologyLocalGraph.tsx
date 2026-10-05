import { publicTypeName } from "../lib/typeNames";
import { useCallback, useMemo } from "react";
import type {
  GraphEdge,
  GraphNode,
  NodeWorkspace,
  StructuralNode,
  WorkspaceContentNode,
  WorkspaceNode,
} from "../api/types";
import type { GraphViewNode, HoverInfo } from "./graphViewShared";
import { buildTypeColors, kindLabel, truncatePath } from "./graphViewShared";
import type { GraphNodeClickOptions } from "./useSigmaGraph";
import { useSigmaGraph } from "./useSigmaGraph";

type Props = {
  workspace: NodeWorkspace;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
  onOpenNode?: (node: StructuralNode) => void;
};

type GraphWorkspaceNode = WorkspaceContentNode & {
  kind: "note" | "embedded";
};

type EmbeddedWorkspaceNode = WorkspaceContentNode & {
  kind: "embedded";
};

function isGraphNode(node: WorkspaceNode | undefined): node is GraphWorkspaceNode {
  return Boolean(node && (node.kind === "note" || node.kind === "embedded"));
}

function isEmbeddedNode(node: GraphWorkspaceNode): node is EmbeddedWorkspaceNode {
  return node.kind === "embedded";
}

function nodeLabel(node: GraphWorkspaceNode) {
  return node.data.title || node.notePath;
}

function nodePath(node: GraphWorkspaceNode) {
  if (node.kind === "embedded" && node.ref.nodeId) {
    return node.ref.nodeId;
  }

  return node.notePath;
}

function embeddedOpenTarget(node: EmbeddedWorkspaceNode) {
  const fragment = node.ref.fragment || node.data.fragment;

  if (fragment) return `${node.notePath}#${fragment}`;

  if (node.ref.nodeId?.includes("#")) return node.ref.nodeId;

  return node.notePath;
}

function toStructuralNode(node: EmbeddedWorkspaceNode): StructuralNode {
  return {
    nodeId: node.ref.nodeId || node.notePath,
    fragment: node.data.fragment || node.ref.fragment,
    title: node.data.title || "",
    typeName: node.data.binding?.typeName || node.data.resolvedType,
    locator: node.data.locator || "EMBEDDED",
    notePath: node.notePath,
    level: node.data.level,
  };
}

type LocalGraphModel = {
  nodes: GraphViewNode[];
  edges: GraphEdge[];
  emptyReason: string;
};

function buildLocalGraph(workspace: NodeWorkspace): LocalGraphModel {
  const local = workspace.views?.localGraph;
  const nodeMap = new Map((workspace.nodes || []).map((node) => [node.id, node]));
  const localNodeIDs = new Set(local?.nodeIds || []);
  const centerNodeIDs = new Set(local?.centerNodeIds || []);

  const nodes: GraphViewNode[] = Array.from(localNodeIDs)
    .flatMap((id) => {
      const node = nodeMap.get(id);

      if (!isGraphNode(node)) return [];

      return [
        {
          id: node.id,
          label: nodeLabel(node),
          path: nodePath(node),
          kind: node.kind,
          resolvedType: publicTypeName(node.data.binding?.typeName || node.data.resolvedType),
          center: centerNodeIDs.has(node.id),
        },
      ];
    })
    .sort((a, b) => a.label.localeCompare(b.label));

  const allowedEdgeKinds = new Set(["links_to", "embeds"]);

  const edges: GraphEdge[] = (workspace.edges || []).flatMap((edge) => {
    if (
      !allowedEdgeKinds.has(edge.kind) ||
      !localNodeIDs.has(edge.fromId) ||
      !localNodeIDs.has(edge.toId)
    ) {
      return [];
    }

    return [
      {
        source: edge.fromId,
        target: edge.toId,
        kind: edge.kind,
      },
    ];
  });

  return {
    nodes,
    edges,
    emptyReason: local?.emptyReason || "No local links or embedded nodes yet.",
  };
}

function HoverCard({ hover }: { hover: HoverInfo }) {
  return (
    <div className="graph-hover">
      <div className="graph-hover-label">{hover.label}</div>
      <div className="graph-hover-meta">
        <span className="graph-hover-kind">{kindLabel(hover.kind)}</span>
        {hover.connectionCount > 0 && (
          <span className="graph-hover-connections">
            {hover.connectionCount} connection
            {hover.connectionCount !== 1 ? "s" : ""}
          </span>
        )}
      </div>
      {hover.path && <div className="graph-hover-path">{truncatePath(hover.path)}</div>}
    </div>
  );
}

export function OntologyLocalGraph({ workspace, onOpen, onOpenNode }: Props) {
  const { nodes, edges, emptyReason } = useMemo(() => buildLocalGraph(workspace), [workspace]);

  const typeColors = useMemo(
    () => buildTypeColors(nodes.flatMap((node) => (node.resolvedType ? [node.resolvedType] : []))),
    [nodes],
  );

  const nodeMap = useMemo(
    () => new Map((workspace.nodes || []).map((node) => [node.id, node])),
    [workspace.nodes],
  );

  const handleNodeClick = useCallback(
    (clicked: GraphNode, options: GraphNodeClickOptions) => {
      const node = nodeMap.get(clicked.id);

      if (!isGraphNode(node)) return;

      if (node.kind === "note") {
        onOpen(node.notePath, options.beside ? "beside" : "stack");

        return;
      }

      if (isEmbeddedNode(node)) {
        if (options.beside) {
          onOpen(embeddedOpenTarget(node), "beside");

          return;
        }

        onOpenNode?.(toStructuralNode(node));
      }
    },
    [nodeMap, onOpen, onOpenNode],
  );

  const {
    containerRef,
    graphMatches,
    graphQuery,
    graphQueryValue,
    handleFitToView,
    handleZoomIn,
    handleZoomOut,
    hover,
    setGraphQuery,
  } = useSigmaGraph({
    nodes,
    edges,
    scope: { mode: "local", label: workspace.node.notePath },
    onNodeClick: handleNodeClick,
    searchActive: false,
    searchPaths: new Set<string>(),
    searchModules: new Set<string>(),
    searchOnlyMatches: false,
    typeColors,
    zoomKey: 0,
  });

  return (
    <section className="ontology-local-graph">
      <div className="ontology-local-graph__header">
        <div>
          <h3>Local graph</h3>
          <p>Inbound links, outbound links, and embedded-node neighborhood.</p>
        </div>
      </div>
      <div className="ontology-local-graph__toolbar">
        <input
          className="graph-search"
          value={graphQuery}
          onChange={(event) => setGraphQuery(event.target.value)}
          placeholder="Find nearby nodes"
        />
        {graphQueryValue ? (
          <span className="ontology-local-graph__matches">
            {graphMatches.size} match{graphMatches.size !== 1 ? "es" : ""}
          </span>
        ) : null}
      </div>
      <div className="graph-container ontology-local-graph__canvas">
        <div ref={containerRef} className="graph-surface" />
        <div className="graph-zoom">
          <button
            type="button"
            className="zoom-btn"
            onClick={handleZoomIn}
            title="Zoom in"
            aria-label="Zoom in"
          >
            +
          </button>
          <button
            type="button"
            className="zoom-btn"
            onClick={handleZoomOut}
            title="Zoom out"
            aria-label="Zoom out"
          >
            -
          </button>
          <button
            type="button"
            className="zoom-btn zoom-btn--fit"
            onClick={handleFitToView}
            title="Fit to view"
            aria-label="Fit to view"
          >
            ⊙
          </button>
        </div>
        {nodes.length === 0 && (
          <div className="graph-notice">
            <div className="graph-notice-title">No local graph yet</div>
            <div className="graph-notice-body">{emptyReason}</div>
          </div>
        )}
        {nodes.length > 0 && edges.length === 0 && (
          <div className="ontology-local-graph__empty-hint">{emptyReason}</div>
        )}
        {hover ? <HoverCard hover={hover} /> : null}
      </div>
    </section>
  );
}
