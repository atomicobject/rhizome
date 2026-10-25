import { act, render } from "@testing-library/react";
import type { ReactNode } from "react";
import Sigma from "sigma";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GraphEdge, GraphNode } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
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
}: {
  nodes: GraphNode[];
  onNodeClick: (node: GraphNode) => void;
  highlightTypes?: ReadonlySet<string>;
  scope?: { mode: "global" | "local"; label?: string };
}): ReactNode {
  const { containerRef, hover } = useSigmaGraph({
    nodes,
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
