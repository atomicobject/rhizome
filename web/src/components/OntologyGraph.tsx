import {
  Background,
  Controls,
  type Edge,
  MarkerType,
  useReactFlow,
  type Node,
  ReactFlow,
} from "@xyflow/react";
import { startTransition, useEffect, useMemo, useState } from "react";

import "@xyflow/react/dist/style.css";

import type { OntologyAtlasResponse } from "../api/types";
import { buildOntologyGraph, layoutOntologyGraph } from "../lib/ontologyGraph";
import { typeAccentColor } from "../lib/typeAccent";
import { OntologyRoutedEdge } from "./OntologyRoutedEdge";
import { OntologyTypeNode } from "./OntologyTypeNode";

const nodeTypes = { ontologyType: OntologyTypeNode };

const edgeTypes = { ontologyRouted: OntologyRoutedEdge };

type Props = { atlas: OntologyAtlasResponse };

type AtlasEdgeData = {
  routedPoints?: { x: number; y: number }[];
  sourceOffset?: number;
  kind: EdgeKind;
  accent: string;
};

type AtlasEdge = Edge<AtlasEdgeData>;

type GraphState = { nodes: Node[]; edges: AtlasEdge[] };

/** Fit padding: cards keep 24px from every edge and clear the finder in the top left. */
const FIT_PADDING = { top: "64px", right: "24px", bottom: "24px", left: "24px" } as const;

const EMPTY_GRAPH: GraphState = { nodes: [], edges: [] };

type EdgeKind = "relation" | "implements" | "neighbor";

/** Each connector kind has its own line pattern, so the legend can tell them apart without color. */
const EDGE_DASH: Record<EdgeKind, string | undefined> = {
  relation: undefined,
  implements: "6 4",
  neighbor: "1.5 4",
};

const NEUTRAL_EDGE = "var(--type-8)";

/** A traced connector's color: the source type's, or ink for an interface's implements line. */
function edgeAccent(edge: AtlasEdge) {
  return edge.data?.kind === "implements" ? "var(--text)" : (edge.data?.accent ?? NEUTRAL_EDGE);
}

function FocusViewport({ ids, selected }: { ids: string[]; selected: string | null }) {
  const { fitView, getNode, setCenter, viewportInitialized } = useReactFlow();
  useEffect(() => {
    if (!viewportInitialized || ids.length === 0) return;
    const node = selected ? getNode(selected) : undefined;

    if (node) {
      void setCenter(
        node.position.x + (node.width ?? 280) / 2,
        node.position.y + (node.height ?? 104) / 2,
        // At zoom 1 a card's fields render at 12px, the least the card shows them at.
        { zoom: 1 },
      );
    } else {
      void fitView({
        nodes: ids.map((id) => ({ id })),
        padding: FIT_PADDING,
        maxZoom: 1,
        minZoom: 0.15,
      });
    }
  }, [viewportInitialized, ids, selected, fitView, getNode, setCenter]);

  return null;
}

