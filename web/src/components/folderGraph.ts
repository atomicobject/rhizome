import type { GraphEdge, GraphNode } from "../api/types";
import { publicTypeName } from "../lib/typeNames";
import { CODE_COLOR, type GraphViewNode, UNTYPED_COLOR } from "./graphViewShared";

/** A folder holding more than this share of the graph opens into its subfolders. */
const SPLIT_SHARE = 0.15;

/** Subfolders smaller than this share of the graph fold back into one node for their parent. */
const MERGE_SHARE = 0.015;

/** Each folder keeps its heaviest links to this many other folders, so the overview shows its main roads. */
const LINKS_PER_FOLDER = 2;

function nodePath(node: GraphNode) {
  return (node.notePath || node.path || "").replace(/^\/+/, "");
}

/** The node's folder segments; a module node is its own folder. */
function folderSegments(node: GraphNode) {
  const parts = nodePath(node).split("/").filter(Boolean);

  return node.kind === "module" ? parts : parts.slice(0, -1);
}

const weightOf = (node: GraphNode) =>
  node.kind === "module" ? Math.max(1, node.childCount ?? 1) : 1;

const fmt = (count: number) => count.toLocaleString("en-US");

type Folder = { path: string; weight: number; members: GraphNode[]; merged: number };

export type FolderGraph = { nodes: GraphViewNode[]; edges: GraphEdge[]; truncated: false };

/**
 * The root Explorer view: one node per folder, sized by what it holds and
 * colored by what most of it is (a note type, code, or untyped notes), with
 * links counted between folders. A folder holding a large share of the vault
 * opens into its subfolders, and its small subfolders fold back into one node,
 * so no single node swallows the picture and none is noise. Selecting a folder
 * loads its own graph.
 */
export function aggregateByFolder(
  graph: { nodes: readonly GraphNode[]; edges: readonly GraphEdge[] },
  typeColor: (type: string) => string | undefined,
): FolderGraph {
  const total = graph.nodes.reduce((sum, node) => sum + weightOf(node), 0);
  const split = new Set<string>();

  // The deepest folder prefix not marked for splitting.
  const leafFor = (node: GraphNode) => {
    const segments = folderSegments(node);
    let depth = 1;

    while (depth < segments.length && split.has(segments.slice(0, depth).join("/"))) depth += 1;

    return segments.slice(0, depth).join("/");
  };

  for (;;) {
    const weights = new Map<string, { weight: number; deeper: boolean }>();

    for (const node of graph.nodes) {
      const path = leafFor(node);
      const entry = weights.get(path) ?? { weight: 0, deeper: false };
      entry.weight += weightOf(node);
      entry.deeper ||= folderSegments(node).length > (path ? path.split("/").length : 0);
      weights.set(path, entry);
    }

    const next = [...weights].filter(
      ([path, entry]) => path && entry.deeper && entry.weight > total * SPLIT_SHARE,
    );

    if (next.length === 0) break;

    for (const [path] of next) split.add(path);
  }

  const leaves = new Map<string, Folder>();

  for (const node of graph.nodes) {
    const path = leafFor(node);
    const folder = leaves.get(path) ?? { path, weight: 0, members: [], merged: 0 };
    folder.weight += weightOf(node);
    folder.members.push(node);
    leaves.set(path, folder);
  }

  // Small subfolders of a split folder share one node, which opens the parent.
  const folders = new Map<string, Folder>();
  const folderOf = new Map<string, string>();

  for (const leaf of leaves.values()) {
    const parent = leaf.path.split("/").slice(0, -1).join("/");
    const merge = parent !== "" && split.has(parent) && leaf.weight < total * MERGE_SHARE;
    const path = merge ? parent : leaf.path;
    const folder = folders.get(path) ?? { path, weight: 0, members: [], merged: 0 };
    folder.weight += leaf.weight;
    folder.members.push(...leaf.members);
    folder.merged += merge ? 1 : 0;
    folders.set(path, folder);

    for (const node of leaf.members) folderOf.set(node.id, path);
  }

  const nodes: GraphViewNode[] = [...folders.values()].map((folder) => {
    const name = folder.path || "Top-level files";
    const label = folder.merged > 0 ? `${name} · ${folder.merged} smaller folders` : name;

    return {
      id: `folder:${folder.path}`,
      kind: "module",
      path: folder.path,
      label: `${label}  ${fmt(folder.weight)}`,
      childCount: folder.weight,
      fill: folderFill(folder.members, typeColor),
    };
  });

  const weights = new Map<string, { source: string; target: string; weight: number }>();

  for (const edge of graph.edges) {
    const a = folderOf.get(edge.source);
    const b = folderOf.get(edge.target);

    if (a === undefined || b === undefined || a === b) continue;
    const [source, target] = a < b ? [a, b] : [b, a];
    const key = `${source}|${target}`;
    const entry = weights.get(key) ?? { source, target, weight: 0 };
    entry.weight += edge.weight ?? 1;
    weights.set(key, entry);
  }

  // Keep a link when it is one of the heaviest for either of its folders.
  const byFolder = new Map<string, { key: string; weight: number }[]>();

  for (const [key, entry] of weights) {
    for (const end of [entry.source, entry.target]) {
      const list = byFolder.get(end) ?? [];
      list.push({ key, weight: entry.weight });
      byFolder.set(end, list);
    }
  }

  const kept = new Set<string>();

  for (const list of byFolder.values()) {
    list.sort((a, b) => b.weight - a.weight || a.key.localeCompare(b.key));

    for (const { key } of list.slice(0, LINKS_PER_FOLDER)) kept.add(key);
  }

  const edges: GraphEdge[] = [...kept].sort().map((key) => {
    const entry = weights.get(key)!;

    return {
      source: `folder:${entry.source}`,
      target: `folder:${entry.target}`,
      kind: "folder",
      weight: entry.weight,
      relationLabel: `${fmt(entry.weight)} links`,
    };
  });

  return { nodes, edges, truncated: false };
}

/** A folder's color: its most common note type, or code or untyped notes when they outnumber every type. */
function folderFill(
  members: readonly GraphNode[],
  typeColor: (type: string) => string | undefined,
) {
  const counts = new Map<string, number>();
  let code = 0;
  let untyped = 0;

  for (const node of members) {
    const note = node.kind === "note" || node.kind === "embedded";
    const type = note ? publicTypeName(node.resolvedType) : "";

    if (type) counts.set(type, (counts.get(type) ?? 0) + 1);
    else if (note) untyped += 1;
    else code += weightOf(node);
  }

  const [best] = [...counts].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));

  if (best && best[1] >= code && best[1] >= untyped) return typeColor(best[0]) ?? UNTYPED_COLOR;

  return code >= untyped ? CODE_COLOR : UNTYPED_COLOR;
}
