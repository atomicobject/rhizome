import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";

/**
 * The workspace's right rail shows the record a collection view has
 * selected (SPEC-0112). The view keeps rendering the record itself, with its
 * own rows, capabilities, and edit session, into a slot the rail provides.
 */
export type RecordRail = {
  /** The rail element records render into; null while the rail is collapsed. */
  slot: HTMLElement | null;
  /** Counts views showing a record, so the rail knows to open its slot. */
  acquire: () => void;
  release: () => void;
};

const RecordRailContext = createContext<RecordRail | null>(null);

export const RecordRailProvider = RecordRailContext.Provider;

/** The workspace side of the rail: the slot it provides and whether any view is showing a record. */
export function useRecordRailState() {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [views, setViews] = useState(0);

  const rail: RecordRail = useMemo(
    () => ({
      slot,
      acquire: () => setViews((count) => count + 1),
      release: () => setViews((count) => count - 1),
    }),
    [slot],
  );

  return { rail, showing: views > 0, setSlot };
}

/** Renders a selected record into the workspace rail while `show` is true. */
export function RecordRailPortal({ show, children }: { show: boolean; children: ReactNode }) {
  const rail = useContext(RecordRailContext);
  const acquire = rail?.acquire;
  const release = rail?.release;

  useEffect(() => {
    if (!show || !acquire || !release) return;
    acquire();

    return release;
  }, [acquire, release, show]);

  return show && rail?.slot ? createPortal(children, rail.slot) : null;
}
