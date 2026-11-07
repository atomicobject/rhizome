import { act, fireEvent, render } from "@testing-library/react";
import type { ReactNode } from "react";
import Sigma from "sigma";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GraphEdge, GraphNode } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
import * as layouts from "./graphLayout";
import type { GraphViewNode } from "./graphViewShared";
import { useSigmaGraph } from "./useSigmaGraph";

const firstNode: GraphNode = {
  id: "one",
  label: "One",
  kind: "note",
  path: "one.md",
};

const secondNode: GraphNode = {
  id: "two",
  label: "Two",
  kind: "note",
  path: "two.md",
};

const emptySet = new Set<string>();

const emptyEdges: GraphEdge[] = [];

const globalScope = { mode: "global" } as const;

function Harness({
  nodes,
  onNodeClick,
  highlightTypes,
  scope = globalScope,
  problemCounts,
  layoutExtent,
}: {
  nodes: GraphViewNode[];
  layoutExtent?: number;
  onNodeClick: (node: GraphNode) => void;
  highlightTypes?: ReadonlySet<string>;
  scope?: { mode: "global" | "local"; label?: string };
  problemCounts?: ReadonlyMap<string, number>;
}): ReactNode {
  const { containerRef, hover, handleFitToView } = useSigmaGraph({
    nodes,
    layoutExtent,
    problemCounts,
    edges: emptyEdges,
    scope,
    onNodeClick,
    searchActive: false,
    searchPaths: emptySet,
    searchModules: emptySet,
    searchOnlyMatches: false,
    highlightTypes,
    zoomKey: 0,
  });

  return (
    <>
      <div ref={containerRef} />
      <button onClick={handleFitToView}>Fit</button>
      {hover && <div data-testid="graph-hover">{hover.label}</div>}
    </>
  );
}

function isSigma(value: unknown): value is Sigma {
  return value instanceof Sigma;
}

function clickNode(sigma: Sigma, node: string, original = new MouseEvent("click")) {
  const event = {
    x: 0,
    y: 0,
    sigmaDefaultPrevented: false,
    preventSigmaDefault: () => undefined,
    original,
  };

  act(() => {
    sigma.emit("clickNode", { node, event, preventSigmaDefault: () => undefined });
  });
}

