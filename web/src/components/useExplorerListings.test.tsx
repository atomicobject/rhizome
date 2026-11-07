import { fireEvent, screen, waitFor } from "@testing-library/react";
import { it, expect } from "vitest";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { withFakeFetch, jsonReply, deferredReply } from "../test/fakeFetch";
import type { TreeResponse } from "../api/types";
import { useExplorerListings } from "./useExplorerListings";

const http = withFakeFetch();

function Listing() {
  const { tree, more } = useExplorerListings(new Set());

  return (
    <>
      <span>{tree.map((entry) => entry.name).join(",")}</span>
      <ul>{more[""]}</ul>
    </>
  );
}

it("keeps successful entries and retries the failed limit after repeated errors", async () => {
  const requests: string[] = [];
  let next = deferredReply<TreeResponse>();
  http.on("GET", "/api/v1/files/tree", (request) => {
    const limit = request.query.get("limit") || "";
    requests.push(limit);

    return limit === "200"
      ? jsonReply({
          path: "",
          entries: [{ path: "loaded.go", name: "loaded.go", kind: "code" }],
          omitted: 401,
        })
      : next.promise;
  });
  renderWithQueryClient(<Listing />);
  await screen.findByText("loaded.go");

  for (let attempt = 0; attempt < 2; attempt++) {
    fireEvent.click(screen.getByRole("button", { name: /Show .* more/ }));
    await waitFor(() => expect(requests).toHaveLength(attempt + 2));
    expect(screen.getByText("loaded.go")).toBeTruthy();
    next.reject(new Error("listing unavailable"));
    await screen.findByRole("alert");
    expect(screen.getByText("loaded.go")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Show .* more/ })).toBeEnabled();
    next = deferredReply<TreeResponse>();
  }

  expect(requests).toEqual(["200", "400", "400"]);
});
