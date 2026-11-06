import { useEffect, useMemo, useRef, useState } from "react";
import type { FolderOverview } from "../api/types";
import { buildFolderTree, initialOpenFolders } from "./folderTree";
import { visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph, type FolderAnchors } from "./folderLayout";

export function useFolderExplorer(
  graph: FolderOverview,
  colors: Map<string, string>,
  focusedPath?: string,
) {
  const anchors = useRef<FolderAnchors>({ positions: new Map(), free: [] });
  const tree = useMemo(() => buildFolderTree(graph), [graph]);
  const initial = useMemo(() => initialOpenFolders(tree), [tree]);
  const [overrides, setOverrides] = useState(new Map<string, boolean>());
  const [revealed, setRevealed] = useState(new Map<string, number>());

  const [explicit, setExplicit] = useState(new Set<string>());

  const open = useMemo(() => {
    const result = new Set(initial);

    for (const [path, value] of overrides) {
      if (value) result.add(path);
      else result.delete(path);
    }

    result.add("");

    return result;
  }, [initial, overrides]);

  const drawn = useMemo(
    () =>
      layoutFolderGraph(
        tree,
        visibleFolderGraph(
          tree,
          { open, explicit, revealed },
          (type) => colors.get(type),
          focusedPath,
        ),
        anchors.current,
      ),
    [tree, open, explicit, revealed, colors, focusedPath],
  );

  return {
    drawn,
    initial,
    open,
    reveal(path: string) {
      setRevealed((previous) => new Map(previous).set(path, (previous.get(path) ?? 0) + 1));
    },
    expand(path: string) {
      setExplicit((previous) => new Set([...previous, path]));
      setOverrides((previous) => {
        const next = new Map(previous);

        const parts = path.split("/");
        parts.forEach((_, index) => next.set(parts.slice(0, index + 1).join("/"), true));

        return next;
      });
    },
    collapse(path: string) {
      setOverrides((previous) => {
        const next = new Map(previous);

        for (const child of tree.folders.keys())
          if (child === path || child.startsWith(`${path}/`)) next.set(child, false);

        return next;
      });
    },
    reset() {
      setOverrides(new Map());
      setExplicit(new Set());
      setRevealed(new Map());
    },
  };
}

export function useDebouncedValue(value: string, delay: number) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);

    return () => window.clearTimeout(timer);
  }, [delay, value]);

  return debounced;
}