describe("useSigmaGraph renderer lifecycle", () => {
  let restoreWebGL: () => void = () => undefined;

  beforeEach(() => {
    restoreWebGL = installFakeWebGL();
  });

  afterEach(() => {
    restoreWebGL();
    vi.restoreAllMocks();
  });

  it("keeps fixed coordinates when expanding without running layout", () => {
    const layout = vi.spyOn(layouts, "runLayout");
    const onSpy = vi.spyOn(Sigma.prototype, "on");

    const nodes = [
      { ...firstNode, x: -100, y: 20 },
      { ...secondNode, x: 80, y: -40 },
    ];

    const view = render(<Harness nodes={nodes} layoutExtent={500} onNodeClick={vi.fn()} />);
    const renderer = Array.from(new Set(onSpy.mock.contexts)).filter(isSigma)[0];
    const before = nodes.map((node) => renderer.getNodeDisplayData(node.id));
    view.rerender(
      <Harness
        nodes={[...nodes, { id: "three", label: "Three", kind: "code", x: 200, y: 50 }]}
        layoutExtent={500}
        onNodeClick={vi.fn()}
      />,
    );

    for (const node of nodes) {
      expect(renderer.getGraph().getNodeAttribute(node.id, "x")).toBe(node.x);
      expect(renderer.getGraph().getNodeAttribute(node.id, "y")).toBe(node.y);
    }

    expect(renderer.getCustomBBox()).toEqual({ x: [-500, 500], y: [-500, 500] });
    expect(nodes.map((node) => renderer.getNodeDisplayData(node.id))).toEqual(before);
    expect(layout).not.toHaveBeenCalled();
    view.unmount();
  });

  it("preserves folder cameras on refresh and expansion, and fits navigation and Fit", () => {
    vi.useFakeTimers();
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const scope = { mode: "global" } as const;
    const root = { id: "folder:", label: "Vault", kind: "module", center: true, x: 0, y: 0 };
    const folder = { id: "folder:pkg", label: "pkg", kind: "module", x: 100, y: 20 };
    const view = render(<Harness nodes={[root, folder]} scope={scope} onNodeClick={vi.fn()} />);

    try {
      const renderer = Array.from(new Set(onSpy.mock.contexts)).filter(isSigma)[0];
      act(() => vi.advanceTimersByTime(600));
      const camera = renderer.getCamera();
      camera.setState({ x: 0.23, y: 0.71, angle: 0.2, ratio: 1.7 });
      const before = camera.getState();
      view.rerender(
        <Harness
          nodes={[{ ...root }, { ...folder }, { ...secondNode, x: 200, y: 50 }]}
          scope={scope}
          onNodeClick={vi.fn()}
        />,
      );
      act(() => vi.advanceTimersByTime(600));
      expect(camera.getState()).toEqual(before);
      view.rerender(
        <Harness
          nodes={[
            { ...root, center: false },
            { ...folder, center: true },
          ]}
          scope={scope}
          onNodeClick={vi.fn()}
        />,
      );
      act(() => vi.advanceTimersByTime(600));
      expect(camera.getState()).not.toEqual(before);
      camera.setState(before);
      view.rerender(
        <Harness
          nodes={[{ ...firstNode, kind: "code", center: true, x: 0, y: 0 }]}
          scope={{ mode: "local", label: "one.md" }}
          onNodeClick={vi.fn()}
        />,
      );
      act(() => vi.advanceTimersByTime(600));
      expect(camera.getState()).not.toEqual(before);
      camera.setState(before);
      view.rerender(
        <Harness
          nodes={[root, folder]}
          scope={{ mode: "global", label: "another-root" }}
          onNodeClick={vi.fn()}
        />,
      );
      act(() => vi.advanceTimersByTime(600));
      expect(camera.getState()).not.toEqual(before);
      camera.setState(before);
      fireEvent.click(view.getByRole("button", { name: "Fit" }));
      act(() => vi.advanceTimersByTime(600));
      expect(camera.getState()).not.toEqual(before);
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it("installs problem counts that arrive while a worker lays the graph out", () => {
    // WHY: a stand-in Worker holds the layout until the test answers it, so
    // counts can change between the layout request and the install.
    let answer: ((positions: Map<string, { x: number; y: number }>) => void) | null = null;

    class HeldWorker {
      onmessage: ((event: { data: Map<string, { x: number; y: number }> }) => void) | null = null;

      postMessage() {
        answer = (positions) => this.onmessage?.({ data: positions });
      }

      terminate() {}
    }

    vi.stubGlobal("Worker", HeldWorker);
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);

    const nodes: GraphNode[] = Array.from({ length: 401 }, (_, index) => ({
      id: `n${index}`,
      label: `Node ${index}`,
      kind: "note",
      path: `n${index}.md`,
    }));

    const view = render(<Harness nodes={nodes} onNodeClick={vi.fn()} />);

    try {
      expect(answer).not.toBeNull();
      view.rerender(
        <Harness nodes={nodes} onNodeClick={vi.fn()} problemCounts={new Map([["n0", 3]])} />,
      );
      act(() => answer?.(new Map(nodes.map((node, index) => [node.id, { x: index, y: 0 }]))));

      expect(renderers()[0].getGraph().getNodeAttribute("n0", "problemCount")).toBe(3);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("keeps the renderer for callback changes and dispatches through the latest callback", () => {
    // WHY: `on` runs once per renderer during setup, so its recorded contexts
    // are the live Sigma instances, in creation order. Spying on the real
    // prototype keeps the renderer real.
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);

    const firstCallback = vi.fn();
    const latestCallback = vi.fn();
    const nodes = [firstNode];
    const view = render(<Harness nodes={nodes} onNodeClick={firstCallback} />);

    expect(renderers()).toHaveLength(1);
    view.rerender(<Harness nodes={nodes} onNodeClick={latestCallback} />);
    expect(renderers()).toHaveLength(1);

    clickNode(renderers()[0], "one");
    expect(firstCallback).not.toHaveBeenCalled();
    expect(latestCallback).toHaveBeenCalledWith(
      expect.objectContaining({ id: "one", path: "one.md" }),
      { beside: false },
    );

    // New graph data swaps the renderer's graph instead of recreating it.
    view.rerender(<Harness nodes={[firstNode, secondNode]} onNodeClick={latestCallback} />);
    expect(renderers()).toHaveLength(1);
    expect(killSpy).not.toHaveBeenCalled();
    expect(renderers()[0].getGraph().hasNode("two")).toBe(true);

    view.unmount();
    expect(killSpy.mock.contexts).toEqual([renderers()[0]]);
  });

  it("refreshes highlight reducers without rebuilding the renderer or camera", () => {
    // WHY: the mount layout refresh animates the camera with Date.now() and
    // requestAnimationFrame. Under real timers a loaded worker can still be
    // mid-animation when the test places its camera state, and the final frame
    // overwrites it. Faking those clocks makes the animation finish on demand.
    vi.useFakeTimers({
      toFake: [
        "setTimeout",
        "clearTimeout",
        "requestAnimationFrame",
        "cancelAnimationFrame",
        "Date",
      ],
    });
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const refreshSpy = vi.spyOn(Sigma.prototype, "refresh");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);
    const nodes = [{ ...firstNode, resolvedType: "Other", nodeId: "node:one" }];
    const callback = vi.fn();

    const view = render(
      <Harness nodes={nodes} onNodeClick={callback} highlightTypes={new Set<string>()} />,
    );

    try {
      const renderer = renderers().at(-1);

      if (!renderer) throw new Error("Sigma renderer did not mount");
      const camera = renderer.getCamera();
      const animate = vi.spyOn(camera, "animate");

      // Run the 120ms mount layout refresh, then its 350ms camera animation to
      // completion, before placing the state the highlight refresh must keep.
      act(() => vi.advanceTimersByTime(120));
      expect(animate).toHaveBeenCalledWith(
        { x: 0.5, y: 0.5, angle: 0, ratio: 1 },
        { duration: 350 },
      );
      act(() => vi.advanceTimersByTime(400));
      expect(camera.isAnimated()).toBe(false);

      camera.setState({ x: 0.23, y: 0.71, angle: 0.2, ratio: 1.7 });
      const cameraBefore = camera.getState();
      const refreshesBefore = refreshSpy.mock.calls.length;

      view.rerender(
        <Harness nodes={nodes} onNodeClick={callback} highlightTypes={new Set(["Spec"])} />,
      );
      act(() => vi.advanceTimersToNextFrame());

      expect(renderers()).toHaveLength(1);
      expect(killSpy).not.toHaveBeenCalled();
      expect(camera.getState()).toEqual(cameraBefore);
      expect(refreshSpy.mock.calls.length).toBeGreaterThan(refreshesBefore);
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it("keeps the renderer and camera when graph data refreshes, and resets on a new subject", () => {
    vi.useFakeTimers({
      toFake: [
        "setTimeout",
        "clearTimeout",
        "requestAnimationFrame",
        "cancelAnimationFrame",
        "Date",
      ],
    });
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);
    const callback = vi.fn();
    const scope = { mode: "local", label: "one.md" } as const;
    const view = render(<Harness nodes={[firstNode]} onNodeClick={callback} scope={scope} />);

    try {
      const renderer = renderers()[0];
      const camera = renderer.getCamera();
      // Let the mount refresh's centering animation finish first.
      act(() => vi.advanceTimersByTime(600));
      camera.setState({ x: 0.3, y: 0.6, angle: 0, ratio: 0.5 });
      const cameraBefore = camera.getState();

      // An index refresh returns equal data as new objects, plus a new node.
      view.rerender(
        <Harness
          nodes={[{ ...firstNode }, secondNode]}
          onNodeClick={callback}
          scope={{ ...scope }}
        />,
      );
      act(() => vi.advanceTimersToNextFrame());

      expect(renderers()).toEqual([renderer]);
      expect(killSpy).not.toHaveBeenCalled();
      expect(renderer.getGraph().order).toBe(2);
      expect(camera.getState()).toEqual(cameraBefore);

      view.rerender(
        <Harness
          nodes={[secondNode]}
          onNodeClick={callback}
          scope={{ ...scope, label: "two.md" }}
        />,
      );
      expect(renderers()).toEqual([renderer]);
      expect(camera.getState()).toEqual({ x: 0.5, y: 0.5, angle: 0, ratio: 1 });
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it.each(["clickStage", "clickEdge"] as const)(
    "opens a nearby node on %s, but ignores farther clicks",
    (eventName) => {
      // The pixel pick misses, as it does a few pixels off a 2-3px Explorer node.
      // Sigma keeps getNodeAtPosition private; element access reaches it, as in the renderer.
      const exactPick = Sigma.prototype["getNodeAtPosition"];
      Sigma.prototype["getNodeAtPosition"] = () => null;

      try {
        const onSpy = vi.spyOn(Sigma.prototype, "on");
        const callback = vi.fn();

        const view = render(
          <Harness nodes={[{ ...firstNode, x: 0, y: 0 }]} onNodeClick={callback} />,
        );

        const renderer = Array.from(new Set(onSpy.mock.contexts)).filter(isSigma).at(-1);

        if (!renderer) throw new Error("Sigma renderer did not mount");
        const display = renderer.getNodeDisplayData("one");

        if (!display) throw new Error("node one is not drawn");
        const center = renderer.framedGraphToViewport(display);
        const edge = center.x + renderer.scaleSize(display.size);

        const clickAt = (x: number) =>
          act(() => {
            renderer.emit(eventName, {
              edge: "link",
              event: {
                x,
                y: center.y,
                sigmaDefaultPrevented: false,
                preventSigmaDefault: () => undefined,
                original: new MouseEvent("click"),
              },
              preventSigmaDefault: () => undefined,
            });
          });

        clickAt(edge + 20);
        expect(callback).not.toHaveBeenCalled();
        clickAt(edge + 6);
        expect(callback).toHaveBeenCalledWith(expect.objectContaining({ id: "one" }), {
          beside: false,
        });
        view.unmount();
      } finally {
        Sigma.prototype["getNodeAtPosition"] = exactPick;
      }
    },
  );

  it("keeps a faded node hoverable and clickable, including beside modifier", () => {
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);

    const node = {
      ...firstNode,
      resolvedType: "Other",
      path: "docs/spec.md#^story-a",
      notePath: "docs/spec.md",
      nodeId: "story-a",
      sourceLocator: "docs/spec.md#^story-a",
      nodeRef: {
        notePath: "docs/spec.md",
        kind: "EMBEDDED" as const,
        nodeId: "story-a",
        fragment: "^story-a",
      },
    };

    const callback = vi.fn();

    const view = render(
      <Harness nodes={[node]} onNodeClick={callback} highlightTypes={new Set(["Spec"])} />,
    );

    const renderer = renderers().at(-1);

    if (!renderer) throw new Error("Sigma renderer did not mount");

    act(() => {
      const event = {
        x: 0,
        y: 0,
        sigmaDefaultPrevented: false,
        preventSigmaDefault: () => undefined,
        original: new MouseEvent("mousemove"),
      };

      renderer.emit("enterNode", { node: "one", event, preventSigmaDefault: () => undefined });
    });
    expect(view.getByTestId("graph-hover")).toHaveTextContent("One");

    clickNode(renderer, "one", new MouseEvent("click", { metaKey: true }));
    expect(callback).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "one",
        path: "docs/spec.md#^story-a",
        notePath: "docs/spec.md",
        nodeId: "story-a",
        sourceLocator: "docs/spec.md#^story-a",
        nodeRef: node.nodeRef,
        resolvedType: "Other",
      }),
      { beside: true },
    );
    view.unmount();
  });
});
