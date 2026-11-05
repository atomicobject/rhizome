import { useEffect, useMemo, useState } from "react";
import type { GraphResponse } from "../api/types";
import { buildFolderTree, initialOpenFolders } from "./folderTree";
import { visibleFolderChildren, visibleFolderGraph } from "./folderGraph";
import { layoutFolderGraph } from "./folderLayout";

export function useFolderExplorer(graph: GraphResponse, colors: Map<string, string>) {
  const tree = useMemo(() => buildFolderTree(graph), [graph]);
  const initial = useMemo(() => initialOpenFolders(tree), [tree]);
  const [overrides, setOverrides] = useState(new Map<string, boolean>());
  const [showAll, setShowAll] = useState(new Set<string>());

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
        visibleFolderGraph(tree, { open, explicit, showAll }, (type) => colors.get(type)),
      ),
    [tree, open, explicit, showAll, colors],
  );

  return {
    drawn,
    initial,
    open,
    reveal(path: string) {
      setShowAll((previous) => new Set([...previous, path]));
    },
    expand(path: string) {
      setExplicit((previous) => new Set([...previous, path]));
      const parts = path.split("/");
      setShowAll((previous) => {
        const next = new Set(previous);
        parts.forEach((_, index) => {
          const parent = parts.slice(0, index).join("/");
          const child = parts.slice(0, index + 1).join("/");
          const folder = tree.folders.get(parent);

          if (
            folder &&
            !visibleFolderChildren(tree, folder, { open, explicit, showAll: next }, (type) =>
              colors.get(type),
            ).some((c) => c.node.id === `folder:${child}`)
          )
            next.add(parent);
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
      setShowAll(new Set());
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
