import type { GraphEdge } from "../api/types";
import { CODE_COLOR, type GraphViewNode, UNTYPED_COLOR } from "./graphViewShared";
import {
  filePath,
  folderWeight,
  nodeTotals,
  sumTotals,
  totalsLabel,
  type Folder,
  type FolderTree,
  type FolderTotals,
} from "./folderTree";

export const MINOR_FOLDER_LINK = "folder-minor";

export const CODE_FILL_KEY = "__code__";

export const UNTYPED_FILL_KEY = "__untyped__";

// Worker B owns the corresponding additive GraphViewNode fields.
export type FolderViewNode = GraphViewNode & {
  x?: number;
  y?: number;
  labelDetail?: string;
  aggregate?: boolean;
};

export type FolderChild = {
  node: FolderViewNode;
  totals: FolderTotals;
  members: string[];
  folder?: Folder;
};

export type FolderGraph = {
  nodes: FolderViewNode[];
  edges: GraphEdge[];
  truncated: boolean;
  children: Map<string, FolderChild[]>;
};

export type FolderExpansion = {
  open: ReadonlySet<string>;
  explicit: ReadonlySet<string>;
  showAll: ReadonlySet<string>;
};

function fill(totals: FolderTotals, color: (type: string) => string | undefined) {
  const [best] = [...totals.types].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));

  if (best && best[1] >= totals.files && best[1] >= totals.untyped)
    return { fillKey: best[0], fill: color(best[0]) ?? UNTYPED_COLOR };

  return totals.files >= totals.untyped
    ? { fillKey: CODE_FILL_KEY, fill: CODE_COLOR }
    : { fillKey: UNTYPED_FILL_KEY, fill: UNTYPED_COLOR };
}

function folderChild(folder: Folder, color: (type: string) => string | undefined): FolderChild {
  return {
    folder,
    totals: folder,
    members: folder.members,
    node: {
      id: `folder:${folder.path}`,
      kind: "module",
      path: folder.path,
      label: folder.path.split("/").pop() || "Vault",
      labelDetail: totalsLabel(folder),
      childCount: folder.notes + folder.files,
      ...fill(folder, color),
    },
  };
}

export function visibleFolderChildren(
  tree: FolderTree,
  folder: Folder,
  state: FolderExpansion,
  color: (type: string) => string | undefined,
): FolderChild[] {
  const score = (child: FolderChild) =>
    folderWeight(child.totals) +
    child.members.reduce((sum, id) => sum + (tree.linkCounts.get(id)?.noteCode ?? 0), 0);

  const folders = folder.folders
    .map((f) => folderChild(f, color))
    .sort((a, b) => score(b) - score(a) || a.node.id.localeCompare(b.node.id));

  const files: FolderChild[] = folder.direct.map((node) => ({
    node: {
      ...node,
      label: filePath(node).split("/").pop() || node.label,
      labelDetail: totalsLabel(nodeTotals(node)),
    },
    totals: nodeTotals(node),
    members: [node.id],
  }));

  files.sort((a, b) => {
    const x = tree.linkCounts.get(a.node.id),
      y = tree.linkCounts.get(b.node.id);

    return (
      (y?.noteCode ?? 0) - (x?.noteCode ?? 0) ||
      (y?.other ?? 0) - (x?.other ?? 0) ||
      a.node.id.localeCompare(b.node.id)
    );
  });

  const showFiles =
    folder.path === "" || state.explicit.has(folder.path) || (!folders.length && files.length <= 8);

  const budget = showFiles && state.explicit.has(folder.path) ? 14 : 10;
  const fileSlots = showFiles ? Math.min(files.length, folders.length ? 8 : budget - 1) : 0;

  const candidates: FolderChild[] =
    folders.length + files.length <= budget
      ? [...folders, ...(showFiles ? files : [])]
      : [...folders.slice(0, budget - fileSlots - 1), ...files.slice(0, fileSlots)];

  const allCount = folders.length + files.length;

  const kept = state.showAll.has(folder.path)
    ? [...folders, ...files]
    : folder.path === ""
      ? [...folders]
      : candidates.slice(0, allCount > budget ? budget - 1 : budget);

  const keptIds = new Set(kept.map((c) => c.node.id));
  const hidden = [...folders, ...files].filter((c) => !keptIds.has(c.node.id));

  if (hidden.length) {
    const totals = sumTotals(hidden.map((c) => c.totals));
    const hiddenFolders = hidden.filter((c) => c.folder).length;

    const hiddenNotes = hidden.filter((c) => !c.folder && c.node.kind === "note").length;
    const hiddenFiles = hidden.length - hiddenFolders - hiddenNotes;
    const noun = (n: number, word: string) => `${n} more ${word}${n === 1 ? "" : "s"}`;

    const label =
      [
        hiddenFolders ? noun(hiddenFolders, "folder") : "",
        hiddenNotes ? noun(hiddenNotes, "note") : "",
        hiddenFiles ? noun(hiddenFiles, "file") : "",
      ]
        .filter(Boolean)
        .join(" · ") + ` in ${folder.path || "root"}`;

    kept.push({
      node: {
        id: `more:${folder.path}`,
        path: folder.path,
        kind: "module",
        label,
        labelDetail: totalsLabel(totals),
        aggregate: true,
        childCount: totals.notes + totals.files,
        ...fill(totals, color),
      },
      totals,
      members: hidden.flatMap((c) => c.members),
    });
  }

  return kept;
}

