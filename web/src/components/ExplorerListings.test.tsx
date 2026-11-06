import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";
import { withFakeFetch, jsonReply } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { TreeList } from "./TreeList";
import { useExplorerListings } from "./useExplorerListings";

const http = withFakeFetch();

for (const path of ["", "folder"]) {
  it(`pages a listing of 201 eligible entries at ${path || "root"}`, async () => {
    const entries = Array.from({ length: 201 }, (_, i) => ({
      name: `file${i}.go`,
      path: `${path}/file${i}.go`,
      kind: "code",
      hasChildren: false,
    }));

    const limits: number[] = [];
    http.on("GET", "/api/v1/files/tree", (request) => {
      const requestedPath = request.query.get("path") ?? "";

      if (requestedPath !== path) return jsonReply({ path: "", entries: [] });
      const limit = Number(request.query.get("limit") ?? 200);
      limits.push(limit);

      return jsonReply({
        path,
        entries: entries.slice(0, limit),
        truncated: limit < 201,
        omitted: Math.max(0, 201 - limit),
      });
    });

    function Listing() {
      const listing = useExplorerListings(new Set(path ? [path] : []));

      return (
        <TreeList
          entries={path ? (listing.expanded[path] ?? []) : listing.tree}
          expanded={{}}
          more={listing.more}
          path={path}
          selectedPath={null}
          onSelect={() => {}}
          onToggle={() => {}}
        />
      );
    }

    renderWithQueryClient(<Listing />);
    const more = await screen.findByText("Show 1 more (1 remaining)");
    await waitFor(() => expect(more).toBeEnabled());
    fireEvent.click(more);
    expect(await screen.findByText("file200.go")).toBeVisible();
    await waitFor(() => expect(screen.queryByText(/remaining/)).not.toBeInTheDocument());
    expect(limits).toEqual([200, 400]);
  });
}
