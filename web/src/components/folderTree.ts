import type { GraphNode, FolderOverview } from "../api/types";
import { publicTypeName } from "../lib/typeNames";

export type FolderTotals = {
  items?: number;
  notes: number;
  files: number;
  types: Map<string, number>;
  untyped: number;
};

export type Folder = FolderTotals & {
  path: string;
  folders: Folder[];
  direct: GraphNode[];
  members: string[];
};

export type FileLink = { source: string; target: string; noteCode: number; other: number };

export type FolderTree = {
  root: Folder;
  folders: Map<string, Folder>;
  files: Map<string, GraphNode>;
  links: FileLink[];
  linkCounts: Map<string, { noteCode: number; other: number }>;
  affinity: Map<string, Map<string, number>>;
  truncated: boolean;
};

export const filePath = (node: GraphNode) => (node.notePath || node.path || "").replace(/^\/+/, "");

export const folderWeight = (folder: FolderTotals) =>
  folder.notes * 3 + folder.files + (folder.items ?? 0);

export function totalsLabel(totals: FolderTotals) {
  const parts: string[] = [];

  if (totals.notes)
    parts.push(`${totals.notes.toLocaleString("en-US")} ${totals.notes === 1 ? "note" : "notes"}`);

  if (totals.files)
    parts.push(`${totals.files.toLocaleString("en-US")} ${totals.files === 1 ? "file" : "files"}`);

  if (totals.items) parts.push(`${totals.items.toLocaleString("en-US")} items (collapsed)`);

  return parts.join(" · ") || "0 files";
}

export function sumTotals(items: readonly FolderTotals[]): FolderTotals {
  const result: FolderTotals = { notes: 0, files: 0, types: new Map(), untyped: 0 };

  for (const item of items) {
    result.items = (result.items ?? 0) + (item.items ?? 0);
    result.notes += item.notes;
    result.files += item.files;
    result.untyped += item.untyped;

    for (const [type, count] of item.types)
      result.types.set(type, (result.types.get(type) ?? 0) + count);
  }

  return result;
}

export function nodeTotals(node: GraphNode): FolderTotals {
  const type = publicTypeName(node.resolvedType);

  return {
    items: node.kind === "module" ? (node.childCount ?? 0) : 0,
    notes: node.kind === "note" ? 1 : 0,
    files: node.kind === "code" ? 1 : 0,
    types: node.kind === "note" && type ? new Map([[type, 1]]) : new Map(),
    untyped: node.kind === "note" && !type ? 1 : 0,
  };
}

export function buildFolderTree(graph: FolderOverview): FolderTree {
  const folders = new Map<string, Folder>();

  const ensure = (path: string): Folder => {
    const existing = folders.get(path);

    if (existing) return existing;
    const folder: Folder = { path, folders: [], direct: [], members: [], ...sumTotals([]) };
    folders.set(path, folder);

    if (path) ensure(path.split("/").slice(0, -1).join("/")).folders.push(folder);

    return folder;
  };

  const root = ensure("");
  const files = new Map<string, GraphNode>();

  for (const node of graph.files) {
    if (node.kind !== "note" && node.kind !== "code" && node.kind !== "module") continue;
    files.set(node.id, node);

    ensure(
      node.kind === "module" ? filePath(node) : filePath(node).split("/").slice(0, -1).join("/"),
    ).direct.push(node);
  }

  const gather = (folder: Folder) => {
    for (const child of folder.folders) gather(child);
    Object.assign(folder, sumTotals([...folder.folders, ...folder.direct.map(nodeTotals)]));
    folder.members = [
      ...folder.direct.map((n) => n.id),
      ...folder.folders.flatMap((f) => f.members),
    ];
    folder.folders.sort((a, b) => a.path.localeCompare(b.path));
  };

  gather(root);

  const links = graph.links;
  const linkCounts: FolderTree["linkCounts"] = new Map();

  for (const link of links)
    for (const id of [link.source, link.target]) {
      const count = linkCounts.get(id) ?? { noteCode: 0, other: 0 };
      count.noteCode += link.noteCode;
      count.other += link.other;
      linkCounts.set(id, count);
    }

  const affinity: FolderTree["affinity"] = new Map();

  {
    const folder = root;
    const childOf = new Map<string, string>();

    for (const child of folder.folders)
      for (const id of child.members) childOf.set(id, `folder:${child.path}`);

    for (const file of folder.direct) childOf.set(file.id, file.id);
    const weights = new Map<string, number>();

    for (const link of links) {
      const a = childOf.get(link.source),
        b = childOf.get(link.target);

      if (a && b && a !== b) {
        const key = JSON.stringify([a, b].sort());
        weights.set(key, (weights.get(key) ?? 0) + link.noteCode * 3 + link.other);
      }
    }

    affinity.set(folder.path, weights);
  }

  return { root, folders, files, links, linkCounts, affinity, truncated: graph.truncated ?? false };
}

export function initialOpenFolders(tree: FolderTree): Set<string> {
  const open = new Set([""]);

  const visit = (folder: Folder) => {
    for (const child of folder.folders) {
      if (
        child.folders.length &&
        (child.notes + child.files > (tree.root.notes + tree.root.files) * 0.15 ||
          child.notes > tree.root.notes * 0.2)
      ) {
        open.add(child.path);
      }
    }
  };

  visit(tree.root);

  return open;
}
