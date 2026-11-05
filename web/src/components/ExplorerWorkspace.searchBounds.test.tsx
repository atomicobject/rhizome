import { fireEvent, screen, waitFor } from "@testing-library/react";
import Sigma from "sigma";
import { installFakeWebGL } from "../test/fakeWebGL";
import { beforeAll, afterEach, expect, it, vi } from "vitest";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { ExplorerWorkspace } from "./ExplorerWorkspace";

beforeAll(async () => {
  await import("./GraphView");
});

let restore = () => {};

afterEach(() => {
  restore();
  vi.restoreAllMocks();
});

const http = withFakeFetch();

it("keeps a large search aggregated before building the renderer graph", async () => {
  restore = installFakeWebGL();
  const sigmaOn = vi.spyOn(Sigma.prototype, "on");

  const nodes = Array.from({ length: 10000 }, (_, i) => ({
    id: `n${i}`,
    path: `${i % 2 ? "docs" : "src"}/item${i}.${i % 2 ? "md" : "ts"}`,
    kind: i % 2 ? "note" : "code",
    label: `item${i}`,
  }));

  http
    .json("GET", "/api/v1/ontology/summary", { types: [], interfaces: [] })
    .json("GET", "/api/v1/files/tree", { path: "", entries: [] })
    .json("GET", "/api/v1/graphs/global", { nodes, edges: [], truncated: false })
    .json("GET", "/api/v1/search", {
      count: 2,
      matches: [
        { path: "docs/item1.md", kind: "note" },
        { path: "src/item0.ts", kind: "code" },
      ],
    })
    .json("GET", "/api/v1/suggest", { count: 0, matches: [] });
  renderWithQueryClient(<ExplorerWorkspace vaultName="test" />);
  await screen.findByRole("img", { name: /^Graph of / });
  const sigma = sigmaOn.mock.contexts.at(-1);

  if (!(sigma instanceof Sigma)) throw new Error("No graph renderer");
  const before = sigma.getGraph().export();
  const search = screen.getByRole("searchbox", { name: "Search notes and code" });
  fireEvent.change(search, { target: { value: "item" } });
  fireEvent.keyDown(search, { key: "Enter" });
  await screen.findByText("2 matches");
  await waitFor(() => {
    const graph = sigma.getGraph();
    expect(graph.order).toBeLessThan(40);
    expect(graph.export()).toEqual(before);
    expect(graph.getNodeAttribute("folder:docs", "childCount")).toBe(5000);
    expect(graph.getNodeAttribute("folder:src", "childCount")).toBe(5000);
  });
});