export function visibleFolderGraph(
  tree: FolderTree,
  state: FolderExpansion,
  color: (type: string) => string | undefined,
): FolderGraph {
  const root = folderChild(tree.root, color);
  const nodes = [root.node];
  const edges: GraphEdge[] = [];
  const children = new Map<string, FolderChild[]>();
  const representatives = new Map<string, string>();

  const visit = (child: FolderChild) => {
    for (const id of child.members) representatives.set(id, child.node.id);

    if (!child.folder || !state.open.has(child.folder.path)) return;
    const list = visibleFolderChildren(tree, child.folder, state, color);
    children.set(child.folder.path, list);

    for (const next of list) {
      nodes.push(next.node);
      edges.push({
        source: child.node.id,
        target: next.node.id,
        kind: "folder-containment",
        weight: next.members.length,
        relationLabel: totalsLabel(next.totals),
      });
      visit(next);
    }
  };

  visit(root);
  const aggregated = new Map<string, GraphEdge>();

  for (const link of tree.links) {
    const a = representatives.get(link.source),
      b = representatives.get(link.target);

    if (!a || !b || a === b) continue;
    const [source, target] = [a, b].sort();

    for (const [kind, count] of [
      ["folder-crosslink", link.noteCode],
      [MINOR_FOLDER_LINK, link.other],
    ] as const) {
      if (!count) continue;
      const key = JSON.stringify([source, target, kind]);
      const edge = aggregated.get(key) ?? { source, target, kind, weight: 0 };
      edge.weight = (edge.weight ?? 0) + count;
      edge.relationLabel = `${edge.weight.toLocaleString("en-US")} ${kind === "folder-crosslink" ? "note–code links" : "other links"}`;
      aggregated.set(key, edge);
    }
  }

  const names = new Map(nodes.map((node) => [node.id, node.label.toLowerCase()]));

  for (const node of nodes) {
    const duplicates = nodes.filter((other) => names.get(other.id) === names.get(node.id));

    if (duplicates.length > 1 && node.path) {
      const parts = node.path.split("/");
      let depth = 2;

      while (
        depth < parts.length &&
        duplicates.some(
          (other) =>
            other.id !== node.id &&
            (other.path || "").split("/").slice(-depth).join("/").toLowerCase() ===
              parts.slice(-depth).join("/").toLowerCase(),
        )
      )
        depth++;
      node.label = parts.slice(-depth).join("/");
    }
  }

  return { nodes, edges: [...edges, ...aggregated.values()], truncated: tree.truncated, children };
}
