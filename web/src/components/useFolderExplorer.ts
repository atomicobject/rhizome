import { useEffect, useMemo, useState } from "react";
import type { GraphResponse } from "../api/types";
import { buildFolderTree, initialOpenFolders } from "./folderTree";
import { visibleFolderChildren, visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

export function useFolderExplorer(
  graph: GraphResponse,
  colors: Map<string, string>,
  focusedPath?: string,
) {
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
      const parts = path.split("/");
      setRevealed((previous) => {
        const next = new Map(previous);
        parts.forEach((_, index) => {
          const parent = parts.slice(0, index).join("/");
          const child = parts.slice(0, index + 1).join("/");
          const folder = tree.folders.get(parent);

          while (
            folder?.folders.some((candidate) => candidate.path === child) &&
            !visibleFolderChildren(tree, folder, { open, explicit, revealed: next }, (type) =>
              colors.get(type),
            ).some((c) => c.node.id === `folder:${child}`)
          )
            next.set(parent, (next.get(parent) ?? 0) + 1);
        });

        return next;
      });
      setOverrides((previous) => {
        const next = new Map(previous);

        for (const auto of initial)
          if (auto && auto !== path && !path.startsWith(`${auto}/`) && !explicit.has(auto))
            next.set(auto, false);
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
