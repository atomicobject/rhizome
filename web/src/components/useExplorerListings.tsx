import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { getTree } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { TreeEntry, TreeResponse } from "../api/types";

export function useExplorerListings(expandedPaths: Set<string>) {
  const queryClient = useQueryClient();
  const [limits, setLimits] = useState<Record<string, number>>({});

  const previousPage = (path: string) =>
    queryClient.getQueryData<TreeResponse>(
      queryKeys.files.tree(path || undefined, Math.max(200, (limits[path] ?? 200) - 200)),
    );

  const options = (path: string) => ({
    queryKey: queryKeys.files.tree(path || undefined, limits[path] ?? 200),
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      getTree(path || undefined, limits[path] ?? 200, { signal }),
    placeholderData: () => previousPage(path),
  });

  const treeQuery = useQuery(options(""));
  const paths = [...expandedPaths].sort();
  const queries = useQueries({ queries: paths.map(options) });
  const expanded: Record<string, TreeEntry[]> = {};
  paths.forEach((path, i) => {
    expanded[path] = (queries[i].data ?? previousPage(path))?.entries ?? [];
  });
  const more: Record<string, React.ReactNode> = {};
  ["", ...paths].forEach((path, i) => {
    const query = i === 0 ? treeQuery : queries[i - 1];
    const omitted = (query.data ?? previousPage(path))?.omitted ?? 0;

    if (omitted > 0)
      more[path] = (
        <li className="tree-item">
          <button
            type="button"
            className="tree-row"
            disabled={query.isFetching}
            onClick={() =>
              setLimits((previous) => ({ ...previous, [path]: (previous[path] ?? 200) + 200 }))
            }
          >
            Show {Math.min(200, omitted)} more ({omitted} remaining)
          </button>
          {query.isError && (
            <span role="alert">
              Could not load more.{" "}
              <button type="button" onClick={() => query.refetch()}>
                Retry
              </button>
            </span>
          )}
        </li>
      );
  });

  return { treeQuery, tree: (treeQuery.data ?? previousPage(""))?.entries ?? [], expanded, more };
}
