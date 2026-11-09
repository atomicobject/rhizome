// The built-in Folder panel (SPEC-0136 R6): native host UI showing the active
// note's folder with its ancestors, its subfolders, and the notes beside it.
// A note opens in Notes; a folder name opens Explorer at that folder.

import { useQuery } from "@tanstack/react-query";
import { type CSSProperties, useLayoutEffect, useMemo } from "react";

import { getFolderGraph } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { buildFolderTree, filePath, type FolderTree } from "./folderTree";
import { notifyLocationChange } from "./locationStore";
import { buildNotesLocation, parseNotesLocation } from "./notesRoute";
import { useRail } from "./RailTree";
import { usePanelOpen } from "./SidebarPanelSlot";

export type FolderRow = {
  kind: "folder" | "note";
  path: string;
  name: string;
  depth: number;
  /** The note's own folder, or the note itself. */
  current: boolean;
};

const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/**
 * The rows Folder shows for `notePath`: each ancestor folder from the top,
 * ending at the note's folder, then that folder's subfolders and notes. Null
 * when the folder graph does not hold the note's folder.
 */
export function folderRows(tree: FolderTree, notePath: string): FolderRow[] | null {
  const path = notePath.replace(/^\/+/, "");
  const dir = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
  const folder = tree.folders.get(dir);

  if (!folder) return null;
  const parts = dir ? dir.split("/") : [];

  const ancestors = parts.map((name, index) => ({
    kind: "folder" as const,
    path: parts.slice(0, index + 1).join("/"),
    name,
    depth: index,
    current: index === parts.length - 1,
  }));

  const depth = parts.length;

  const folders = folder.folders.map((child) => ({
    kind: "folder" as const,
    path: child.path,
    name: basename(child.path),
    depth,
    current: false,
  }));

  const notes = folder.direct
    .filter((node) => node.kind === "note")
    .map((node) => ({
      kind: "note" as const,
      path: filePath(node),
      name: node.label || basename(filePath(node)),
      depth,
      current: filePath(node) === path,
    }))
    .sort((a, b) => a.name.localeCompare(b.name));

  return [...ancestors, ...folders, ...notes];
}

function openNote(path: string) {
  const current = parseNotesLocation(
    window.location.pathname,
    window.location.search,
    window.location.hash,
  );

  window.history.pushState(
    {},
    "",
    buildNotesLocation({ selection: current.selection, note: path }),
  );
  notifyLocationChange();
}

function openExplorerFolder(path: string) {
  window.history.pushState({}, "", `/explorer?${new URLSearchParams({ folder: path })}`);
  notifyLocationChange();
}

const FOLDER_ICON =
  "M2 4.5A1 1 0 0 1 3 3.5h3l1.5 1.5H13a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1z";

const NOTE_ICON = "M4 2.5h5.5L12 5v8.5H4zM9.5 2.5V5H12M6 8h4M6 10.5h4";

function RowIcon({ kind }: { kind: FolderRow["kind"] }) {
  return (
    <svg
      className="folder-panel__icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d={kind === "folder" ? FOLDER_ICON : NOTE_ICON} />
    </svg>
  );
}

type DepthStyle = CSSProperties & { "--folder-depth": number };

export function FolderPanel({ notePath, active = true }: { notePath: string; active?: boolean }) {
  const open = usePanelOpen();

  const graph = useQuery({
    queryKey: queryKeys.graph.folders(),
    queryFn: ({ signal }) => getFolderGraph({ signal }),
    enabled: active && open,
  });

  const tree = useMemo(() => (graph.data ? buildFolderTree(graph.data) : null), [graph.data]);
  const rows = useMemo(() => (tree ? folderRows(tree, notePath) : null), [tree, notePath]);
  const { railProps } = useRail("rhizome:notes:folder-panel:v1", [], null);

  // The note stays in view as it changes.
  useLayoutEffect(() => {
    railProps.ref.current
      ?.querySelector('[aria-current="page"]')
      ?.scrollIntoView?.({ block: "nearest" });
  }, [rows, railProps.ref]);

  if (!rows) {
    return (
      <p className="folder-panel__state">
        {graph.isPending
          ? "Loading folder…"
          : graph.isError
            ? "The folder could not be read."
            : "This note’s folder is not in the file graph yet."}
      </p>
    );
  }

  return (
    <nav className="folder-panel" aria-label="Folder contents" {...railProps}>
      {rows.map((row) => {
        const style: DepthStyle = { "--folder-depth": row.depth };
        const folder = row.kind === "folder";

        return (
          <button
            key={`${row.kind}:${row.path}`}
            type="button"
            data-rail-row
            className={`folder-panel__row folder-panel__row--${row.kind}${folder && row.current ? " folder-panel__row--current" : ""}${!folder && row.current ? " is-selected" : ""}`}
            style={style}
            aria-current={!folder && row.current ? "page" : undefined}
            title={folder ? `Open ${row.path} in Explorer` : row.path}
            onClick={() => (folder ? openExplorerFolder(row.path) : openNote(row.path))}
          >
            <RowIcon kind={row.kind} />
            <span className="folder-panel__label">{row.name}</span>
          </button>
        );
      })}
    </nav>
  );
}
