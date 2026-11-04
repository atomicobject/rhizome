import type { GraphEdge, GraphNode } from "../api/types";
import { publicTypeName } from "../lib/typeNames";
import { CODE_COLOR, type GraphViewNode, UNTYPED_COLOR } from "./graphViewShared";

/** A folder holding more than this share of the graph opens into its subfolders. */
const SPLIT_SHARE = 0.15;

/** Subfolders below this share of the graph and of its notes fold back into one node for their parent. */
const MERGE_SHARE = 0.015;

/**
 * Notes are few beside code files, so their shares are larger: a folder
 * holding a fifth of the notes opens, and a subfolder below a twentieth of
 * them folds back.
 */
const NOTE_SPLIT_SHARE = 0.2;

const NOTE_MERGE_SHARE = 0.05;

/**
 * Beside the spanning tree, a link carrying at least this share of the
 * heaviest one is drawn by default. The rest are minor links, drawn only while
 * one of their folders is hovered.
 */
const MAJOR_SHARE = 0.2;

/** The edge kind of a minor link; the renderer hides it until one of its folders is hovered. */
export const MINOR_FOLDER_LINK = "folder-minor";

function nodePath(node: GraphNode) {
  return (node.notePath || node.path || "").replace(/^\/+/, "");
}

/** The node's folder segments; a module node is its own folder. */
function folderSegments(node: GraphNode) {
  const parts = nodePath(node).split("/").filter(Boolean);

  return node.kind === "module" ? parts : parts.slice(0, -1);
}

/**
 * What a folder holds: its notes and code files. Embedded items (acceptance
 * criteria, user stories) live inside notes, so they neither size nor color it.
 */
const weightOf = (node: GraphNode) =>
  node.kind === "module" ? Math.max(1, node.childCount ?? 1) : node.kind === "embedded" ? 0 : 1;

const isNote = (node: GraphNode) => node.kind === "note";

const fmt = (count: number) => count.toLocaleString("en-US");

type Folder = { path: string; weight: number; members: GraphNode[]; merged: number };

/** What a folder's count counts: "76 notes", "1,295 files", or both. */
function contents(members: readonly GraphNode[]) {
  const notes = members.filter(isNote).length;
  const files = members.reduce((sum, node) => sum + (isNote(node) ? 0 : weightOf(node)), 0);
  const noun = (n: number, one: string) => `${fmt(n)} ${n === 1 ? one : `${one}s`}`;

  if (notes && files) return `${noun(notes, "note")}, ${noun(files, "file")}`;

  return notes ? noun(notes, "note") : noun(files, "file");
}

export type FolderGraph = { nodes: GraphViewNode[]; edges: GraphEdge[]; truncated: boolean };

/**
 * The root Explorer view: one node per folder, sized by what it holds and
 * colored by what most of it is (a note type, code, or untyped notes), with
 * links counted between folders. A folder holding a large share of the vault
 * opens into its subfolders, and its small subfolders fold back into one node,
 * so no single node swallows the picture and none is noise. Selecting a folder
 * loads its own graph.
 */
