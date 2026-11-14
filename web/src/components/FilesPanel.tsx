// The built-in Files panel (SPEC-0136 R6, SPEC-0128): native host UI docked
// at the bottom of the Notes sidebar, showing the vault's folders and note
// files as an expandable tree. A note opens in Notes; the active note is
// selected and its folders open to reveal it.

import { useQuery } from "@tanstack/react-query";
import { type CSSProperties, useEffect, useLayoutEffect, useMemo } from "react";

import { getFolderGraph } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { DisclosureChevron } from "./DisclosureChevron";
import { buildFolderTree, filePath, type Folder, type FolderTree } from "./folderTree";
import { notifyLocationChange } from "./locationStore";
import { buildNotesLocation, parseNotesLocation } from "./notesRoute";
import { useRail } from "./RailTree";
import { usePanelOpen } from "./SidebarPanelSlot";

/** The Files panel's identity, which keeps its height and minimised state. */
export const FILES_PANEL_IDENTITY = "builtin:files";

const EXPANDED_KEY = "rhizome:notes:files-panel:expanded:v1";

export type FileRow = {
  kind: "folder" | "note";
  path: string;
  name: string;
  depth: number;
  /** A folder's expansion; false for a note. */
  open: boolean;
  /** The active note. */
  current: boolean;
};

const basename = (path: string) => path.slice(path.lastIndexOf("/") + 1);

/** The folders holding `notePath`, from the top. */
export function noteFolders(notePath: string): string[] {
  const parts = notePath.replace(/^\/+/, "").split("/").slice(0, -1);

  return parts.map((_, index) => parts.slice(0, index + 1).join("/"));
}

/**
 * The rows the tree shows: under each open folder, its subfolders that hold
 * notes, then its notes, each sorted by name. Code files are left out.
 */
export function fileTreeRows(
  tree: FolderTree,
  isOpen: (folder: string) => boolean,
  currentNote: string | null,
): FileRow[] {
  const current = currentNote?.replace(/^\/+/, "") ?? null;
  const rows: FileRow[] = [];

  const visit = (folder: Folder, depth: number) => {
    for (const child of folder.folders) {
      if (child.notes === 0) continue;
      const open = isOpen(child.path);
      rows.push({
        kind: "folder",
        path: child.path,
        name: basename(child.path),
        depth,
        open,
        current: false,
      });

      if (open) visit(child, depth + 1);
    }

    const notes = folder.direct
      .flatMap((node) => (node.kind === "note" ? [filePath(node)] : []))
      .sort((a, b) => basename(a).localeCompare(basename(b)));

    for (const path of notes)
      rows.push({
        kind: "note",
        path,
        name: basename(path),
        depth,
        open: false,
        current: path === current,
      });
  };

  visit(tree.root, 0);

  return rows;
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

const NOTE_ICON = "M4 2.5h5.5L12 5v8.5H4zM9.5 2.5V5H12M6 8h4M6 10.5h4";

type DepthStyle = CSSProperties & { "--files-depth": number };

export function FilesPanel({
  notePath,
  active = true,
  onOpenNote = openNote,
}: {
  /** The active note, selected and revealed; null when no note is active. */
  notePath: string | null;
  active?: boolean;
  /** Opens a note in the workspace; without it, the URL names the note. */
  onOpenNote?: (path: string) => void;
}) {
  const open = usePanelOpen();

  const graph = useQuery({
    queryKey: queryKeys.graph.folders(),
    queryFn: ({ signal }) => getFolderGraph({ signal }),
    enabled: active && open,
  });

  const tree = useMemo(() => (graph.data ? buildFolderTree(graph.data) : null), [graph.data]);
  const { isExpanded, toggle, railProps } = useRail(EXPANDED_KEY, [], null);

  // Opening a note opens the folders above it, as an editor's explorer does.
  useEffect(() => {
    // Passing open=false to toggle opens the folder; only a new note reveals.
    for (const folder of notePath ? noteFolders(notePath) : []) toggle(folder, false);
  }, [notePath]);

  const rows = tree ? fileTreeRows(tree, (folder) => isExpanded(folder), notePath) : null;
  const currentShown = rows?.some((row) => row.current) ?? false;

  // The note scrolls into view once its folders open, not when others toggle.
  useLayoutEffect(() => {
    if (!currentShown) return;
    railProps.ref.current
      ?.querySelector('[aria-current="page"]')
      ?.scrollIntoView?.({ block: "nearest" });
  }, [notePath, currentShown, railProps.ref]);

  if (!rows || rows.length === 0) {
    return (
      <p className="files-panel__state">
        {graph.isPending
          ? "Loading files…"
          : graph.isError
            ? "The files could not be read."
            : "No notes yet."}
      </p>
    );
  }

  return (
    <nav className="files-panel" aria-label="Files" {...railProps}>
      {rows.map((row) => {
        const style: DepthStyle = { "--files-depth": row.depth };
        const folder = row.kind === "folder";

        return (
          <button
            key={`${row.kind}:${row.path}`}
            type="button"
            data-rail-row
            className={`files-panel__row files-panel__row--${row.kind}${row.current ? " is-selected" : ""}`}
            style={style}
            aria-expanded={folder ? row.open : undefined}
            aria-current={row.current ? "page" : undefined}
            title={row.path}
            onClick={() => (folder ? toggle(row.path, row.open) : onOpenNote(row.path))}
          >
            <span className="files-panel__caret" aria-hidden="true">
              {folder ? <DisclosureChevron open={row.open} /> : null}
            </span>
            {folder ? null : (
              <svg
                className="files-panel__icon"
                viewBox="0 0 16 16"
                aria-hidden="true"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d={NOTE_ICON} />
              </svg>
            )}
            <span className="files-panel__label">{row.name}</span>
          </button>
        );
      })}
    </nav>
  );
}
