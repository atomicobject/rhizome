import { useLayoutEffect, useMemo, useState } from "react";
import type { Tab } from "./useNoteTabs";

const RECENT_NOTE_COUNT = 5;

type RetainedNotes = {
  activeId: string;
  recent: string[];
  resident: ReadonlySet<string>;
};

/** Retain data in every tab; only recent and protected note bodies stay rendered. */
export function useRetainedNoteTabs({
  tabs,
  activeId,
  protectEditors,
}: {
  tabs: readonly Tab[];
  activeId: string;
  protectEditors: boolean;
}): ReadonlySet<string> {
  const openNotes = useMemo(
    () => new Set(tabs.flatMap((tab) => (tab.kind === "note" ? [tab.id] : []))),
    [tabs],
  );

  const [retained, setRetained] = useState<RetainedNotes>(() => {
    const recent = openNotes.has(activeId) ? [activeId] : [];

    return { activeId, recent, resident: new Set(recent) };
  });

  useLayoutEffect(() => {
    setRetained((current) => {
      const recent = current.recent.filter((id) => id !== activeId && openNotes.has(id));

      if (openNotes.has(activeId)) recent.unshift(activeId);
      recent.length = Math.min(recent.length, RECENT_NOTE_COUNT);

      // Finishing editing releases protection on the next activation, not during save teardown.
      const previous = protectEditors || current.activeId === activeId ? current.resident : [];
      const resident = new Set([...previous, ...recent].filter((id) => openNotes.has(id)));

      if (
        current.activeId === activeId &&
        recent.length === current.recent.length &&
        recent.every((id, index) => id === current.recent[index]) &&
        resident.size === current.resident.size &&
        [...resident].every((id) => current.resident.has(id))
      )
        return current;

      return { activeId, recent, resident };
    });
  }, [activeId, openNotes, protectEditors]);

  return retained.resident;
}
