import { useCallback, useRef, useState } from "react";

const NONE: ReadonlySet<string> = new Set();

/**
 * Checked rows for bulk edits, separate from the focused row. `orderedKeys`
 * are the rows on screen in display order: a range toggle checks every row
 * from the last toggled one through this one, and a row that leaves the
 * screen, such as one a filter hides, is unchecked.
 */
export function useRowSelection(orderedKeys: string[]) {
  const [checked, setChecked] = useState<ReadonlySet<string>>(NONE);
  const anchor = useRef<string | null>(null);
  const shown = new Set(orderedKeys);

  if ([...checked].some((key) => !shown.has(key))) {
    setChecked(new Set([...checked].filter((key) => shown.has(key))));
  }

  const toggle = useCallback(
    (key: string, range = false) => {
      const from = anchor.current ? orderedKeys.indexOf(anchor.current) : -1;
      const to = orderedKeys.indexOf(key);
      anchor.current = key;

      setChecked((current) => {
        const next = new Set(current);

        if (range && from >= 0 && to >= 0) {
          for (const item of orderedKeys.slice(Math.min(from, to), Math.max(from, to) + 1)) {
            next.add(item);
          }
        } else if (next.has(key)) {
          next.delete(key);
        } else {
          next.add(key);
        }

        return next;
      });
    },
    [orderedKeys],
  );

  const setAll = useCallback((keys: string[]) => setChecked(new Set(keys)), []);

  const clear = useCallback(() => {
    anchor.current = null;
    setChecked(NONE);
  }, []);

  return { checked, toggle, setAll, clear };
}
