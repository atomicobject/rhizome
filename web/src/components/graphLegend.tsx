import { useState } from "react";
import { ChevronDown } from "lucide-react";
import type { GraphEdge, GraphNode } from "../api/types";
import { publicTypeName } from "../lib/typeNames";
import { CODE_COLOR, FOLDER_COLOR, UNTYPED_COLOR } from "./graphViewShared";

export const UNTYPED_NODE_TYPE = "__untyped__";

/** Legend keys for nodes that are not notes; their colors match `nodeColor` under a type-color map. */
export const CODE_NODE_TYPE = "__code__";

export const FOLDER_NODE_TYPE = "__module__";

export const KIND_ENTRIES = new Map([
  [UNTYPED_NODE_TYPE, { label: "Untyped notes", color: UNTYPED_COLOR }],
  [CODE_NODE_TYPE, { label: "Code files", color: CODE_COLOR }],
  [FOLDER_NODE_TYPE, { label: "Folders", color: FOLDER_COLOR }],
]);

/** Legend groups, in order: what the colors of notes mean, then code, then folders. */
export const LEGEND_GROUPS = [
  { key: "typed", title: "Notes by type" },
  { key: "code", title: "Code" },
  { key: "folder", title: "Folders" },
] as const;

export function legendGroup(name: string) {
  if (name === CODE_NODE_TYPE) return "code";

  if (name === FOLDER_NODE_TYPE) return "folder";

  return "typed";
}

export const EMPTY_PROBLEM_COUNTS: ReadonlyMap<string, number> = new Map();

export type GraphTypeEntry = {
  name: string;
  label: string;
  count: number;
  color: string;
  selected: boolean;
};

export function graphTypeName(node: GraphNode): string {
  if (node.kind === "code") return CODE_NODE_TYPE;

  if (node.kind === "module") return FOLDER_NODE_TYPE;

  return publicTypeName(node.resolvedType) || UNTYPED_NODE_TYPE;
}

export function legendItem(entry: GraphTypeEntry) {
  return (
    <li
      key={entry.name}
      className={`graph-type-legend__item ${entry.selected ? "is-highlighted" : ""}`}
      data-type={entry.name}
    >
      <span
        className="graph-type-legend__swatch"
        style={entry.name === FOLDER_NODE_TYPE ? undefined : { backgroundColor: entry.color }}
        aria-hidden="true"
      />
      <span className="graph-type-legend__label">{entry.label}</span>
      <span className="graph-type-legend__count num">{entry.count.toLocaleString("en-US")}</span>
    </li>
  );
}

/**
 * The folder view's key: the hues its folders actually take first, then what
 * size and lines mean; every type in the graph waits behind "All types".
 */
export function FolderLegend({
  entries,
  shown,
  caption,
  sizeCaption,
  edgeCaption,
  edges,
}: {
  entries: readonly GraphTypeEntry[];
  /** The fill keys the drawn folders use. */
  shown: ReadonlySet<string>;
  caption?: string;
  sizeCaption?: string;
  edgeCaption?: string;
  edges: GraphEdge[];
}) {
  const [all, setAll] = useState(false);
  const inventory = entries.filter((entry) => entry.name !== FOLDER_NODE_TYPE);

  return (
    <aside className="graph-type-legend graph-type-legend--folders" aria-label="Graph type legend">
      <div className="graph-type-legend__row">
        <h3 className="graph-type-legend__title">Types</h3>
        <ul className="graph-type-legend__list">
          {inventory
            .filter((entry) => shown.has(entry.name) && legendGroup(entry.name) !== "code")
            .map(legendItem)}
        </ul>
        <button
          type="button"
          className="graph-type-legend__more"
          aria-expanded={all}
          aria-controls="graph-type-legend-all"
          onClick={() => setAll((open) => !open)}
        >
          <span className="graph-type-legend__chevron" aria-hidden="true">
            <ChevronDown size={16} strokeWidth={1.75} />
          </span>
          All types
        </button>
      </div>
      <div className="graph-type-legend__row graph-type-legend__keys">
        <h3 className="graph-type-legend__title">Graph</h3>
        <span className="graph-type-legend__key">
          <span
            className="graph-type-legend__swatch"
            style={{
              background: "transparent",
              border: "1.5px solid currentColor",
              borderRadius: 2,
            }}
            aria-hidden="true"
          />
          Folder (colour: what it mostly holds)
        </span>
        <span className="graph-type-legend__key">
          <span
            className="graph-type-legend__swatch"
            style={{ background: "transparent", border: "1.5px solid currentColor" }}
            aria-hidden="true"
          />
          File
        </span>
        <span className="graph-type-legend__key">
          <span
            className="graph-type-legend__swatch"
            style={{
              background: "transparent",
              border: "1.5px dashed currentColor",
              borderRadius: 2,
            }}
            aria-hidden="true"
          />
          +N more
        </span>
        <ul className="graph-type-legend__list">
          {inventory.filter((entry) => legendGroup(entry.name) === "code").map(legendItem)}
        </ul>
        {sizeCaption && (
          <span className="graph-type-legend__key">
            <span className="graph-type-legend__size" aria-hidden="true" />
            {sizeCaption}
          </span>
        )}
        {edgeCaption && (
          <span className="graph-type-legend__key">
            <span className="graph-type-legend__line" aria-hidden="true" />
            {edgeCaption}
          </span>
        )}
      </div>
      <EdgeLegend edges={edges} />
      {caption && <p className="graph-type-legend__caption">{caption}</p>}
      {all && (
        <section id="graph-type-legend-all" className="graph-type-legend__all">
          <h3 className="graph-type-legend__title">Everything in the graph</h3>
          <ul className="graph-type-legend__list">{inventory.map(legendItem)}</ul>
        </section>
      )}
    </aside>
  );
}

export function EdgeLegend({ edges }: { edges: GraphEdge[] }) {
  const kinds = new Set(edges.map((edge) => edge.kind));

  if (
    ![
      "folder-containment",
      "folder-crosslink",
      "folder-minor",
      "explorer-documents",
      "explorer-code",
      "explorer-tests",
    ].some((kind) => kinds.has(kind))
  )
    return null;

  return (
    <div className="graph-edge-legend" aria-label="Graph link legend">
      {[
        ["folder-containment", "Contains"],
        ["explorer-documents", "Documents / mentions"],
        ["explorer-code", "Calls / imports"],
        ["explorer-tests", "Tests"],
        ["folder-crosslink", "Note–code links"],
        ["folder-minor", "Other links on hover"],
      ].flatMap(([kind, label]) =>
        kinds.has(kind)
          ? [
              <span className="graph-type-legend__key" key={kind}>
                <span
                  className={`graph-edge-legend__line graph-edge-legend__line--${kind}`}
                  aria-hidden="true"
                />
                {label}
              </span>,
            ]
          : [],
      )}
    </div>
  );
}
