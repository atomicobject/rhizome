// The workbench sidebar's collapsed state and width, shared by every section
// (SPEC-0127). In a browser both persist per origin and follow other tabs. In
// Rhizome Desktop the app owns the collapsed state: it sends it as a page
// command, and once the page has it, the page reports its state through its
// title, so a change made in the page (a shortcut) reaches the app, which
// stores it for every window. The width persists per origin everywhere.

import { useSyncExternalStore } from "react";

import { decodeJson, isBoolean, isFiniteNumber, isJsonObject } from "../api/parse";
import { inDesktop } from "./desktopHost";
import { clampWidth, SIDEBAR_RANGE } from "./shellPanes";

export type SidebarState = {
  collapsed: boolean;
  width: number;
  /** In Desktop, whether the app has sent its stored choice yet. */
  synced: boolean;
  /** How many times the page itself toggled it; Desktop tells these apart
   * from replies to its own commands. */
  toggles: number;
};

export const SIDEBAR_KEY = "rhizome:workbench:sidebar:v1";

/** Where the Notes shell kept these before the sidebar was shared. */
const LEGACY_KEY = "rhizome:notes:panes:v1";

type Stored = { collapsed?: boolean; width?: number };

function isStored(value: unknown): value is Stored {
  return (
    isJsonObject(value) &&
    (value.collapsed === undefined || isBoolean(value.collapsed)) &&
    (value.width === undefined || isFiniteNumber(value.width))
  );
}

function isLegacy(value: unknown): value is { sidebar?: number; sidebarCollapsed?: boolean } {
  return (
    isJsonObject(value) &&
    (value.sidebar === undefined || isFiniteNumber(value.sidebar)) &&
    (value.sidebarCollapsed === undefined || isBoolean(value.sidebarCollapsed))
  );
}

function readStored(): Stored {
  try {
    const current = decodeJson(localStorage.getItem(SIDEBAR_KEY), isStored);

    if (current) return current;
    const legacy = decodeJson(localStorage.getItem(LEGACY_KEY), isLegacy);

    return legacy ? { collapsed: legacy.sidebarCollapsed, width: legacy.sidebar } : {};
  } catch {
    // Browser storage is optional; the defaults apply.
    return {};
  }
}

function read(): SidebarState {
  const stored = readStored();

  return {
    // Desktop shows the sidebar until the app sends its stored choice.
    collapsed: inDesktop() ? false : (stored.collapsed ?? false),
    width: clampWidth(stored.width ?? SIDEBAR_RANGE.initial, SIDEBAR_RANGE),
    synced: !inDesktop(),
    toggles: 0,
  };
}

let state: SidebarState | null = null;

const listeners = new Set<() => void>();

function current(): SidebarState {
  if (state) return state;
  state = read();

  // Copy carried-over Notes values to the shared key, which the Notes pane
  // store no longer keeps.
  try {
    if (localStorage.getItem(SIDEBAR_KEY) === null) write(state);
  } catch {
    // Browser storage is optional.
  }

  return state;
}

function write(next: SidebarState) {
  try {
    localStorage.setItem(
      SIDEBAR_KEY,
      JSON.stringify(
        inDesktop() ? { width: next.width } : { collapsed: next.collapsed, width: next.width },
      ),
    );
  } catch {
    // Browser storage is optional; the choice lasts until reload.
  }
}

function update(next: SidebarState, persist = true) {
  const previous = current();

  if (
    next.collapsed === previous.collapsed &&
    next.width === previous.width &&
    next.synced === previous.synced &&
    next.toggles === previous.toggles
  )
    return;
  state = next;

  if (persist) write(next);

  for (const listener of listeners) listener();
}

function onStorage(event: StorageEvent) {
  if (event.key !== SIDEBAR_KEY || inDesktop()) return;
  const stored = readStored();

  update(
    {
      collapsed: stored.collapsed ?? false,
      width: clampWidth(stored.width ?? SIDEBAR_RANGE.initial, SIDEBAR_RANGE),
      synced: true,
      toggles: current().toggles,
    },
    false,
  );
}

function subscribe(listener: () => void) {
  if (listeners.size === 0) globalThis.addEventListener?.("storage", onStorage);
  listeners.add(listener);

  return () => {
    listeners.delete(listener);

    if (listeners.size === 0) globalThis.removeEventListener?.("storage", onStorage);
  };
}

/** Show or hide the sidebar, as the user asked. */
export function setSidebarCollapsed(collapsed: boolean) {
  const state = current();

  if (state.collapsed === collapsed) return;
  update({ ...state, collapsed, toggles: state.toggles + 1 });
}

export function toggleSidebar() {
  setSidebarCollapsed(!current().collapsed);
}

export function setSidebarWidth(width: number) {
  update({ ...current(), width: clampWidth(width, SIDEBAR_RANGE) });
}

/** The app's stored choice, sent as a page command. */
export function applyDesktopSidebar(collapsed: boolean) {
  update({ ...current(), collapsed, synced: true }, false);
}

export function useWorkbenchSidebar(): SidebarState {
  return useSyncExternalStore(subscribe, current, current);
}

/** Forget the cached state; tests call this after changing storage. */
export function resetSidebarForTests() {
  state = null;
}
