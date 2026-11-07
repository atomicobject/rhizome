import { folderOverview } from "../test/folderOverview";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import Sigma from "sigma";
import { it, vi } from "vitest";
import { expect } from "vitest";
import type { GraphResponse } from "../api/types";
import { withFakeFetch, deferredReply } from "../test/fakeFetch";
import { installFakeWebGL } from "../test/fakeWebGL";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ExplorerWorkspace } from "./ExplorerWorkspace";

const http = withFakeFetch();

const graphWith = (...ids: string[]): GraphResponse => ({
  nodes: ids.map((id) => ({ id, kind: "code", path: id, label: id })),
  edges: [],
});

const tree = (entry: { path: string; name: string; kind: string; hasChildren: boolean }) => ({
  path: "",
  entries: [entry],
});

const fileView = (path: string) => ({
  path,
  kind: "code",
  content: path,
  links: [],
  frontmatter: {},
});

function renderExplorer() {
  const sigmaOn = vi.spyOn(Sigma.prototype, "on");
  render(<ExplorerWorkspace vaultName="rhizome" />);

  return {
    async graph() {
      await screen.findByRole("img", { name: /^Graph of / });
      const sigma = sigmaOn.mock.contexts.at(-1);

      if (!(sigma instanceof Sigma)) throw new Error("No renderer");

      return sigma.getGraph();
    },
  };
}

it("preserves root folder positions across an uncached file visit", async () => {
  const restore = installFakeWebGL();

  try {
    http.json("GET", "/api/v1/ontology/summary", { types: [], interfaces: [] });
    const local = deferredReply<GraphResponse>();
    http
      .json(
        "GET",
        "/api/v1/graphs/folders",
        folderOverview(graphWith("a/one.go", "b/two.go", "c/three.go")),
      )
      .on("GET", "/api/v1/graphs/local", () => local.promise)
      .json(
        "GET",
        "/api/v1/files/tree",
        tree({ path: "main.go", name: "main.go", kind: "code", hasChildren: false }),
      )
      .json("GET", "/api/v1/files/view", fileView("main.go"));
    const explorer = renderExplorer();

    const positions = async () => {
      const graph = await explorer.graph();

      return graph
        .filterNodes((id) => id.startsWith("folder:"))
        .map((id) => [id, graph.getNodeAttribute(id, "x"), graph.getNodeAttribute(id, "y")]);
    };

    await waitFor(async () => expect((await positions()).length).toBeGreaterThan(1));
    const before = await positions();
    fireEvent.click(await screen.findByRole("button", { name: /main.go/i }));
    await waitFor(() => expect(http.count("GET", "/api/v1/graphs/local")).toBe(1));
    local.resolve(graphWith("main.go", "z/local.go"));
    await waitFor(async () => expect((await explorer.graph()).hasNode("main.go")).toBe(true));
    act(() => {
      window.history.replaceState({}, "", "/explorer");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await waitFor(async () => expect(await positions()).toEqual(before));
  } finally {
    vi.restoreAllMocks();
    restore();
    window.history.replaceState({}, "", "/explorer");
  }
});
