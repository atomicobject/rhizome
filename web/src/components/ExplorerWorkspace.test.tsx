import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import Sigma from "sigma";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { FileView, GraphResponse, SearchResponse, TreeResponse } from "../api/types";
import {
  type DeferredReply,
  type FakeFetch,
  deferredReply,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { installFakeWebGL } from "../test/fakeWebGL";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { ExplorerWorkspace } from "./ExplorerWorkspace";

function graphWith(...ids: string[]): GraphResponse {
  return {
    nodes: ids.map((id) => ({ id, label: id, kind: "code", path: id })),
    edges: [],
    truncated: false,
  };
}

function fileView(path: string): FileView {
  return { path, kind: "code", content: path, links: [], frontmatter: {} };
}

/** Answers the tree route: a deferred reply per expanded folder, the root listing otherwise. */
function treeRoutes(
  http: FakeFetch,
  children: Map<string, DeferredReply<TreeResponse>>,
  root: TreeEntry[],
) {
  http.on("GET", "/api/v1/files/tree", (request) => {
    const path = request.query.get("path");
    const child = path ? children.get(path) : undefined;

    return child ? child.promise : jsonReply(tree(...root));
  });
}

type TreeEntry = TreeResponse["entries"][number];

function tree(...entries: TreeEntry[]): TreeResponse {
  return { path: "", entries };
}

describe("ExplorerWorkspace", () => {
  const http = withFakeFetch();

  // WHY: the workspace lazy-loads its panes. The first import transforms
  // their whole module graph, which can outlast a findBy timeout on a loaded
  // worker and leave "Preparing pane…" on screen. Loading them once here keeps
  // module transform time out of the behavior assertions.
  beforeAll(async () => {
    await Promise.all([import("./FilePanel"), import("./GraphView")]);
  });
  let restoreWebGL = () => {};

  beforeEach(() => {
    restoreWebGL = installFakeWebGL();
    // Empty, well-formed reads for everything a test does not care about.
    http
      .json("GET", "/api/v1/files/tree", tree())
      .json("GET", "/api/v1/graphs/global", graphWith())
      .json("GET", "/api/v1/graphs/local", graphWith())
      .json("GET", "/api/v1/graphs/expand", graphWith())
      .json("GET", "/api/v1/files/view", fileView(""))
      .json("GET", "/api/v1/suggest", { count: 0, matches: [] })
      .json("GET", "/api/v1/search", { count: 0, matches: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    restoreWebGL();
    window.history.replaceState({}, "", "/explorer");
  });

  /** Renders the workspace and exposes the live Sigma renderer the graph pane mounted. */
  function renderExplorer() {
    const sigmaOn = vi.spyOn(Sigma.prototype, "on");
    const utils = render(<ExplorerWorkspace vaultName="rhizome" />);

    async function renderer() {
      await screen.findByPlaceholderText("Search nodes");
      const sigma = sigmaOn.mock.contexts.at(-1);

      if (!(sigma instanceof Sigma)) throw new Error("graph pane never mounted a renderer");

      return sigma;
    }

    return {
      ...utils,
      renderer,
      async graph() {
        return (await renderer()).getGraph();
      },
    };
  }

  it("routes note selections into the notes workspace instead of opening the explorer pane", async () => {
    http.json(
      "GET",
      "/api/v1/files/tree",
      tree({ path: "docs/specs/demo.md", name: "demo.md", kind: "note", hasChildren: false }),
    );

    window.history.replaceState({}, "", "/explorer");
    const pushStateSpy = vi.spyOn(window.history, "pushState");

    renderExplorer();

    const noteRow = await screen.findByRole("button", { name: /demo\.md/i });
    fireEvent.click(noteRow);

    await waitFor(() =>
      expect(pushStateSpy).toHaveBeenCalledWith({}, "", "/notes?note=docs%2Fspecs%2Fdemo.md"),
    );
    expect(http.count("GET", "/api/v1/files/view")).toBe(0);
    expect(screen.queryByText("Close")).toBeNull();
  });

  it("uses server file kinds before suffixes and falls back for unclassified note URLs", async () => {
    http
      .json(
        "GET",
        "/api/v1/files/tree",
        tree(
          { path: "notes/Decision.HTML", name: "Decision.HTML", kind: "note", hasChildren: false },
          { path: "notes/brief.htm", name: "brief.htm", kind: "note", hasChildren: false },
          { path: "notes/README.MD", name: "README.MD", kind: "note", hasChildren: false },
          { path: "notes/legacy.MD", name: "legacy.MD", kind: "", hasChildren: false },
          { path: "src/template.html", name: "template.html", kind: "code", hasChildren: false },
        ),
      )
      .on("GET", "/api/v1/files/view", (request) =>
        jsonReply(fileView(String(request.query.get("path")))),
      );

    const pushState = vi.spyOn(window.history, "pushState");
    renderExplorer();

    for (const [name, path] of [
      ["Decision.HTML", "notes/Decision.HTML"],
      ["brief.htm", "notes/brief.htm"],
      ["README.MD", "notes/README.MD"],
      ["legacy.MD", "notes/legacy.MD"],
    ]) {
      fireEvent.click(await screen.findByRole("button", { name: new RegExp(name, "i") }));
      expect(pushState).toHaveBeenCalledWith({}, "", `/notes?note=${encodeURIComponent(path)}`);
    }

    expect(http.count("GET", "/api/v1/files/view")).toBe(0);
    fireEvent.click(screen.getByRole("button", { name: /template\.html/i }));
    expect(await screen.findByRole("heading", { name: "template.html" })).toBeVisible();
    expect(http.requests("GET", "/api/v1/files/view").at(-1)?.query.get("path")).toBe(
      "src/template.html",
    );
  });

  it("opens an embedded graph node at its canonical target", async () => {
    http.json("GET", "/api/v1/graphs/global", {
      nodes: [
        {
          id: "criterion",
          label: "Criterion",
          kind: "embedded",
          path: "specs/demo.md",
          notePath: "specs/demo.md",
          sourceLocator: "specs/demo.md#struct:criterion-fingerprint",
        },
      ],
      edges: [],
      truncated: false,
    });
    const pushState = vi.spyOn(window.history, "pushState");
    const explorer = renderExplorer();
    const renderer = await explorer.renderer();

    const event = {
      x: 0,
      y: 0,
      sigmaDefaultPrevented: false,
      preventSigmaDefault: () => undefined,
      original: new MouseEvent("click"),
    };

    act(() => {
      renderer.emit("clickNode", {
        node: "criterion",
        event,
        preventSigmaDefault: () => undefined,
      });
    });

    expect(pushState).toHaveBeenCalledWith(
      {},
      "",
      "/notes?note=specs%2Fdemo.md#struct%3Acriterion-fingerprint",
    );
  });

  it("never lets the initial global graph replace a later file selection", async () => {
    const global = deferredReply<GraphResponse>();
    http
      .json(
        "GET",
        "/api/v1/files/tree",
        tree(
          { path: "src/old.ts", name: "old.ts", kind: "code", hasChildren: false },
          { path: "src/current.ts", name: "current.ts", kind: "code", hasChildren: false },
        ),
      )
      .on("GET", "/api/v1/graphs/global", () => global.promise)
      .on("GET", "/api/v1/files/view", (request) =>
        jsonReply(fileView(String(request.query.get("path")))),
      )
      .on("GET", "/api/v1/graphs/local", (request) =>
        jsonReply(graphWith(String(request.query.get("path")))),
      );

    const explorer = renderExplorer();
    fireEvent.click(await screen.findByRole("button", { name: /current\.ts/i }));

    expect(await screen.findByRole("heading", { name: "current.ts" })).toBeVisible();
    const graph = await explorer.graph();
    await waitFor(() => expect(graph.hasNode("src/current.ts")).toBe(true));

    global.resolve(graphWith("global-late"));
    // Settle the late reply and re-acquire the live renderer: adopting the global
    // graph would rebuild Graph/Sigma, leaving the handle above stale and the
    // assertion vacuous.
    await act(async () => {
      await global.promise;
    });
    await waitFor(() => expect(http.count("GET", "/api/v1/graphs/global")).toBe(1));
    const latest = await explorer.graph();
    expect(latest.hasNode("src/current.ts")).toBe(true);
    expect(latest.hasNode("global-late")).toBe(false);
  });

  it("keeps the newest file and graph when older requests resolve last", async () => {
    const oldFile = deferredReply<FileView>();
    const currentFile = deferredReply<FileView>();
    const oldGraph = deferredReply<GraphResponse>();
    const currentGraph = deferredReply<GraphResponse>();
    http
      .json(
        "GET",
        "/api/v1/files/tree",
        tree(
          { path: "src/old.ts", name: "old.ts", kind: "code", hasChildren: false },
          { path: "src/current.ts", name: "current.ts", kind: "code", hasChildren: false },
        ),
      )
      .on("GET", "/api/v1/files/view", (request) =>
        String(request.query.get("path")).endsWith("old.ts")
          ? oldFile.promise
          : currentFile.promise,
      )
      .on("GET", "/api/v1/graphs/local", (request) =>
        String(request.query.get("path")).endsWith("old.ts")
          ? oldGraph.promise
          : currentGraph.promise,
      );

    const explorer = renderExplorer();
    fireEvent.click(await screen.findByRole("button", { name: /old\.ts/i }));
    fireEvent.click(screen.getByRole("button", { name: /current\.ts/i }));

    currentFile.resolve(fileView("src/current.ts"));
    currentGraph.resolve(graphWith("current-graph"));
    expect(await screen.findByRole("heading", { name: "current.ts" })).toBeVisible();
    const graph = await explorer.graph();
    await waitFor(() => expect(graph.hasNode("current-graph")).toBe(true));

    oldFile.resolve(fileView("src/old.ts"));
    oldGraph.resolve(graphWith("old-graph"));
    await act(async () => {
      await Promise.all([oldFile.promise, oldGraph.promise]);
    });
    await waitFor(() => expect(http.count("GET", "/api/v1/graphs/local")).toBe(2));
    expect(screen.getByRole("heading", { name: "current.ts" })).toBeVisible();
    // Re-acquire: adopting the older graph would mount a new renderer.
    const latest = await explorer.graph();
    expect(latest.hasNode("current-graph")).toBe(true);
    expect(latest.hasNode("old-graph")).toBe(false);
  });

  it("does not re-expand a folder when its late child request resolves", async () => {
    const children = deferredReply<TreeResponse>();
    treeRoutes(http, new Map([["specs", children]]), [
      { path: "specs", name: "specs", kind: "dir", hasChildren: true },
    ]);

    renderExplorer();
    const specs = await screen.findByRole("button", { name: /specs/i });
    fireEvent.click(specs);
    fireEvent.click(specs);
    children.resolve({
      path: "specs",
      entries: [{ path: "specs/hidden.md", name: "hidden.md", kind: "note", hasChildren: false }],
    });

    await waitFor(() =>
      expect(
        http.requests("GET", "/api/v1/files/tree").some((r) => r.query.get("path") === "specs"),
      ).toBe(true),
    );
    const rail = document.querySelector(".explorer-rail");

    if (!(rail instanceof HTMLElement)) throw new Error("explorer rail did not render");
    expect(within(rail).queryByRole("button", { name: /hidden\.md/i })).toBeNull();
  });

  it("keeps the newest folder selected when older children resolve last", async () => {
    const oldChildren = deferredReply<TreeResponse>();
    const currentChildren = deferredReply<TreeResponse>();
    treeRoutes(
      http,
      new Map([
        ["old", oldChildren],
        ["current", currentChildren],
      ]),
      [
        { path: "old", name: "old", kind: "dir", hasChildren: true },
        { path: "current", name: "current", kind: "dir", hasChildren: true },
      ],
    );

    renderExplorer();
    fireEvent.click(await screen.findByRole("button", { name: /^.*old$/i }));
    fireEvent.click(screen.getByRole("button", { name: /^.*current$/i }));
    currentChildren.resolve({ path: "current", entries: [] });
    expect(await screen.findByRole("heading", { name: "current" })).toBeVisible();

    oldChildren.resolve({ path: "old", entries: [] });
    await waitFor(() => expect(http.count("GET", "/api/v1/files/tree")).toBe(3));
    expect(screen.getByRole("heading", { name: "current" })).toBeVisible();
  });

  it("shows suggestions only for the latest debounced query", async () => {
    const oldSuggestions = deferredReply<SearchResponse>();
    const currentSuggestions = deferredReply<SearchResponse>();
    http.on("GET", "/api/v1/suggest", (request) =>
      request.query.get("q") === "spe" ? oldSuggestions.promise : currentSuggestions.promise,
    );

    renderExplorer();
    const search = screen.getByPlaceholderText("Search notes + code");
    fireEvent.focus(search);
    fireEvent.change(search, { target: { value: "spe" } });
    await waitFor(() => {
      const request = http
        .requests("GET", "/api/v1/suggest")
        .find((candidate) => candidate.query.get("q") === "spe");

      expect(request?.query.get("limit")).toBe("20");
      expect(request?.signal).toBeInstanceOf(AbortSignal);
    });
    fireEvent.change(search, { target: { value: "spec" } });
    await waitFor(() => {
      const request = http
        .requests("GET", "/api/v1/suggest")
        .find((candidate) => candidate.query.get("q") === "spec");

      expect(request?.query.get("limit")).toBe("20");
      expect(request?.signal).toBeInstanceOf(AbortSignal);
    });

    currentSuggestions.resolve({
      matches: [{ path: "specs/current.md", kind: "note" }],
      count: 1,
    });
    expect(await screen.findByRole("button", { name: /specs\/current\.md/i })).toBeInTheDocument();
    oldSuggestions.resolve({ count: 1, matches: [{ path: "specs/old.md", kind: "note" }] });
    await waitFor(() => expect(http.count("GET", "/api/v1/suggest")).toBe(2));
    expect(screen.queryByRole("button", { name: /specs\/old\.md/i })).toBeNull();
  });

  it("activates a classified suggestion through its click event", async () => {
    http.json("GET", "/api/v1/suggest", {
      count: 1,
      matches: [{ path: "src/guide.html", kind: "code" }],
    });
    const pushState = vi.spyOn(window.history, "pushState");

    renderExplorer();
    const search = screen.getByPlaceholderText("Search notes + code");
    fireEvent.focus(search);
    fireEvent.change(search, { target: { value: "guide" } });
    const suggestion = await screen.findByRole("button", { name: /src\/guide\.html/i });

    fireEvent.click(suggestion);

    expect(pushState).toHaveBeenCalledWith({}, "", "/explorer?file=src%2Fguide.html");
  });

  it("keeps search matches bound to the submitted query", async () => {
    const oldSearch = deferredReply<SearchResponse>();
    const currentSearch = deferredReply<SearchResponse>();
    http
      .json("GET", "/api/v1/graphs/global", graphWith("specs/current.md", "specs/old.md"))
      .on("GET", "/api/v1/search", (request) =>
        request.query.get("q") === "old" ? oldSearch.promise : currentSearch.promise,
      );

    const explorer = renderExplorer();
    const sigma = await explorer.renderer();
    const displayHidden = (id: string) => sigma.getNodeDisplayData(id)?.hidden;
    const search = screen.getByPlaceholderText("Search notes + code");
    fireEvent.change(search, { target: { value: "old" } });
    fireEvent.keyDown(search, { key: "Enter" });
    fireEvent.change(search, { target: { value: "current" } });
    fireEvent.keyDown(search, { key: "Enter" });
    fireEvent.click(screen.getByLabelText("Only matches"));

    currentSearch.resolve({ count: 1, matches: [{ path: "specs/current.md", kind: "note" }] });
    // With "Only matches" on, the graph hides everything the submitted search
    // did not return, so node visibility is the search set the pane is using.
    await waitFor(() => expect(displayHidden("specs/current.md")).toBe(false));
    expect(displayHidden("specs/old.md")).toBe(true);

    oldSearch.resolve({ count: 1, matches: [{ path: "specs/old.md", kind: "note" }] });
    await act(async () => {
      await oldSearch.promise;
    });
    await waitFor(() => expect(http.count("GET", "/api/v1/search")).toBe(2));
    // Display data only changes on refresh, which the hook defers to rAF; force it
    // so adopting the stale search set would actually show up below.
    act(() => {
      sigma.refresh();
    });
    expect(displayHidden("specs/current.md")).toBe(false);
    expect(displayHidden("specs/old.md")).toBe(true);
  });
});
