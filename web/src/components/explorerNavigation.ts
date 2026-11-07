import type { GraphResponse } from "../api/types";
import { encodeURLFragment } from "../lib/noteDeepLink";
import { NOTES_ROOT_PATH } from "./notesRoute";

export function moduleKey(path: string, depth: number) {
  const cleaned = path.replace(/^\/+/, "");
  const parts = cleaned.split("/");

  if (parts.length === 0) return "root";

  if (parts.length < depth) return parts.join("/");

  return parts.slice(0, depth).join("/");
}

export function isNoteTarget(path: string, kind?: string): boolean {
  if (kind) return ["note", "section", "embedded"].includes(kind.toLowerCase());
  const barePath = path.split("#", 1)[0] || path;

  return /\.(?:md|html?)$/i.test(barePath);
}

export function openNoteWorkspace(path: string) {
  const hashIndex = path.indexOf("#");
  const notePath = hashIndex >= 0 ? path.slice(0, hashIndex) : path;
  const noteHash = hashIndex >= 0 ? path.slice(hashIndex) : "";
  const params = new URLSearchParams();
  params.set("note", notePath);
  window.history.pushState(
    {},
    "",
    `${NOTES_ROOT_PATH}?${params.toString()}${encodeURLFragment(noteHash)}`,
  );
  window.dispatchEvent(new PopStateEvent("popstate"));
}

export function explorerFileFromLocation(search: string): { path: string; line?: number } | null {
  const params = new URLSearchParams(search);
  const path = params.get("file")?.trim();

  if (!path) return null;
  const parsedLine = Number(params.get("line"));

  return {
    path,
    line: Number.isInteger(parsedLine) && parsedLine > 0 ? parsedLine : undefined,
  };
}

/**
 * What the folder view's counts cover. The graph can hold only part of a large
 * vault, so its note counts can run below Overview's; say so when it does.
 */
export function folderCoverage(graph: GraphResponse) {
  const fmt = (count: number) => count.toLocaleString("en-US");
  const notes = graph.nodes.filter((node) => node.kind === "note").length;
  const files = graph.nodes.filter((node) => node.kind === "code").length;
  const counts = `Counts cover the ${fmt(notes)} notes and ${fmt(files)} code files in the graph`;

  return graph.truncated
    ? `${counts}, the best-connected part of the vault, so Overview can count more notes.`
    : `${counts}.`;
}
