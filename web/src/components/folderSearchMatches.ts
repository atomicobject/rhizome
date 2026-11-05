import type { GraphResponse } from "../api/types";
import type { FolderGraph } from "./folderGraph";

/** Highlight the visible representatives without expanding or moving the folder graph. */
export function folderSearchMatches(
  folders: FolderGraph | null,
  graph: GraphResponse,
  paths: Set<string>,
) {
  const ids = new Set(
    graph.nodes
      .filter((node) => paths.has(node.notePath || node.path || ""))
      .map((node) => node.id),
  );

  const matches = new Set<string>();

  for (const children of folders?.children.values() ?? []) {
    for (const child of children) {
      if (child.members.some((id) => ids.has(id))) matches.add(child.node.id);
    }
  }

  return matches;
}
