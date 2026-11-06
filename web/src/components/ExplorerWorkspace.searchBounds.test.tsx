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
    expect(graph.export().edges).toEqual(before.edges);
    expect(graph.nodes()).toEqual(before.nodes.map((node) => node.key));

    for (const node of before.nodes) {
      expect(graph.getNodeAttributes(node.key)).toMatchObject({
        x: node.attributes?.x,
        y: node.attributes?.y,
      });
    }

    expect(graph.getNodeAttribute("folder:docs", "searchMatchCount")).toBe(1);
    expect(graph.getNodeAttribute("folder:src", "labelDetail")).toBe("1 match");
    expect(graph.getNodeAttribute("folder:docs", "childCount")).toBe(5000);
    expect(graph.getNodeAttribute("folder:src", "childCount")).toBe(5000);
  });
});

it("keeps hidden file matches represented until the user reveals them", async () => {
  restore = installFakeWebGL();
  const sigmaOn = vi.spyOn(Sigma.prototype, "on");
  window.history.replaceState(null, "", "?file=src/focus.ts");

  const nodes = Array.from({ length: 40 }, (_, i) => ({
    id: `n${i}`,
    path: `src/item${String(i).padStart(2, "0")}.ts`,
    kind: "code",
    label: `item${i}`,
  }));

  const graph = {
    nodes: [{ id: "focus", path: "src/focus.ts", kind: "code", label: "focus" }, ...nodes],
    edges: [],
    truncated: false,
  };

  http
    .json("GET", "/api/v1/ontology/summary", { types: [], interfaces: [] })
    .json("GET", "/api/v1/files/tree", { path: "", entries: [] })
    .json("GET", "/api/v1/files/view", {
      path: "src/focus.ts",
      kind: "code",
      content: "",
      links: [],
      frontmatter: {},
    })
    .json("GET", "/api/v1/graphs/global", graph)
    .json("GET", "/api/v1/graphs/local", graph)
    .json("GET", "/api/v1/search", {
      count: 2,
      matches: [{ path: nodes[38].path }, { path: nodes[39].path }],
    })
    .json("GET", "/api/v1/suggest", { count: 0, matches: [] });
  renderWithQueryClient(<ExplorerWorkspace vaultName="test" />);
  await screen.findByRole("img", { name: /^Graph of / });
  const sigma = sigmaOn.mock.contexts.at(-1);

  if (!(sigma instanceof Sigma)) throw new Error("No graph renderer");
  const before = sigma.getGraph().export();
  const search = screen.getByRole("searchbox", { name: "Search notes and code" });
  expect(sigma.getGraph().getNodeAttribute("file-more:Other code", "muted")).toBe(false);
  fireEvent.change(search, { target: { value: "item" } });
  fireEvent.keyDown(search, { key: "Enter" });
  await screen.findByText("2 matches");
  await waitFor(() =>
    expect(sigma.getGraph().getNodeAttribute("file-more:Other code", "searchMatchCount")).toBe(2),
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Only matches" }));
  await waitFor(() => {
    const graph = sigma.getGraph();
    expect(graph.getNodeAttributes("file-more:Other code")).toMatchObject({
      searchMatchCount: 2,
      labelDetail: "2 matches",
      muted: false,
    });
    expect(graph.getNodeAttribute("file-group:Other code", "searchMatchCount")).toBe(2);
    expect(graph.nodes()).toEqual(before.nodes.map((node) => node.key));
    expect(graph.hasNode("n39")).toBe(false);
    expect(sigma.getNodeDisplayData("file-more:Other code")?.hidden).toBe(false);
    expect(sigma.getNodeDisplayData("file-group:Other code")?.hidden).toBe(false);
  });
  window.history.replaceState(null, "", "/");
});
