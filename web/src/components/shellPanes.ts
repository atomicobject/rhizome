import { useCallback, useEffect, useState, useSyncExternalStore } from "react";

import { decodeJson, isBoolean, isFiniteNumber, isJsonObject } from "../api/parse";

export type PanelRange = { min: number; max: number; initial: number };

export const SIDEBAR_RANGE: PanelRange = { min: 200, max: 400, initial: 264 };

export const PANE_RANGE: PanelRange = { min: 280, max: 720, initial: 380 };

/** Below this width the right pane starts collapsed and opens over the content. */
export const NARROW_QUERY = "(max-width: 1199px)";

const STORAGE_KEY = "rhizome:notes:panes:v1";

type PaneLayout = {
  pane: number;
  paneCollapsed: boolean;
  /** PROTOTYPE: the pane shows its Chat tab instead of the section's own. */
  chat: boolean;
};

export function clampWidth(value: number, range: PanelRange): number {
  return Math.round(Math.min(range.max, Math.max(range.min, value)));
}

function isLayout(value: unknown): value is Partial<PaneLayout> {
  return (
    isJsonObject(value) &&
    (value.pane === undefined || isFiniteNumber(value.pane)) &&
    (value.paneCollapsed === undefined || isBoolean(value.paneCollapsed)) &&
    (value.chat === undefined || isBoolean(value.chat))
  );
}

// The pane's width, collapsed state, and tab are per browser origin, like the
// other shell layout choices. The sidebar's live in sidebarState.ts.
function readLayout(): PaneLayout {
  let stored: Partial<PaneLayout> = {};

  try {
    stored = decodeJson(window.localStorage.getItem(STORAGE_KEY), isLayout) ?? {};
  } catch {
    // Browser storage is optional; the defaults apply.
  }

  return {
    pane: clampWidth(stored.pane ?? PANE_RANGE.initial, PANE_RANGE),
    paneCollapsed: stored.paneCollapsed ?? false,
    chat: stored.chat ?? false,
  };
}

function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => window.matchMedia?.(query).matches ?? false);

  useEffect(() => {
    const list = window.matchMedia?.(query);

    if (!list) return;
    const update = () => setMatches(list.matches);
    update();
    list.addEventListener("change", update);

    return () => list.removeEventListener("change", update);
  }, [query]);

  return matches;
}

// One pane for the whole app, so every section and the top bar's toggle share
// this state. The narrow overlay's open flag is shared too.
// `own` says whether the shown section has something of its own for the pane,
// a note's context or a selected record. Without it the pane shows only once
// the person chooses Chat, so Home and other sections keep their width.
type PaneState = PaneLayout & { overlayOpen: boolean; own: boolean };

let state: PaneState | null = null;

const listeners = new Set<() => void>();

function current(): PaneState {
  state ??= { ...readLayout(), overlayOpen: false, own: false };

  return state;
}

function update(change: (value: PaneState) => Partial<PaneState>) {
  const value = current();
  state = { ...value, ...change(value) };

  try {
    const { pane, paneCollapsed, chat } = state;
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ pane, paneCollapsed, chat }));
  } catch {
    // Browser storage is optional; the layout lasts until reload.
  }

  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);

  return () => listeners.delete(listener);
}

/** Opens or closes the pane, honoring the narrow overlay. */
export function togglePaneOpen(narrow = window.matchMedia?.(NARROW_QUERY).matches ?? false) {
  update((value) => {
    const open = narrow ? value.overlayOpen : !value.paneCollapsed;

    if ((value.own || value.chat) && open)
      return narrow ? { overlayOpen: false } : { paneCollapsed: true };

    // Opening where the section has nothing of its own opens Chat.
    const change: Partial<PaneState> = narrow ? { overlayOpen: true } : { paneCollapsed: false };

    if (!value.own) change.chat = true;

    return change;
  });
}

/** Shows the pane on its Chat tab, or closes it when Chat is already showing. */
export function toggleChat(
  showing: boolean,
  narrow = window.matchMedia?.(NARROW_QUERY).matches ?? false,
) {
  update(() => {
    if (showing) return narrow ? { overlayOpen: false } : { paneCollapsed: true };

    return { chat: true, ...(narrow ? { overlayOpen: true } : { paneCollapsed: false }) };
  });
}

/** The shown section says whether it has a note's context or a record for the pane. */
export function setPaneOwnContent(own: boolean) {
  if (current().own !== own) update(() => ({ own }));
}

export function setPaneChat(chat: boolean) {
  update(() => ({ chat }));
}

/**
 * The app's one right pane. Its width, tab, and the user's explicit collapse
 * choice persist; below 1200px it ignores the stored choice, starts
 * collapsed, and opens as an overlay.
 */
export function useShellPanes() {
  const layout = useSyncExternalStore(subscribe, current, current);
  const narrow = useMediaQuery(NARROW_QUERY);

  // Crossing the breakpoint either way starts the overlay closed.
  useEffect(() => {
    if (current().overlayOpen) update(() => ({ overlayOpen: false }));
  }, [narrow]);

  const setPaneWidth = useCallback(
    (width: number) => update(() => ({ pane: clampWidth(width, PANE_RANGE) })),
    [],
  );

  return {
    paneWidth: layout.pane,
    paneCollapsed:
      !(layout.own || layout.chat) || (narrow ? !layout.overlayOpen : layout.paneCollapsed),
    /** The pane, when open, covers the content instead of narrowing it. */
    paneOverlay: narrow,
    chat: layout.chat,
    setPaneWidth,
  };
}

/** Forget the cached state; tests call this after changing storage. */
export function resetPanesForTests() {
  state = null;
}
