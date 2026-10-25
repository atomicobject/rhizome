import { createContext, type ReactNode, useContext, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";

/**
 * The workspace's right pane shows the record a collection view has
 * selected (SPEC-0112, SPEC-0126 US6). The view keeps rendering the record
 * itself, with its own rows, capabilities, and edit session, into a slot the
 * pane provides.
 */
export type RecordRail = {
  /** The pane element records render into; null while the pane is closed. */
  slot: HTMLElement | null;
  /**
   * Counts views showing a record. `selected` is false while the view has no
   * selected row, which keeps the pane closed; omitted means a row is selected.
   */
  acquire: (selected?: boolean) => void;
  release: (selected?: boolean) => void;
};

const RecordRailContext = createContext<RecordRail | null>(null);

export const RecordRailProvider = RecordRailContext.Provider;

/** The workspace side of the slot: the element it provides and whether a view has a record to show. */
export function useRecordRailState() {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [views, setViews] = useState(0);
  const [selectedViews, setSelectedViews] = useState(0);

  const rail: RecordRail = useMemo(
    () => ({
      slot,
      acquire: (selected = true) => {
        setViews((count) => count + 1);

        if (selected) setSelectedViews((count) => count + 1);
      },
      release: (selected = true) => {
        setViews((count) => count - 1);

        if (selected) setSelectedViews((count) => count - 1);
      },
    }),
    [slot],
  );

  return { rail, showing: views > 0, selected: selectedViews > 0, setSlot };
}

/**
 * Renders a selected record into the workspace's right pane while `show` is
 * true. Pass `selected={false}` while nothing is selected so the pane closes.
 */
export function RecordRailPortal({
  show,
  selected = true,
  children,
}: {
  show: boolean;
  selected?: boolean;
  children: ReactNode;
}) {
  const rail = useContext(RecordRailContext);
  const acquire = rail?.acquire;
  const release = rail?.release;

  useEffect(() => {
    if (!show || !acquire || !release) return;
    acquire(selected);

    return () => release(selected);
  }, [acquire, release, selected, show]);

  return show && rail?.slot ? createPortal(children, rail.slot) : null;
}