export function aggregateByFolder(
  graph: { nodes: readonly GraphNode[]; edges: readonly GraphEdge[]; truncated?: boolean },
  typeColor: (type: string) => string | undefined,
): FolderGraph {
  const total = graph.nodes.reduce((sum, node) => sum + weightOf(node), 0);
  const totalNotes = graph.nodes.filter(isNote).length;
  const split = new Set<string>();

  // The deepest folder prefix not marked for splitting.
  const leafFor = (node: GraphNode) => {
    const segments = folderSegments(node);
    let depth = 1;

    while (depth < segments.length && split.has(segments.slice(0, depth).join("/"))) depth += 1;

    return segments.slice(0, depth).join("/");
  };

  for (;;) {
    const weights = new Map<string, { weight: number; notes: number; deeper: boolean }>();

    for (const node of graph.nodes) {
      const path = leafFor(node);
      const entry = weights.get(path) ?? { weight: 0, notes: 0, deeper: false };
      entry.weight += weightOf(node);
      entry.notes += isNote(node) ? 1 : 0;
      entry.deeper ||= folderSegments(node).length > (path ? path.split("/").length : 0);
      weights.set(path, entry);
    }

    const next = [...weights].filter(
      // A folder opens when it holds a large share of everything, or of the notes alone.
      ([path, entry]) =>
        path &&
        entry.deeper &&
        (entry.weight > total * SPLIT_SHARE || entry.notes > totalNotes * NOTE_SPLIT_SHARE),
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
    const notes = leaf.members.filter(isNote).length;

    const merge =
      parent !== "" &&
      split.has(parent) &&
      leaf.weight < total * MERGE_SHARE &&
      notes < totalNotes * NOTE_MERGE_SHARE;

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

    const fill = folderFill(folder.members);

    return {
      id: `folder:${folder.path}`,
      kind: "module",
      path: folder.path,
      label: `${label}  ${contents(folder.members)}`,
      childCount: Math.max(1, folder.weight),
      fill: fill.type ? (typeColor(fill.type) ?? UNTYPED_COLOR) : fill.color,
      fillKey: fill.key,
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

  // The main roads: a maximum spanning tree, so every folder stays attached
  // by its strongest links with no cycle to cross, plus any heavy link.
  const ranked = [...weights].sort((a, b) => b[1].weight - a[1].weight || a[0].localeCompare(b[0]));
  const root = new Map<string, string>();

  const find = (path: string): string => {
    const parent = root.get(path) ?? path;

    if (parent === path) return path;
    const top = find(parent);
    root.set(path, top);

    return top;
  };

  const max = ranked[0]?.[1].weight ?? 0;
  const major = new Set<string>();

  for (const [key, entry] of ranked) {
    const [a, b] = [find(entry.source), find(entry.target)];

    if (a !== b) root.set(a, b);

    if (a !== b || entry.weight >= max * MAJOR_SHARE) major.add(key);
  }

  const edges: GraphEdge[] = [...weights.keys()].sort().map((key) => {
    const entry = weights.get(key)!;

    return {
      source: `folder:${entry.source}`,
      target: `folder:${entry.target}`,
      kind: major.has(key) ? "folder" : MINOR_FOLDER_LINK,
      weight: entry.weight,
      relationLabel: `${fmt(entry.weight)} links`,
    };
  });

  return { nodes, edges, truncated: graph.truncated ?? false };
}

/** Legend keys for folders colored by code or untyped notes rather than a note type. */
export const CODE_FILL_KEY = "__code__";

export const UNTYPED_FILL_KEY = "__untyped__";

type FolderFill = {
  /** The legend key: a type name, CODE_FILL_KEY, or UNTYPED_FILL_KEY. */
  key: string;
  /** The note type whose palette color the folder takes. */
  type?: string;
  /** The color when no type sets it. */
  color: string;
};

/**
 * A folder's color: its most common note type, or code or untyped notes when
 * they outnumber every type. `key` names it in the legend: the type, or one of
 * the fill keys above. Embedded items count for nothing here.
 */
function folderFill(members: readonly GraphNode[]): FolderFill {
  const counts = new Map<string, number>();
  let code = 0;
  let untyped = 0;

  for (const node of members) {
    const type = isNote(node) ? publicTypeName(node.resolvedType) : "";

    if (type) counts.set(type, (counts.get(type) ?? 0) + 1);
    else if (isNote(node)) untyped += 1;
    else code += weightOf(node);
  }

  const [best] = [...counts].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));

  if (best && best[1] >= code && best[1] >= untyped)
    return { key: best[0], type: best[0], color: UNTYPED_COLOR };

  return code >= untyped
    ? { key: CODE_FILL_KEY, color: CODE_COLOR }
    : { key: UNTYPED_FILL_KEY, color: UNTYPED_COLOR };
}
