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

type GraphState = { nodes: Node[]; edges: Edge[] };

const EMPTY_GRAPH: GraphState = { nodes: [], edges: [] };

function FocusViewport({ ids, selected }: { ids: string[]; selected: string | null }) {
  const { fitView, getNode, setCenter, viewportInitialized } = useReactFlow();
  useEffect(() => {
    if (!viewportInitialized || ids.length === 0) return;
    const node = selected ? getNode(selected) : undefined;

    if (node) {
      void setCenter(
        node.position.x + (node.width ?? 280) / 2,
        node.position.y + (node.height ?? 104) / 2,
        { zoom: 0.9 },
      );
    } else {
      void fitView({ nodes: ids.map((id) => ({ id })), padding: 0.06, maxZoom: 1, minZoom: 0.15 });
    }
  }, [viewportInitialized, ids, selected, fitView, getNode, setCenter]);

  return null;
}

export function OntologyGraph({ atlas }: Props) {
  const [selection, setSelected] = useState<string | null>(null);
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

          const edges: Edge[] = laidOut.edges.map((e) => {
            if (e.type === "implements") {
              return {
                id: e.id,
                source: e.source,
                target: e.target,
                sourceHandle: e.sourceHandle,
                targetHandle: e.targetHandle,
                type: "ontologyRouted",
                animated: false,
                data: { routedPoints: e.points },
                style: {
                  stroke: "var(--text-muted, #94a3b8)",
                  strokeDasharray: "4 4",
                  strokeWidth: 1,
                },
                markerEnd: {
                  type: MarkerType.ArrowClosed,
                  color: "var(--text-muted, #94a3b8)",
                  width: 14,
                  height: 14,
                },
                zIndex: 0,
              };
            }

            const accent =
              e.kind === "section"
                ? "var(--text-default, #1f2937)"
                : e.kind === "neighbor"
                  ? "var(--text-muted, #94a3b8)"
                  : typeAccentColor(e.source);

            return {
              id: e.id,
              source: e.source,
              target: e.target,
              sourceHandle: e.sourceHandle,
              targetHandle: e.targetHandle,
              type: "ontologyRouted",
              data: { routedPoints: e.points },
              style: {
                stroke: accent,
                strokeWidth: e.kind === "section" ? 2 : 1.5,
                strokeDasharray: e.kind === "neighbor" ? "5 3" : undefined,
              },
              markerEnd: {
                type: MarkerType.ArrowClosed,
                color: accent,
                width: 16,
                height: 16,
              },
              zIndex: 1,
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

  const nodes = graph.nodes.map((node) => ({
    ...node,
    className:
      selected === node.id
        ? "atlas-node--focused"
        : selected && !focused.has(node.id)
          ? "atlas-node--dimmed"
          : undefined,
  }));

  const edges = graph.edges.map((edge) => ({
    ...edge,
    style: {
      ...edge.style,
      opacity: selected ? (edge.source === selected || edge.target === selected ? 1 : 0.08) : 0.45,
    },
    zIndex: selected && (edge.source === selected || edge.target === selected) ? 2 : 0,
  }));

  const listed = graph.nodes
    .filter((node) => node.id.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => a.id.localeCompare(b.id));

  return (
    <div className="atlas-workbench">
      <aside className="atlas-index" aria-label="Atlas types">
        <label className="atlas-index__search">
          <span>Find a type</span>
          <input
            type="search"
            placeholder="Find a type…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <button
          type="button"
          className="atlas-index__all"
          aria-pressed={selected === null}
          onClick={() => setSelected(null)}
        >
          All types <span>{graph.nodes.length}</span>
        </button>
        <div className="atlas-index__list">
          {listed.map((node) => (
            <button
              type="button"
              key={node.id}
              aria-label={`Focus ${node.id}`}
              aria-pressed={selected === node.id}
              onClick={() => setSelected(node.id)}
            >
              <span className="atlas-index__name">{node.id}</span>
              <span className="atlas-index__count">
                {String(node.data.role === "interface" ? "interface" : (node.data.count ?? 0))}
              </span>
            </button>
          ))}
          {search && listed.length === 0 ? (
            <p className="atlas-index__empty">No matching types.</p>
          ) : null}
        </div>
        <p className="atlas-index__hint">
          Focus a type to trace its connections. Open a card to explore its notes.
        </p>
      </aside>
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
          fitViewOptions={{ padding: 0.15 }}
          minZoom={0.15}
          maxZoom={1.5}
          nodesDraggable={false}
          nodesConnectable={false}
          panOnDrag
          panOnScroll={false}
          zoomOnScroll={false}
          zoomOnPinch
          preventScrolling={false}
          proOptions={{ hideAttribution: true }}
        >
          <FocusViewport ids={focusedIds} selected={selected} />
          <Background gap={24} size={1} color="#dedbd5" />
          <Controls showInteractive={false} />
        </ReactFlow>
        <div className="atlas-legend" aria-label="Connection legend">
          <span>— Relation</span>
          <span>┄ Inheritance / neighbor</span>
        </div>
      </div>
    </div>
  );
}
