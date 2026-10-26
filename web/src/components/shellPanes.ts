import { useCallback, useEffect, useState } from "react";

import { decodeJson, isBoolean, isFiniteNumber, isJsonObject } from "../api/parse";

export type PanelRange = { min: number; max: number; initial: number };

export const SIDEBAR_RANGE: PanelRange = { min: 200, max: 400, initial: 264 };

export const PANE_RANGE: PanelRange = { min: 280, max: 560, initial: 340 };

/** Below this width the right pane starts collapsed and opens over the content. */
export const NARROW_QUERY = "(max-width: 1199px)";

const STORAGE_KEY = "rhizome:notes:panes:v1";

type PaneLayout = {
  pane: number;
  paneCollapsed: boolean;
};

export function clampWidth(value: number, range: PanelRange): number {
  return Math.round(Math.min(range.max, Math.max(range.min, value)));
}

function isLayout(value: unknown): value is Partial<PaneLayout> {
  return (
    isJsonObject(value) &&
    (value.pane === undefined || isFiniteNumber(value.pane)) &&
    (value.paneCollapsed === undefined || isBoolean(value.paneCollapsed))
  );
}

// The pane's width and collapsed state are per browser origin, like the other
// shell layout choices. The sidebar's live in sidebarState.ts.
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

/**
 * The Notes shell's one right pane. Its width and the user's explicit collapse
 * choice persist; below 1200px it ignores the stored choice, starts collapsed,
 * and opens as an overlay.
 */
export function useShellPanes() {
  const [layout, setLayout] = useState(readLayout);
  const narrow = useMediaQuery(NARROW_QUERY);
  const [overlayOpen, setOverlayOpen] = useState(false);

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
    } catch {
      // Browser storage is optional; the layout lasts until reload.
    }
  }, [layout]);

  // Crossing the breakpoint either way starts the overlay closed.
  useEffect(() => setOverlayOpen(false), [narrow]);

  const setPaneWidth = useCallback(
    (width: number) =>
      setLayout((current) => ({ ...current, pane: clampWidth(width, PANE_RANGE) })),
    [],
  );

  const togglePane = useCallback(() => {
    if (narrow) setOverlayOpen((open) => !open);
    else setLayout((current) => ({ ...current, paneCollapsed: !current.paneCollapsed }));
  }, [narrow]);

  return {
    paneWidth: layout.pane,
    paneCollapsed: narrow ? !overlayOpen : layout.paneCollapsed,
    /** The pane, when open, covers the content instead of narrowing it. */
    paneOverlay: narrow,
    setPaneWidth,
    togglePane,
  };
}
