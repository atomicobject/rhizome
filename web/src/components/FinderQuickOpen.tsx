import { useCallback, useEffect, useEffectEvent, useRef, useState } from "react";

import { findItems, recordFinderOpen } from "../api/client";
import type { FindItem } from "../api/types";
import { useDesktopCommands } from "./desktopHost";
import { FinderPalette, type FinderPaletteItem, IS_MAC } from "./FinderPalette";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { buildNotesLocation, NOTES_ROOT_PATH } from "./notesRoute";
import { TypeLabelsProvider, useTypeLabel } from "./typeLabels";
import type { OpenMode } from "./useNoteTabs";

type QuickOpenItem = FinderPaletteItem & { item: FindItem };

type NoteOpener = (path: string, mode: OpenMode) => void;

let notesOpener: NoteOpener | null = null;

/** The Notes workspace opens finder notes as tabs, following its own open rules. */
export function useFinderNoteRequests(open: NoteOpener) {
  const onOpen = useEffectEvent(open);

  useEffect(() => {
    const opener: NoteOpener = (path, mode) => onOpen(path, mode);

    notesOpener = opener;

    return () => {
      if (notesOpener === opener) notesOpener = null;
    };
  }, []);
}

/**
 * Opens a note. Returns true when the location stayed put (a tab opened
 * beside), so the caller records that open itself; a location change is
 * recorded by useRecordLocationOpens.
 */
// Known limit: tab targets are strings split at the first # and ?, so a note
// whose file name contains either opens the wrong file; that is the tab
// model's, not the finder's (see the effort's follow-ups).
function openNote(path: string, mode: OpenMode): boolean {
  if (notesOpener) {
    notesOpener(path, mode);

    return mode === "beside";
  }

  // Outside Notes there is no tab strip to open beside, so the note opens in
  // Notes as a regular tab through its URL.
  window.history.pushState({}, "", buildNotesLocation({ selection: { kind: "all" }, note: path }));
  notifyLocationChange();

  return false;
}

function openCodeFile(path: string) {
  window.history.pushState({}, "", `/explorer?${new URLSearchParams({ file: path }).toString()}`);
  notifyLocationChange();
}

/**
 * Counts the note or file the location shows as an open for finder frecency,
 * so tabs and Explorer files opened any way rank in ⌘P.
 */
function useRecordLocationOpens() {
  const snapshot = useLocationSnapshot();
  const last = useRef<string | null>(null);

  useEffect(() => {
    const params = new URLSearchParams(snapshot.search);

    const inNotes =
      snapshot.pathname === NOTES_ROOT_PATH || snapshot.pathname.startsWith(`${NOTES_ROOT_PATH}/`);

    const path = inNotes
      ? params.get("note")
      : snapshot.pathname === "/explorer"
        ? params.get("file")
        : null;

    // Leaving files clears the memory, so a later visit to the same one counts.
    if (!path) {
      last.current = null;

      return;
    }

    if (path === last.current) return;
    last.current = path;
    void recordFinderOpen(path);
  }, [snapshot.pathname, snapshot.search]);
}

/** ⌘P (Ctrl+P off macOS): find any note or code file. */
export function FinderQuickOpen() {
  const [open, setOpen] = useState(false);

  useRecordLocationOpens();
  useDesktopCommands((command) => {
    if (command.command === "find") setOpen(true);
  });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key.toLowerCase() !== "p" ||
        event.shiftKey ||
        event.altKey ||
        (IS_MAC ? !event.metaKey || event.ctrlKey : !event.ctrlKey || event.metaKey)
      )
        return;
      // Keep the browser's print dialog from opening.
      event.preventDefault();
      setOpen(true);
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // Type labels load only while the palette is open.
  return open ? (
    <TypeLabelsProvider>
      <QuickOpenPalette onClose={() => setOpen(false)} />
    </TypeLabelsProvider>
  ) : null;
}

function QuickOpenPalette({ onClose }: { onClose: () => void }) {
  const typeLabel = useTypeLabel();

  // Stable, so type labels loading later never re-runs the search and
  // resets the selection; describe applies them as rows render.
  const search = useCallback(
    (query: string, signal: AbortSignal) =>
      findItems(query, { signal }).then((response) =>
        response.items.map((item): QuickOpenItem => ({
          id: `${item.kind}:${item.path}`,
          kind: item.kind,
          title: item.title,
          path: item.path,
          titleMatches: item.titleMatches,
          pathMatches: item.pathMatches,
          item,
        })),
      ),
    [],
  );

  const describe = useCallback(
    (row: QuickOpenItem) =>
      row.item.kind === "code"
        ? row.item.lang
        : row.item.type
          ? typeLabel(row.item.type)
          : undefined,
    [typeLabel],
  );

  const onOpen = useCallback((row: QuickOpenItem, { beside }: { beside: boolean }) => {
    // Explorer shows one file at a time, so a code file has no "beside";
    // the palette hides that hint for code rows.
    if (row.kind === "code") {
      openCodeFile(row.path);

      return;
    }

    const locationUnchanged = openNote(row.path, beside ? "beside" : "activate");

    if (locationUnchanged) void recordFinderOpen(row.path);
  }, []);

  return (
    <FinderPalette
      open
      label="Find a note or file"
      placeholder="Find a note or file…"
      recentHeading="Recent"
      search={search}
      onOpen={onOpen}
      onClose={onClose}
      emptyText={(query) => `No notes or files match “${query}”`}
      describe={describe}
    />
  );
}