export function OntologyGraph({ atlas }: Props) {
  const [selection, setSelected] = useState<string | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const [layout, setLayout] = useState<(GraphState & { atlas: OntologyAtlasResponse }) | null>(
    null,
  );

  const graph = layout?.atlas === atlas ? layout : EMPTY_GRAPH;
  const selected = graph.nodes.some((node) => node.id === selection) ? selection : null;

  useEffect(() => {
    let cancelled = false;

    function loadGraph() {
      if (cancelled) return;
      setError(null);
      const built = buildOntologyGraph(atlas);

      if (cancelled) return;
      void layoutOntologyGraph(built)
        .then((laidOut) => {
          if (cancelled) return;

          const nodes: Node[] = laidOut.nodes.map((n) => ({
            id: n.id,
            type: "ontologyType",
            position: n.position,
            data: n.data,
            width: n.width,
            height: n.height,
            style: { width: n.width, height: n.height },
            draggable: false,
            selectable: true,
          }));

          const edges: AtlasEdge[] = laidOut.edges.map((e) => {
            const kind: EdgeKind =
              e.type === "implements"
                ? "implements"
                : e.kind === "neighbor"
                  ? "neighbor"
                  : "relation";

            return {
              id: e.id,
              source: e.source,
              target: e.target,
              sourceHandle: e.sourceHandle,
              targetHandle: e.targetHandle,
              type: "ontologyRouted",
              animated: false,
              data: {
                routedPoints: e.points,
                sourceOffset: e.sourceOffset,
                kind,
                accent: typeAccentColor(e.source),
              },
            };
          });

          startTransition(() => {
            setLayout({ atlas, nodes, edges });
          });
        })
        .catch(() => {
          if (!cancelled) setError("Could not arrange the diagram.");
        });
    }

    loadGraph();

    return () => {
      cancelled = true;
    };
  }, [atlas, attempt]);

  const focusedIds = useMemo(() => {
    if (!selected || !graph.nodes.some((node) => node.id === selected))
      return graph.nodes.map((node) => node.id);
    const ids = new Set([selected]);

    for (const edge of graph.edges) {
      if (edge.source === selected || edge.target === selected) {
        ids.add(edge.source);
        ids.add(edge.target);
      }
    }

    return [...ids];
  }, [graph, selected]);

  const focused = new Set(focusedIds);
  // Hovering a card traces its relations without moving the viewport; otherwise the focused type does.
  const traced = hovered ?? selected;

  const touches = (edge: AtlasEdge) =>
    traced !== null && (edge.source === traced || edge.target === traced);

  const nodes = graph.nodes.map((node) => ({
    ...node,
    className:
      selected === node.id
        ? "atlas-node--focused"
        : selected && !focused.has(node.id)
          ? "atlas-node--dimmed"
          : undefined,
  }));

  // At rest every connector is the opaque neutral, 3:1 in both themes, and
  // neighbor links stay hidden; tracing a type draws its links in its color,
  // neighbors included, and fades the rest.
  const edges = graph.edges.flatMap((edge) => {
    const kind = edge.data?.kind ?? "relation";
    const lit = touches(edge);

    if (kind === "neighbor" && !lit) return [];
    const color = lit ? edgeAccent(edge) : NEUTRAL_EDGE;

    return [
      {
        ...edge,
        style: {
          stroke: color,
          strokeWidth: lit ? 2 : 1.5,
          strokeDasharray: EDGE_DASH[kind],
          strokeLinecap: kind === "neighbor" ? ("round" as const) : undefined,
          opacity: traced === null || lit ? 1 : 0.16,
        },
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
        zIndex: lit ? 1 : 0,
      },
    ];
  });

  const query = search.trim().toLowerCase();

  const listed = query
    ? graph.nodes
        .filter((node) => node.id.toLowerCase().includes(query))
        .sort((a, b) => a.id.localeCompare(b.id))
    : [];

  const focus = (id: string | null) => {
    setSelected(id);
    setSearch("");
  };

  return (
    <div className="atlas-workbench">
      <div className="atlas-canvas">
        {error ? (
          <p role="alert" className="atlas-canvas__status">
            {error}{" "}
            <button type="button" onClick={() => setAttempt((value) => value + 1)}>
              Retry
            </button>
          </p>
        ) : graph.nodes.length === 0 ? (
          <p role="status" className="atlas-canvas__status">
            {(atlas.types?.length ?? 0) + (atlas.interfaces?.length ?? 0) > 0
              ? "Arranging types…"
              : "No types to display."}
          </p>
        ) : null}
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          fitView
          fitViewOptions={{ padding: FIT_PADDING }}
          minZoom={0.15}
          maxZoom={1.5}
          nodesDraggable={false}
          nodesConnectable={false}
          panOnDrag
          panOnScroll
          zoomOnScroll={false}
          zoomOnPinch
          proOptions={{ hideAttribution: true }}
          onNodeMouseEnter={(_, node) => setHovered(node.id)}
          onNodeMouseLeave={() => setHovered(null)}
        >
          <FocusViewport ids={focusedIds} selected={selected} />
          <Background gap={24} size={1} />
          <Controls showInteractive={false} />
        </ReactFlow>
        <div className="atlas-find" role="search" aria-label="Atlas types">
          <input
            type="search"
            aria-label="Find a type"
            placeholder="Find a type…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && listed[0]) {
                event.preventDefault();
                focus(listed[0].id);
              }

              if (event.key === "Escape") setSearch("");
            }}
          />
          {query ? (
            <div className="atlas-find__list">
              {listed.map((node) => (
                <button
                  type="button"
                  key={node.id}
                  aria-label={`Focus ${node.id}`}
                  aria-pressed={selected === node.id}
                  onClick={() => focus(node.id)}
                >
                  <span className="atlas-find__name">{node.id}</span>
                  <span className="atlas-find__count">
                    {node.data.role === "interface" ? "interface" : String(node.data.count ?? 0)}
                  </span>
                </button>
              ))}
              {listed.length === 0 ? <p className="atlas-find__empty">No matching types.</p> : null}
            </div>
          ) : null}
          {selected ? (
            <p className="atlas-find__focus">
              <span>
                Tracing <strong>{selected}</strong>
              </span>
              <button type="button" onClick={() => focus(null)}>
                Show all types
              </button>
            </p>
          ) : null}
        </div>
      </div>
      <div className="atlas-legend" aria-label="Connection legend">
        <span>
          <s data-kind="relation" /> Relation
        </span>
        <span>
          <s data-kind="implements" /> Implements
        </span>
        <span>
          <s data-kind="neighbor" /> Neighbor, shown while tracing
        </span>
        <span className="atlas-legend__hint">Hover a card to trace it in its color</span>
      </div>
    </div>
  );
}
